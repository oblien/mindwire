package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type messageQuestionAdapter struct {
	*fakeAdapter
	run func(context.Context, agent.TurnInput, agent.Emit) (agent.TurnResult, error)
}

func (f *messageQuestionAdapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{Input: true, Respond: true, Cancel: true}
}
func (f *messageQuestionAdapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	return f.run(ctx, in, emit)
}

func messageQuestion() agent.Interaction {
	blocking := false
	return agent.Interaction{ID: "native:questions", Kind: "form", Title: "Your input", ResponseMode: "message", NeedsResponse: true, Blocking: &blocking,
		Questions: []agent.Question{
			{ID: "color", Title: "Which color?", AllowOther: true, Options: []agent.Action{{ID: "blue", Label: "Blue"}, {ID: "green", Label: "Green"}}},
			{ID: "format", Title: "Which format?", AllowOther: true, Options: []agent.Action{{ID: "plain", Label: "Plain text"}, {ID: "markdown", Label: "Markdown"}}},
		}}
}

func messageAnswer() agent.InteractionResponse {
	return agent.InteractionResponse{InteractionID: "native:questions", Answers: map[string]agent.QuestionAnswer{
		"color": {Options: []string{"blue"}, Text: "Keep it subtle"}, "format": {Options: []string{"markdown"}},
	}}
}

func questionSupervisor(t *testing.T, run func(context.Context, agent.TurnInput, agent.Emit) (agent.TurnResult, error)) (*Supervisor, *Agent, string) {
	t.Helper()
	fake := &messageQuestionAdapter{fakeAdapter: &fakeAdapter{id: t.Name()}, run: run}
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

func finishQuestionTurn(emit agent.Emit) (agent.TurnResult, error) {
	emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "Questions sent.", SessionID: "native-session"}})
	return agent.TurnResult{Text: "Questions sent.", SessionID: "native-session"}, nil
}

func TestAsyncQuestionSurvivesCompletionRestartAndResumesExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	received := make(chan agent.TurnInput, 2)
	s, a, path := questionSupervisor(t, func(_ context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		calls.Add(1)
		if in.Message == "ask" {
			it := messageQuestion()
			emit(agent.Event{Type: agent.EventInteraction, Interaction: &it})
		} else {
			received <- in
		}
		return finishQuestionTurn(emit)
	})
	run, ok := s.StartTurn(a, StartTurnInput{ChatID: "chat", Message: "ask"})
	if !ok {
		t.Fatal("not started")
	}
	s.Wait()
	snap, _ := s.Snapshot(run.ID)
	if len(snap.Parts) == 0 || !snap.Parts[0].Interaction.NeedsResponse || snap.Parts[0].Interaction.RunID != run.ID {
		t.Fatalf("lost question: %+v", snap)
	}
	store, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(store, stream.New(), notify.Fanout(nil), s.cwd, a.ID())
	t.Cleanup(restarted.Wait)
	if len(store.Messages("chat")) != 2 || !store.Messages("chat")[1].Parts[0].Interaction.NeedsResponse {
		t.Fatal("reopen lost pending form")
	}
	invalid := messageAnswer()
	delete(invalid.Answers, "format")
	if restarted.RespondInteraction(run.ID, invalid) == nil {
		t.Fatal("incomplete form accepted")
	}
	if err := restarted.RespondInteraction(run.ID, messageAnswer()); err != nil {
		t.Fatal(err)
	}
	restarted.Wait()
	input := <-received
	if input.SessionID != "native-session" || !strings.Contains(input.Message, "Which color?\nBlue\nFeedback: Keep it subtle") || !strings.Contains(input.Message, "Which format?\nMarkdown") {
		t.Fatalf("wrong native continuation: %+v", input)
	}
	if err := restarted.RespondInteraction(run.ID, messageAnswer()); err != nil {
		t.Fatal("lost HTTP ack was not idempotent:", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("answer started %d total turns", calls.Load())
	}
	final, _ := session.Open(path)
	record, _ := final.MessageQuestion(run.ID, messageAnswer().InteractionID)
	if record.Interaction.NeedsResponse || record.Interaction.Response == nil || record.ResponseRunID == run.ID || record.ResponseRunID == "" {
		t.Fatalf("answer not persisted: %+v", record)
	}
	snapshot, _ := restarted.Snapshot(run.ID)
	if snapshot.Parts[0].Interaction.NeedsResponse || snapshot.Parts[0].Interaction.Response == nil {
		t.Fatal("snapshot reopened answered choices")
	}
	forked := final.OverlayInteractions("fork", snapshot.Parts)
	if forked[0].Interaction.NeedsResponse || forked[0].Interaction.RunID != "" {
		t.Fatal("fork can answer source chat")
	}
}

func TestAsyncAnswerWaitsForNativeAckAndCanRetryARejection(t *testing.T) {
	ready, finish := make(chan struct{}), make(chan struct{})
	received, acknowledge := make(chan agent.Inbound, 2), make(chan error, 2)
	s, a, _ := questionSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		it := messageQuestion()
		emit(agent.Event{Type: agent.EventInteraction, Interaction: &it})
		close(ready)
		for {
			select {
			case input := <-in.Inbound:
				received <- input
				select {
				case err := <-acknowledge:
					input.Ack <- err
				case <-ctx.Done():
					return agent.TurnResult{}, ctx.Err()
				}
			case <-finish:
				return finishQuestionTurn(emit)
			case <-ctx.Done():
				return agent.TurnResult{}, ctx.Err()
			}
		}
	})
	run, _ := s.StartTurn(a, StartTurnInput{ChatID: "chat", Message: "ask"})
	<-ready
	result := make(chan error, 1)
	go func() { result <- s.RespondInteraction(run.ID, messageAnswer()) }()
	input := <-received
	if input.Kind != "input" || input.InteractionID != "" {
		t.Fatalf("invented native control request: %+v", input)
	}
	if err := s.RespondInteraction(run.ID, messageAnswer()); !errors.Is(err, ErrInteractionSending) {
		t.Fatalf("duplicate in flight accepted: %v", err)
	}
	if q, _ := s.store.MessageQuestion(run.ID, messageAnswer().InteractionID); !q.Interaction.NeedsResponse {
		t.Fatal("question resolved before ack")
	}
	acknowledge <- errors.New("native steer rejected")
	if err := <-result; err == nil {
		t.Fatal("native rejection ignored")
	}
	go func() { result <- s.RespondInteraction(run.ID, messageAnswer()) }()
	<-received
	acknowledge <- nil
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if q, _ := s.store.MessageQuestion(run.ID, messageAnswer().InteractionID); q.Interaction.NeedsResponse {
		t.Fatal("accepted answer not resolved")
	}
	close(finish)
	s.Wait()
	snapshot, _ := s.Snapshot(run.ID)
	if snapshot.Parts[0].Interaction.NeedsResponse {
		t.Fatal("terminal result revived the answered question")
	}
}

func TestAsyncAnswerRacingTurnEndIsResumedInsteadOfDropped(t *testing.T) {
	ready, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s, a, _ := questionSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		if calls.Add(1) == 1 {
			it := messageQuestion()
			emit(agent.Event{Type: agent.EventInteraction, Interaction: &it})
			close(ready)
			// Deliberately finish without reading the queued answer, as an adapter can
			// when its terminal event wins the select against inbound.
			select {
			case <-finish:
			case <-ctx.Done():
				return agent.TurnResult{}, ctx.Err()
			}
		} else if !strings.Contains(in.Message, "Blue") {
			t.Error("resumed without the answer")
		}
		return finishQuestionTurn(emit)
	})
	run, _ := s.StartTurn(a, StartTurnInput{ChatID: "chat", Message: "ask"})
	<-ready
	result := make(chan error, 1)
	go func() { result <- s.RespondInteraction(run.ID, messageAnswer()) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		queued := len(s.inputs[run.ID]) > 0
		s.mu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("answer was not queued")
		}
		time.Sleep(time.Millisecond)
	}
	close(finish)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("answer hung after completion")
	}
	s.Wait()
	if calls.Load() != 2 {
		t.Fatalf("expected one resumed turn, got %d", calls.Load())
	}
}

func TestEachAsyncQuestionNotifiesOnceAndCompletionDoesNotCoverIt(t *testing.T) {
	s, a, _ := questionSupervisor(t, func(_ context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		one := messageQuestion()
		emit(agent.Event{Type: agent.EventInteraction, Interaction: &one})
		emit(agent.Event{Type: agent.EventInteraction, Interaction: &one})
		two := messageQuestion()
		two.ID = "next:questions"
		two.Questions = two.Questions[1:]
		emit(agent.Event{Type: agent.EventInteraction, Interaction: &two})
		return finishQuestionTurn(emit)
	})
	notes, _, cancel := s.Notes().Subscribe()
	defer cancel()
	run, _ := s.StartTurn(a, StartTurnInput{ChatID: "chat", Message: "ask"})
	for i := 0; i < 2; i++ {
		select {
		case note := <-notes:
			if note.Condition != agent.WaitingFeedback || note.ChatID != "chat" || note.RunID != run.ID {
				t.Fatalf("wrong question notification: %+v", note)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a question did not notify")
		}
	}
	s.Wait()
	select {
	case note := <-notes:
		t.Fatalf("duplicate or completion covered question: %+v", note)
	default:
	}
}
