package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const catalogTTL = time.Hour

type catalogCache struct {
	mu         sync.Mutex
	url        string
	path       string
	client     *http.Client
	catalog    Catalog
	source     string
	fetchedAt  time.Time
	retryAfter time.Time
	pending    chan struct{}
}

type savedCatalog struct {
	URL       string          `json:"url"`
	FetchedAt time.Time       `json:"fetchedAt"`
	Catalog   json.RawMessage `json:"catalog"`
}

func newCatalogCache(root, source string) *catalogCache {
	c := &catalogCache{url: source, path: filepath.Join(root, "compatibility.json"),
		client: &http.Client{Timeout: 5 * time.Second}, catalog: Bundled(), source: "bundled"}
	if data, err := os.ReadFile(c.path); err == nil {
		var saved savedCatalog
		if json.Unmarshal(data, &saved) == nil && saved.URL == source {
			if parsed, err := DecodeCatalog(saved.Catalog); err == nil && parsed.Revision >= c.catalog.Revision {
				c.catalog, c.fetchedAt, c.source = parsed, saved.FetchedAt, "cached"
			}
		}
	}
	return c
}

func (c *catalogCache) snapshot() (Catalog, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.catalog, c.source, time.Since(c.fetchedAt) >= catalogTTL
}

// Refresh never edits a selected CLI. Failed, oversized, older, or future-schema documents
// retain the last accepted policy. Concurrent clients share a single bounded HTTPS request.
func (c *catalogCache) refresh(ctx context.Context, force bool) error {
	c.mu.Lock()
	if c.url == "off" || (!force && time.Since(c.fetchedAt) < catalogTTL) || time.Now().Before(c.retryAfter) {
		c.mu.Unlock()
		return nil
	}
	if pending := c.pending; pending != nil {
		c.mu.Unlock()
		select {
		case <-pending:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.pending = make(chan struct{})
	c.mu.Unlock()
	data, err := c.fetch(ctx)
	var candidate Catalog
	if err == nil {
		candidate, err = DecodeCatalog(data)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { close(c.pending); c.pending = nil }()
	if err == nil && candidate.Revision < c.catalog.Revision {
		err = fmt.Errorf("catalog revision moved backwards")
	}
	if err == nil {
		for id := range Bundled().Harnesses {
			if _, ok := candidate.Harnesses[id]; !ok {
				err = fmt.Errorf("catalog omits harness %s", id)
				break
			}
		}
	}
	if err == nil && candidate.Revision == c.catalog.Revision {
		old, _ := json.Marshal(c.catalog)
		next, _ := json.Marshal(candidate)
		if string(old) != string(next) {
			err = fmt.Errorf("catalog content changed without a revision increment")
		}
	}
	if err != nil {
		c.retryAfter = time.Now().Add(time.Minute)
		return err
	}
	c.catalog, c.source, c.fetchedAt = candidate, "remote", time.Now()
	c.retryAfter = time.Now().Add(5 * time.Second)
	encoded, _ := json.Marshal(savedCatalog{URL: c.url, FetchedAt: c.fetchedAt, Catalog: data})
	// Persistence is a cache optimization, not permission to switch executables. A read-only
	// filesystem can still use a verified policy in memory and the bundled fallback next launch.
	_ = atomicWrite(c.path, encoded)
	return nil
}

func (c *catalogCache) fetch(ctx context.Context) ([]byte, error) {
	u, err := url.Parse(c.url)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return nil, fmt.Errorf("harness catalog requires HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog HTTP %d", res.StatusCode)
	}
	const limit = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if len(data) > limit {
		return nil, fmt.Errorf("harness catalog exceeds size limit")
	}
	return data, err
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
