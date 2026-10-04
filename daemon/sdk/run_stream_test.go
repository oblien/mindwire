package mindwire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/api"
	"github.com/oblien/mindwire/daemon/internal/session"
)

// Completion happens inside the consumer callback, after subscription but before
// Stream starts reading its live channel. No scheduler timing or sleeps required.
func TestStreamDrainsEventsWhenRunFinishesDuringReplay(t *testing.T) {
	for _, sentinel := range []bool{false, true} {
		name := "replay"
		if sentinel {
			name = "open sentinel"
		}
		t.Run(name, func(t *testing.T) {
			c := newFakeClient(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			run, err := c.Turn(ctx, TurnRequest{ChatID: "stream-completion", Message: "partial"})
			if err != nil {
				t.Fatal(err)
			}
			// Make the first text event part of the next subscription's replay.
			for range run.Stream(ctx) {
				break
			}
			var options []StreamOption
			if sentinel {
				options = append(options, WithOpenSentinel())
			}
			var text string
			var sequence int64
			finished := false
			for event := range run.Stream(ctx, options...) {
				if !finished {
					finished = true
					if err := run.Cancel(); err != nil {
						t.Fatal(err)
					}
					c.core.sup.Wait()
				}
				if event.Type == EventText {
					text += event.Text
					if event.Sequence != sequence+1 {
						t.Fatalf("event sequence jumped from %d to %d", sequence, event.Sequence)
					}
					sequence = event.Sequence
				}
			}
			if err := ctx.Err(); err != nil {
				t.Fatal(err)
			}
			if text != "Partial reply" || sequence != 2 {
				t.Fatalf("completion dropped queued events: text=%q sequence=%d", text, sequence)
			}
		})
	}
}

type streamTestNotifier func(context.Context, Notification) error

func (f streamTestNotifier) Notify(ctx context.Context, n Notification) error { return f(ctx, n) }

type streamFlushRecorder struct {
	*httptest.ResponseRecorder
	onFlush func()
}

func (r *streamFlushRecorder) Flush() { r.ResponseRecorder.Flush(); r.onFlush() }

// A terminal status is persisted before notification delivery. Both transports
// must leave the producer alive and deliver its final event after replay.
func TestStreamIncludesEventsPublishedAfterTerminalStatus(t *testing.T) {
	for _, transport := range []string{"sdk", "http"} {
		t.Run(transport, func(t *testing.T) {
			reached, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			c := newFakeClient(t, streamTestNotifier(func(ctx context.Context, _ Notification) error {
				close(reached)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			run, err := c.Turn(ctx, TurnRequest{ChatID: "finishing", Message: "script"})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-reached:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if record, err := run.Refresh(); err != nil || record.Status != "done" {
				t.Fatalf("terminal record not saved before notification: %+v %v", record, err)
			}
			var events []Event
			if transport == "sdk" {
				for event := range run.Stream(ctx) {
					unblock()
					events = append(events, event)
				}
			} else {
				mux := http.NewServeMux()
				api.New(c.core.store, c.core.hub, c.core.sup).Register(mux)
				response := &streamFlushRecorder{ResponseRecorder: httptest.NewRecorder(), onFlush: unblock}
				mux.ServeHTTP(response, httptest.NewRequestWithContext(ctx, "GET", "/runs/"+run.ID()+"/stream", nil))
				if response.Code != http.StatusOK {
					t.Fatalf("stream HTTP %d", response.Code)
				}
				for _, line := range strings.Split(response.Body.String(), "\n") {
					if data, ok := strings.CutPrefix(line, "data: "); ok {
						var event Event
						if err := json.Unmarshal([]byte(data), &event); err != nil {
							t.Fatal(err)
						}
						events = append(events, event)
					}
				}
			}
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
			if len(events) == 0 {
				t.Fatal("stream returned no events")
			}
			last := events[len(events)-1]
			if last.Type != EventStatus || last.Meta["notify"] != "sent ✓" || last.Replay || last.Sequence != 5 {
				t.Fatalf("stream lost its post-terminal event: %+v", events)
			}
		})
	}
}

func TestStreamClosesAfterRestartWithoutCreatingLiveTopic(t *testing.T) {
	c := newFakeClient(t, nil)
	if err := c.core.store.SaveRun(session.Run{ID: "restored", ChatID: "chat", Agent: "fake", Status: "done"}); err != nil {
		t.Fatal(err)
	}
	run, err := c.Run("restored")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for event := range run.Stream(ctx) {
		t.Fatalf("restored stream invented an event: %+v", event)
	}
	if ctx.Err() != nil {
		t.Fatal("restored stream waited on a topic with no producer")
	}
	replay, ch, done, cancelSubscription := run.Subscribe()
	defer cancelSubscription()
	if len(replay) != 0 || ch != nil || !done {
		t.Fatal("restored run subscription did not terminate")
	}
	if _, retained := c.core.hub.Snapshot(run.ID()); retained {
		t.Fatal("restored stream created a live topic")
	}
}
