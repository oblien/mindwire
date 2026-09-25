package agent

import (
	"encoding/json"
	"os"
	"reflect"
	"sync"
)

// NativeTranscripts caches parsing, never the source of truth. Every read checks
// the native file identity, size, and nanosecond modification time. Recorded run
// state and interaction answers are overlaid by the adapters/API after this cache.
var NativeTranscripts = newHistoryCache(8, 64<<20)

type historyCacheKey struct{ path, chatID string }
type historyCacheEntry struct {
	ready    chan struct{}
	stamp    os.FileInfo
	messages []Message
	cost     int
	used     uint64
}

type historyCache struct {
	mu       sync.Mutex
	entries  map[historyCacheKey]*historyCacheEntry
	clock    uint64
	capacity int
	maxBytes int
}

func newHistoryCache(capacity, maxBytes int) *historyCache {
	return &historyCache{entries: make(map[historyCacheKey]*historyCacheEntry), capacity: capacity, maxBytes: maxBytes}
}

// Read returns owned messages. Mutable slices, maps, and pointers are copied so
// interaction overlays and in-process SDK consumers cannot modify the cache.
// A per-file flight shares concurrent reads; unrelated conversations never wait.
func (c *historyCache) Read(path, chatID string, parse func(*os.File) ([]Message, error)) ([]Message, error) {
	key := historyCacheKey{path, chatID}
	for {
		file, err := os.Open(path)
		if err != nil {
			c.mu.Lock()
			delete(c.entries, key)
			c.mu.Unlock()
			return nil, err
		}
		stamp, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		c.mu.Lock()
		c.clock++
		if entry := c.entries[key]; entry != nil {
			if entry.ready != nil {
				ready := entry.ready
				c.mu.Unlock()
				file.Close()
				<-ready
				continue // Re-stat after waiting: the native CLI may have appended.
			}
			if sameTranscript(entry.stamp, stamp) {
				entry.used = c.clock
				messages := entry.messages
				c.mu.Unlock()
				file.Close()
				return cloneHistory(messages), nil
			}
		}
		entry := &historyCacheEntry{ready: make(chan struct{}), used: c.clock}
		c.entries[key] = entry
		c.mu.Unlock()

		messages, parseErr := parse(file)
		after, statErr := file.Stat()
		file.Close()
		cost := 0
		if parseErr == nil && statErr == nil && sameTranscript(stamp, after) {
			// Conservative decoded-memory allowance. Encode one message at a time,
			// then discard it; never allocate another full transcript just to size it.
			for _, message := range messages {
				encoded, err := json.Marshal(message)
				if err != nil {
					cost = c.maxBytes + 1
					break
				}
				cost += 2*len(encoded) + 256
				if cost > c.maxBytes {
					break
				}
			}
		} else {
			cost = c.maxBytes + 1
		}
		c.mu.Lock()
		ready := entry.ready
		entry.ready = nil
		if c.entries[key] == entry {
			if cost <= c.maxBytes {
				entry.messages, entry.stamp, entry.cost = messages, after, cost
				c.trim()
			} else {
				delete(c.entries, key)
			}
		}
		close(ready)
		c.mu.Unlock()
		return cloneHistory(messages), parseErr
	}
}

func cloneHistory(messages []Message) []Message {
	return cloneHistoryValue(reflect.ValueOf(messages)).Interface().([]Message)
}

// Protocol records contain JSON data: acyclic structs, slices, maps, and scalar
// values. Copy mutable containers without re-encoding large immutable strings.
// Walking the shape also covers new optional protocol fields automatically.
func cloneHistoryValue(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return value
		}
		if value.Kind() == reflect.Pointer {
			copy := reflect.New(value.Type().Elem())
			copy.Elem().Set(cloneHistoryValue(value.Elem()))
			return copy
		}
		copy := reflect.New(value.Type()).Elem()
		copy.Set(cloneHistoryValue(value.Elem()))
		return copy
	case reflect.Slice:
		if value.IsNil() {
			return value
		}
		copy := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		if value.Type().Elem().Kind() == reflect.Uint8 {
			reflect.Copy(copy, value)
			return copy
		}
		for i := 0; i < value.Len(); i++ {
			copy.Index(i).Set(cloneHistoryValue(value.Index(i)))
		}
		return copy
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		copy := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			copy.SetMapIndex(iterator.Key(), cloneHistoryValue(iterator.Value()))
		}
		return copy
	case reflect.Struct:
		copy := reflect.New(value.Type()).Elem()
		copy.Set(value)
		for i := 0; i < value.NumField(); i++ {
			if copy.Field(i).CanSet() {
				copy.Field(i).Set(cloneHistoryValue(value.Field(i)))
			}
		}
		return copy
	default:
		return value
	}
}

func sameTranscript(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (c *historyCache) trim() {
	for {
		bytes, count := 0, 0
		var oldest *historyCacheEntry
		var key historyCacheKey
		for k, entry := range c.entries {
			if entry.ready != nil {
				continue
			}
			bytes += entry.cost
			count++
			if oldest == nil || entry.used < oldest.used {
				oldest, key = entry, k
			}
		}
		if count <= c.capacity && bytes <= c.maxBytes {
			return
		}
		delete(c.entries, key)
	}
}
