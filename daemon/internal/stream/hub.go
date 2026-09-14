// Package stream is the daemon's per-run event hub: the runner publishes unified
// events for a run; the SSE endpoint replays the buffer to a late subscriber and
// then streams live events until the run completes (or the client disconnects).
//
// A finished run's topic is reaped after a short grace window so memory doesn't grow
// without bound across the daemon's lifetime, and a subscriber that can't keep up is
// evicted (its channel closed) rather than silently losing events — the client
// reconnects and replays the full retained buffer.
package stream

import (
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

const (
	// subBuffer is a subscriber channel's depth; overflow evicts the subscriber (see Publish).
	subBuffer = 256
	// reapAfter keeps a closed run's topic (its replay buffer) alive briefly so a client that
	// connects just after completion still gets the full stream, then frees it.
	reapAfter = 2 * time.Minute
)

type Hub struct {
	mu     sync.Mutex
	topics map[string]*topic
	reap   time.Duration // grace window before a closed topic is freed (test seam; New uses reapAfter)
}

type topic struct {
	mu         sync.Mutex
	buf        []agent.Event // full replay buffer for this run
	transcript Transcript
	subs       map[chan agent.Event]struct{}
	closed     bool
}

func New() *Hub { return newHub(reapAfter) }

// newHub is the reap-duration-parameterized constructor New delegates to; tests use it to shrink the
// grace window so reaping is observable without a real wall-clock wait.
func newHub(reap time.Duration) *Hub { return &Hub{topics: map[string]*topic{}, reap: reap} }

func (h *Hub) get(id string) *topic {
	h.mu.Lock()
	defer h.mu.Unlock()
	t := h.topics[id]
	if t == nil {
		t = &topic{subs: map[chan agent.Event]struct{}{}}
		h.topics[id] = t
	}
	return t
}

// Publish appends an event to the run's buffer and fans it out to live subscribers.
func (h *Hub) Publish(id string, ev agent.Event) {
	t := h.get(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	ev.Sequence = int64(len(t.buf)) + 1
	ev.Replay = false
	if ev.At == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	t.transcript.Apply(ev)
	t.buf = append(t.buf, ev)
	for ch := range t.subs {
		select {
		case ch <- ev:
		default:
			// Close a slow subscriber so it reconnects from its last delivered sequence.
			// Unseen events stay in the retained buffer; none is silently dropped.
			delete(t.subs, ch)
			close(ch)
		}
	}
}

// Snapshot reads the components and cursor under the same lock as Publish. Looking up
// a completed/reaped run must not create a new, empty live topic.
func (h *Hub) Snapshot(id string) (Snapshot, bool) {
	h.mu.Lock()
	t := h.topics[id]
	h.mu.Unlock()
	if t == nil {
		return Snapshot{Parts: []agent.Part{}}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.transcript.Snapshot(t.closed), true
}

// Close marks the run finished, closes all live subscriber channels, and schedules the
// topic (and its buffer) to be freed after a grace window for late replay subscribers.
func (h *Hub) Close(id string) {
	t := h.get(id)
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	for ch := range t.subs {
		close(ch)
		delete(t.subs, ch)
	}
	t.mu.Unlock()

	time.AfterFunc(h.reap, func() {
		h.mu.Lock()
		delete(h.topics, id)
		h.mu.Unlock()
	})
}

// Subscribe atomically snapshots the replay buffer and (if the run is still live)
// registers a channel for subsequent events. `done` is true when the run already
// finished — the caller just sends the replay and stops. Always call cancel().
func (h *Hub) Subscribe(id string) (replay []agent.Event, ch <-chan agent.Event, done bool, cancel func()) {
	return h.SubscribeAfter(id, 0)
}

// SubscribeAfter atomically replays events newer than after and subscribes to live
// updates. Sequence numbers survive reconnects for the lifetime of the run topic.
func (h *Hub) SubscribeAfter(id string, after int64) (replay []agent.Event, ch <-chan agent.Event, done bool, cancel func()) {
	t := h.get(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	start := min(max(after, 0), int64(len(t.buf)))
	replay = append([]agent.Event(nil), t.buf[start:]...)
	if t.closed {
		return replay, nil, true, func() {}
	}
	c := make(chan agent.Event, subBuffer)
	t.subs[c] = struct{}{}
	cancel = func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if _, ok := t.subs[c]; ok {
			delete(t.subs, c)
			close(c)
		}
	}
	return replay, c, false, cancel
}
