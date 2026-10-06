package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type queuedInputAdapter struct {
	*fakeAdapter
	run func(context.Context, agent.TurnInput, agent.Emit) (agent.TurnResult, error)
}

func (f *queuedInputAdapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{Input: true, QueuedInput: true, Cancel: true, Resume: true}
}
func (f *queuedInputAdapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	return f.run(ctx, in, emit)
}

func inputSupervisor(t *testing.T, fn func(context.Context, agent.TurnInput, agent.Emit) (agent.TurnResult, error)) (*Supervisor, *Agent, string) {
	t.Helper()
	fake := &queuedInputAdapter{fakeAdapter: &fakeAdapter{id: t.Name()}, run: fn}
	agent.Register(fake)
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, stream.New(), notify.Fanout(nil), t.TempDir(), fake.id)
	a, _ := s.Resolve(fake.id)
	t.Cleanup(func() {
		s.mu.Lock()
		for _, cancel := range s.cancels {
			cancel()
		}
		s.mu.Unlock()
		s.Wait()
	})
	return s, a, path
}

func queuePrompt(t *testing.T, s *Supervisor, a *Agent, id, text string) session.Run {
	t.Helper()
	run, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", RequestID: id, Message: text, QueueIfRunning: true})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func inputSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("native input fixture did not advance")
	}
}

func TestQueuedInputNativeDeliveryAndLostHTTPReceipt(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var delivered []string
	s, a, path := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		calls.Add(1)
		emit(agent.Event{Type: agent.EventSession, SessionID: "native-session"})
		emit(agent.Event{Type: agent.EventText, Text: "Before follow-ups"})
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return agent.TurnResult{Cancelled: true}, nil
		}
		for range 2 {
			input := <-in.Inbound
			delivered = append(delivered, input.Text)
			agent.AcknowledgeInput(input, nil, emit)
		}
		emit(agent.Event{Type: agent.EventText, Text: "After follow-ups"})
		emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "After follow-ups"}})
		return agent.TurnResult{Text: "After follow-ups", SessionID: "native-session"}, nil
	})
	initial := queuePrompt(t, s, a, "initial", "Start")
	inputSignal(t, started)
	one := queuePrompt(t, s, a, "follow-one", "Do this next")
	two := queuePrompt(t, s, a, "follow-two", "Then test it")
	if one.ID != initial.ID || two.ID != initial.ID || len(two.Inputs) != 2 {
		t.Fatal("follow-up created another run")
	}
	if replay := queuePrompt(t, s, a, "follow-one", "Do this next"); len(replay.Inputs) != 2 {
		t.Fatal("retry duplicated delivery")
	}
	if _, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", RequestID: "follow-one", Message: "Different", QueueIfRunning: true}); !errors.Is(err, session.ErrTurnRequestConflict) {
		t.Fatalf("conflicting receipt: %v", err)
	}
	snapshot, _ := s.Snapshot(initial.ID)
	if len(snapshot.Run.Inputs) != 2 || snapshot.Run.Inputs[0].Status != "queued" {
		t.Fatal("queued messages not restorable")
	}
	close(release)
	events := drainResolve(t, s, initial.ID)
	s.Wait()
	if calls.Load() != 1 || !reflect.DeepEqual(delivered, []string{"Do this next", "Then test it"}) {
		t.Fatalf("native deliveries: %d %#v", calls.Load(), delivered)
	}
	results := 0
	for _, event := range events {
		if event.Type == agent.EventResult {
			results++
		}
	}
	if results != 1 {
		t.Fatalf("got %d public terminal results", results)
	}
	reopened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := reopened.GetRun(initial.ID)
	if stored.Status != "done" || len(stored.Inputs) != 2 || stored.Inputs[1].Status != "accepted" {
		t.Fatalf("receipts lost: %+v", stored)
	}
	var roles, texts []string
	for _, message := range reopened.Messages("chat") {
		roles = append(roles, message.Role)
		texts = append(texts, message.Text)
	}
	if !reflect.DeepEqual(roles, []string{"user", "assistant", "user", "user", "assistant"}) {
		t.Fatalf("interleaved history: %#v %#v", roles, texts)
	}
	if replay := queuePrompt(t, s, a, "follow-one", "Do this next"); replay.ID != initial.ID || calls.Load() != 1 {
		t.Fatal("post-completion retry started a turn")
	}
	// A fresh hub restores the same user boundaries from the durable segments.
	fresh := New(reopened, stream.New(), notify.Fanout(nil), s.cwd, a.ID())
	restored, _ := fresh.Snapshot(initial.ID)
	users := 0
	for _, part := range restored.Parts {
		if part.Type == "user" {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("restart lost input boundaries: %+v", restored.Parts)
	}
}

func TestQueuedInputRacingCompletionContinuesOnceInSameSession(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	seen := make(chan agent.TurnInput, 2)
	s, a, _ := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		n := calls.Add(1)
		seen <- in
		emit(agent.Event{Type: agent.EventSession, SessionID: "same-native-session"})
		if n == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return agent.TurnResult{Cancelled: true}, nil
			}
		} else {
			input := <-in.Inbound
			agent.AcknowledgeInput(input, nil, emit)
		}
		emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "Finished"}})
		return agent.TurnResult{Text: "Finished", SessionID: "same-native-session"}, nil
	})
	first := queuePrompt(t, s, a, "initial", "First")
	inputSignal(t, started)
	queuePrompt(t, s, a, "next", "Second")
	queuePrompt(t, s, a, "another", "Third")
	close(release)
	events := drainResolve(t, s, first.ID)
	s.Wait()
	<-seen
	continued := <-seen
	if calls.Load() != 2 || continued.Message != "Second" || continued.SessionID != "same-native-session" {
		t.Fatalf("wrong continuation: %d %+v", calls.Load(), continued)
	}
	results := 0
	for _, event := range events {
		if event.Type == agent.EventResult {
			results++
		}
	}
	if results != 1 {
		t.Fatalf("completion escaped before the queue drained: %d", results)
	}
	saved, _ := s.store.GetRun(first.ID)
	if saved.Status != "done" || saved.Inputs[0].Status != "accepted" || saved.Inputs[1].Status != "accepted" {
		t.Fatalf("queue did not settle: %+v", saved)
	}
}

func TestQueuedInputStopAndAmbiguousDeliveryNeverAutoResend(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		t.Run(map[bool]string{true: "stop", false: "lost-native-ack"}[cancelled], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			s, a, _ := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
				calls.Add(1)
				close(started)
				if cancelled {
					<-ctx.Done()
				} else {
					<-release
					<-in.Inbound
				}
				emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "Stopped"}})
				return agent.TurnResult{Text: "Stopped"}, nil
			})
			first := queuePrompt(t, s, a, "initial", "First")
			inputSignal(t, started)
			queuePrompt(t, s, a, "follow", "Keep this message")
			if cancelled {
				s.Cancel(first.ID)
			} else {
				close(release)
			}
			drainResolve(t, s, first.ID)
			s.Wait()
			saved, _ := s.store.GetRun(first.ID)
			want := "failed"
			if cancelled {
				want = "cancelled"
			}
			if calls.Load() != 1 || saved.Inputs[0].Status != want || saved.Inputs[0].Text != "Keep this message" {
				t.Fatalf("unsafe retry or lost draft: %d %+v", calls.Load(), saved)
			}
			if replay := queuePrompt(t, s, a, "follow", "Keep this message"); replay.ID != first.ID || calls.Load() != 1 {
				t.Fatal("ambiguous receipt was replayed")
			}
		})
	}
}

func TestQueuedInputConcurrentRetriesHaveOneDelivery(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var received atomic.Int32
	s, a, _ := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return agent.TurnResult{Cancelled: true}, nil
		}
		for {
			select {
			case input := <-in.Inbound:
				received.Add(1)
				agent.AcknowledgeInput(input, nil, emit)
			default:
				return agent.TurnResult{Text: "Done"}, nil
			}
		}
	})
	first := queuePrompt(t, s, a, "initial", "First")
	inputSignal(t, started)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			_, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", RequestID: "same-retry", Message: "Only once", QueueIfRunning: true})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	drainResolve(t, s, first.ID)
	s.Wait()
	if received.Load() != 1 {
		t.Fatalf("duplicate sends: %d", received.Load())
	}
}

func TestQueuedInputNativeConsumptionWinsOverLostResponse(t *testing.T) {
	started := make(chan struct{})
	s, a, _ := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		close(started)
		select {
		case input := <-in.Inbound:
			echo := input
			echo.Ack = nil
			agent.AcknowledgeInput(echo, nil, emit)
			input.Ack <- errors.New("RPC response lost after the native user echo")
		case <-ctx.Done():
			return agent.TurnResult{Cancelled: true}, nil
		}
		return agent.TurnResult{Text: "Consumed"}, nil
	})
	first := queuePrompt(t, s, a, "initial", "First")
	inputSignal(t, started)
	queuePrompt(t, s, a, "follow", "Only once")
	events := drainResolve(t, s, first.ID)
	s.Wait()
	for _, event := range events {
		if event.Input != nil && event.Input.Status == "failed" {
			t.Fatal("native consumption was contradicted by a later transport error")
		}
	}
	saved, _ := s.store.GetRun(first.ID)
	if len(saved.Inputs) != 1 || saved.Inputs[0].Status != "accepted" {
		t.Fatalf("native acceptance lost: %+v", saved.Inputs)
	}
}

func TestQueuedInputCapacityAndSettingsFailuresDoNotAdmitMessages(t *testing.T) {
	started := make(chan struct{})
	s, a, _ := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		close(started)
		<-ctx.Done()
		return agent.TurnResult{Cancelled: true}, nil
	})
	first := queuePrompt(t, s, a, "initial", "First")
	inputSignal(t, started)
	if _, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "No receipt", QueueIfRunning: true}); !errors.Is(err, ErrInvalidTurnRequest) {
		t.Fatalf("missing receipt was admitted: %v", err)
	}
	if _, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", RequestID: "settings", Message: "Change model", QueueIfRunning: true,
		Options: agent.TurnOptions{Settings: map[string]string{"model": "different"}}}); !errors.Is(err, ErrInputOptions) {
		t.Fatalf("active settings change was admitted: %v", err)
	}
	for i := range 16 {
		queuePrompt(t, s, a, fmt.Sprintf("input-%d", i), "Pending")
	}
	if _, err := s.SendInputChecked(first.ID, "Overflow", "overflow", nil); !errors.Is(err, ErrInputQueueFull) {
		t.Fatalf("queue exceeded its bound: %v", err)
	}
	saved, _ := s.store.GetRun(first.ID)
	if len(saved.Inputs) != 16 {
		t.Fatalf("failed admissions changed the queue: %+v", saved.Inputs)
	}
	s.Cancel(first.ID)
	drainResolve(t, s, first.ID)
	s.Wait()
}
