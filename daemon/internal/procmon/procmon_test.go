package procmon

import (
	"testing"
	"time"
)

// running reports whether the sampler goroutine is currently alive. Same-package test helper: reads
// the lifecycle flag under the monitor's own lock.
func (m *Monitor) running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stop != nil
}

// TestSubscribeLifecycle is the on-demand / no-leak guarantee: the sampler runs ONLY while at least
// one subscriber is connected — it starts on the first Subscribe and stops on the last cancel.
func TestSubscribeLifecycle(t *testing.T) {
	m := NewMonitor()
	if m.running() {
		t.Fatal("new monitor must not sample before any subscriber")
	}

	_, cancel1, ok := m.Subscribe()
	if !ok {
		t.Fatal("first Subscribe rejected")
	}
	if !m.running() {
		t.Fatal("sampler must start on the first subscriber")
	}

	_, cancel2, ok := m.Subscribe()
	if !ok {
		t.Fatal("second Subscribe rejected")
	}
	if !m.running() {
		t.Fatal("sampler must keep running with a second subscriber")
	}

	cancel1()
	if !m.running() {
		t.Fatal("sampler must keep running while one subscriber remains")
	}

	cancel2()
	if m.running() {
		t.Fatal("sampler must stop when the last subscriber leaves")
	}

	// cancel is idempotent and re-subscribe restarts the sampler.
	cancel2()
	_, cancel3, ok := m.Subscribe()
	if !ok || !m.running() {
		t.Fatal("re-subscribe after full teardown must restart the sampler")
	}
	cancel3()
}

// TestSubscriberCap rejects beyond maxSubscribers so a flood of stream opens can't fan out unboundedly.
func TestSubscriberCap(t *testing.T) {
	m := NewMonitor()
	cancels := make([]func(), 0, maxSubscribers)
	for i := 0; i < maxSubscribers; i++ {
		_, cancel, ok := m.Subscribe()
		if !ok {
			t.Fatalf("Subscribe %d rejected below cap", i)
		}
		cancels = append(cancels, cancel)
	}
	if _, _, ok := m.Subscribe(); ok {
		t.Fatal("Subscribe past the cap must be rejected")
	}
	// Freeing a slot lets a new subscriber in again.
	cancels[0]()
	_, cancel, ok := m.Subscribe()
	if !ok {
		t.Fatal("Subscribe after freeing a slot must succeed")
	}
	cancel()
	for _, c := range cancels[1:] {
		c()
	}
}

// TestFrameBroadcast confirms a subscribed client receives frames — even with no tracked turns, the
// tick emits an empty keep-alive frame (a valid, meaningful frame).
func TestFrameBroadcast(t *testing.T) {
	m := NewMonitor()
	ch, cancel, ok := m.Subscribe()
	if !ok {
		t.Fatal("Subscribe rejected")
	}
	defer cancel()

	// Drive one tick directly rather than waiting on the 1.5s ticker (the background loop is harmless).
	m.tick(time.Unix(1_700_000_000, 0))
	select {
	case f := <-ch:
		if len(f.Samples) != 0 {
			t.Fatalf("expected empty samples with no tracked turns, got %d", len(f.Samples))
		}
		if f.At == "" {
			t.Fatal("frame must carry a timestamp")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no frame delivered to subscriber")
	}
}

// TestTrackUntrack covers the turn-lifecycle bookkeeping: Track adds a labeled group, re-Track updates
// its pgid, Untrack removes it, and Untrack of an unknown run is a no-op.
func TestTrackUntrack(t *testing.T) {
	m := NewMonitor()
	m.Track("run1", "claude", "chatA", 111)
	m.Track("run1", "claude", "chatA", 222) // re-track updates pgid
	m.Track("run2", "codex", "chatB", 333)

	m.mu.Lock()
	if got := m.tracks["run1"].pgid; got != 222 {
		m.mu.Unlock()
		t.Fatalf("re-Track must update pgid, got %d", got)
	}
	if len(m.tracks) != 2 {
		m.mu.Unlock()
		t.Fatalf("expected 2 tracks, got %d", len(m.tracks))
	}
	m.mu.Unlock()

	m.Untrack("run1")
	m.Untrack("does-not-exist") // no-op
	m.mu.Lock()
	if _, ok := m.tracks["run1"]; ok {
		m.mu.Unlock()
		t.Fatal("Untrack must remove the run")
	}
	if len(m.tracks) != 1 {
		m.mu.Unlock()
		t.Fatalf("expected 1 track after Untrack, got %d", len(m.tracks))
	}
	m.mu.Unlock()
}
