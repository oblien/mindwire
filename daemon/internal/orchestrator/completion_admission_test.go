package orchestrator

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

type heldCompletionNotification struct {
	entered, release chan struct{}
	calls            atomic.Int32
}

func (n *heldCompletionNotification) Notify(ctx context.Context, notification agent.Notification) error {
	if n.calls.Add(1) == 1 {
		close(n.entered)
		select {
		case <-n.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestNextTurnCanStartWhilePreviousCompletionNotificationIsPending(t *testing.T) {
	var calls atomic.Int32
	s, a, _ := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		calls.Add(1)
		return agent.TurnResult{Text: "Done"}, nil
	})
	notifier := &heldCompletionNotification{entered: make(chan struct{}), release: make(chan struct{})}
	s.notifier = notifier
	defer close(notifier.release)
	first, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "First", RequestID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	inputSignal(t, notifier.entered)
	if run, _ := s.store.GetRun(first.ID); run.Status != "done" {
		t.Fatal("first did not complete")
	}
	second, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "Next", RequestID: "second"})
	if err != nil || first.ID == second.ID {
		t.Fatal("finished notification blocked the next turn", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatal("second native turn did not start")
	}
}

func TestStopThenImmediateFreshSendKeepsOneNativeWriter(t *testing.T) {
	var concurrent, maximum atomic.Int32
	s, a, _ := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		n := concurrent.Add(1)
		defer concurrent.Add(-1)
		if n > maximum.Load() {
			maximum.Store(n)
		}
		<-ctx.Done()
		return agent.TurnResult{Cancelled: true}, nil
	})
	for i := range 25 {
		run, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "Work", RequestID: fmt.Sprint("turn-", i)})
		if err != nil {
			t.Fatal("stopped turn still blocked admission", err)
		}
		s.Cancel(run.ID)
		deadline := time.Now().Add(3 * time.Second)
		for {
			state, _ := s.store.GetRun(run.ID)
			if state.Status == "cancelled" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("cancel did not settle")
			}
			time.Sleep(time.Millisecond)
		}
	}
	s.Wait()
	if maximum.Load() > 1 || concurrent.Load() != 0 {
		t.Fatal("overlapping native writers")
	}
}
