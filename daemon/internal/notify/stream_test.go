package notify

import (
	"context"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestStreamDeliversLiveAndReplay(t *testing.T) {
	s := NewStream()
	ctx := context.Background()

	// A notification sent before subscribing should appear in the replay buffer.
	_ = s.Notify(ctx, agent.Notification{Condition: agent.Finished, Title: "first"})

	ch, replay, cancel := s.Subscribe()
	defer cancel()
	if len(replay) != 1 || replay[0].Title != "first" {
		t.Fatalf("replay = %+v, want [first]", replay)
	}

	// A notification sent after subscribing should arrive live.
	_ = s.Notify(ctx, agent.Notification{Condition: agent.WaitingApproval, Title: "second"})
	select {
	case n := <-ch:
		if n.Title != "second" || n.Condition != agent.WaitingApproval {
			t.Fatalf("live = %+v, want second/waiting_approval", n)
		}
	default:
		t.Fatal("expected a live notification on the channel")
	}
}

func TestStreamUnsubscribeStopsDelivery(t *testing.T) {
	s := NewStream()
	ch, _, cancel := s.Subscribe()
	cancel()
	// channel is closed by cancel
	if _, open := <-ch; open {
		t.Fatal("expected channel closed after unsubscribe")
	}
}
