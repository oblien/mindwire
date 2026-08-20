package stream

import (
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func txt(s string) agent.Event { return agent.Event{Type: agent.EventText, Text: s} }

// A subscriber that joins mid-run gets the full replay buffer in publish order, then live events on the
// channel — with no gap or reordering across the boundary.
func TestSubscribeReplaysThenStreamsLive(t *testing.T) {
	h := New()
	h.Publish("r", txt("a"))
	h.Publish("r", txt("b"))

	replay, ch, done, cancel := h.Subscribe("r")
	defer cancel()
	if done {
		t.Fatal("run is still live; done should be false")
	}
	if len(replay) != 2 || replay[0].Text != "a" || replay[1].Text != "b" {
		t.Fatalf("replay = %+v, want [a b] in order", replay)
	}

	h.Publish("r", txt("c"))
	select {
	case ev := <-ch:
		if ev.Text != "c" {
			t.Fatalf("live event = %q, want c", ev.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the live event")
	}
}

// One event fans out to every live subscriber.
func TestPublishFansOutToAllSubscribers(t *testing.T) {
	h := New()
	_, a, _, ca := h.Subscribe("r")
	defer ca()
	_, b, _, cb := h.Subscribe("r")
	defer cb()

	h.Publish("r", txt("hi"))
	for name, ch := range map[string]<-chan agent.Event{"a": a, "b": b} {
		select {
		case ev := <-ch:
			if ev.Text != "hi" {
				t.Errorf("subscriber %s got %q, want hi", name, ev.Text)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %s timed out", name)
		}
	}
}

// A subscriber that can't keep up (its buffer overflows) is EVICTED — its channel is closed rather than
// silently losing events, so the client reconnects and replays. Filling > subBuffer without draining
// forces the eviction path.
func TestSlowSubscriberEvicted(t *testing.T) {
	h := New()
	_, ch, _, cancel := h.Subscribe("r")
	defer cancel()

	for i := 0; i < subBuffer+10; i++ {
		h.Publish("r", txt("x"))
	}

	// Drain the buffered events; once drained, the channel must be closed (evicted), not blocking.
	closed := false
	deadline := time.After(2 * time.Second)
	for !closed {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
			}
		case <-deadline:
			t.Fatal("evicted subscriber's channel was never closed")
		}
	}
	// The full history is still retained for a reconnecting client.
	replay, _, _, c2 := h.Subscribe("r")
	defer c2()
	if len(replay) != subBuffer+10 {
		t.Errorf("retained buffer = %d events, want %d", len(replay), subBuffer+10)
	}
}

// After Close, a new subscriber gets done=true plus the full retained replay (a client that connects
// just after completion still sees the whole stream), and Publish is a no-op.
func TestSubscribeAfterCloseReplaysAndSignalsDone(t *testing.T) {
	h := New()
	h.Publish("r", txt("a"))
	h.Close("r")
	h.Publish("r", txt("ignored")) // after close → dropped

	replay, ch, done, cancel := h.Subscribe("r")
	defer cancel()
	if !done {
		t.Fatal("done should be true after Close")
	}
	if ch != nil {
		t.Error("a finished run should hand back a nil live channel")
	}
	if len(replay) != 1 || replay[0].Text != "a" {
		t.Fatalf("replay = %+v, want just [a] (post-close publish dropped)", replay)
	}
}

// Close closes all live subscriber channels so their SSE handlers end.
func TestCloseClosesLiveSubscribers(t *testing.T) {
	h := New()
	_, ch, _, cancel := h.Subscribe("r")
	defer cancel()
	h.Close("r")
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel should be closed after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("live subscriber channel was not closed by Close")
	}
}

// After the grace window, a closed topic is reaped: its retained buffer is freed, so a later subscribe
// starts fresh (empty replay, live again under a brand-new topic).
func TestReapAfterGrace(t *testing.T) {
	h := newHub(30 * time.Millisecond)
	h.Publish("r", txt("a"))
	h.Close("r")

	// Before reap: replay still available.
	if replay, _, _, c := h.Subscribe("r"); len(replay) != 1 {
		c()
		t.Fatalf("pre-reap replay = %d, want 1", len(replay))
	} else {
		c()
	}

	time.Sleep(80 * time.Millisecond) // let the reap timer fire

	replay, _, done, cancel := h.Subscribe("r")
	defer cancel()
	if done || len(replay) != 0 {
		t.Fatalf("after reap the topic should be fresh: done=%v replay=%d", done, len(replay))
	}
}
