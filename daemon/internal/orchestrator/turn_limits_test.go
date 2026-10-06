package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestTurnSurvivesWeeksWithoutAViewerAndReattaches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		finish := make(chan struct{})
		calls := 0
		s, a, _ := inputSupervisor(t, func(ctx context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
			calls++
			emit(agent.Event{Type: agent.EventSession, SessionID: "long-native-session"})
			emit(agent.Event{Type: agent.EventText, Text: "Working in the background"})
			select {
			case <-finish:
				emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "Finished after reconnect"}})
				return agent.TurnResult{Text: "Finished after reconnect"}, nil
			case <-ctx.Done():
				return agent.TurnResult{}, errors.New("signal: killed")
			}
		})
		run := queuePrompt(t, s, a, "start", "Keep working")
		synctest.Wait()
		_, _, _, disconnect := s.hub.Subscribe(run.ID)
		disconnect()
		// Fake time exercises the real supervisor/context and persistence paths,
		// without a long wall-clock test or replacing the cancellation behavior.
		for _, duration := range []time.Duration{31 * time.Minute, 24 * 24 * time.Hour} {
			time.Sleep(duration)
			synctest.Wait()
			current, _ := s.store.GetRun(run.ID)
			if current.Status != "running" || current.Error != "" {
				t.Fatalf("viewer disconnect or elapsed time killed the native turn: %+v", current)
			}
		}
		replay, _, done, disconnect := s.hub.Subscribe(run.ID)
		defer disconnect()
		if done || len(replay) == 0 || s.store.Session(a.ID(), run.ChatID) != "long-native-session" {
			t.Fatal("reconnecting viewer lost the live transcript or native session")
		}
		close(finish)
		s.Wait()
		current, _ := s.store.GetRun(run.ID)
		if calls != 1 || current.Status != "done" || current.ReplyID == "" {
			t.Fatalf("reattachment restarted or lost the turn: calls=%d run=%+v", calls, current)
		}
	})
}

type longResolveAdapter struct{ *fakeAdapter }

func (f longResolveAdapter) RunStream(ctx context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	f.calls++
	select {
	case <-time.After(90 * time.Minute):
		emit(agent.Event{Type: agent.EventText, Text: "Long step completed"})
		return agent.TurnResult{Text: "Long step completed " + resolveSentinel}, nil
	case <-ctx.Done():
		return agent.TurnResult{}, ctx.Err()
	}
}

func TestResolveLongStepKeepsItsRequestedOverallBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeAdapter{id: t.Name()}
		s := newResolveSup(t, fake)
		agent.Register(longResolveAdapter{fake})
		s = New(s.store, s.hub, s.notifier, s.cwd, fake.id)
		a, _ := s.Resolve(fake.id)
		run, err := s.StartResolveChecked(a, StartTurnInput{ChatID: "resolve-chat", Message: "Long task"}, ResolveOptions{Deadline: 4 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		s.Wait()
		completed, _ := s.store.GetRun(run.ID)
		if completed.Status != "done" || completed.StopReason != "done" || fake.calls != 1 {
			t.Fatalf("hidden per-step timer cut off the requested resolve budget: %+v calls=%d", completed, fake.calls)
		}
	})
}

func TestConfiguredTurnLimitExplainsFailureAndResumesSameSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, a, _ := inputSupervisor(t, func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
			if in.Message == "Continue" {
				if in.SessionID != "saved-native-session" {
					t.Errorf("retry did not resume the saved session: %q", in.SessionID)
				}
				return agent.TurnResult{Text: "Continued safely"}, nil
			}
			emit(agent.Event{Type: agent.EventSession, SessionID: "saved-native-session"})
			emit(agent.Event{Type: agent.EventText, Text: "Partial work is retained"})
			<-ctx.Done()
			// Some adapters call any interrupted context "cancelled". The deadline
			// must still surface as an error in the stream and persisted run.
			emit(agent.Event{Type: agent.EventError, Error: "signal: killed"})
			emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Cancelled: true}})
			return agent.TurnResult{Cancelled: true}, nil
		}, WithTurnTimeout(time.Hour))
		run := queuePrompt(t, s, a, "limited", "Work")
		synctest.Wait()
		time.Sleep(time.Hour)
		s.Wait()
		current, _ := s.store.GetRun(run.ID)
		if current.Status != "error" || !strings.Contains(current.Error, "TURN_TIMEOUT") || current.ReplyID == "" {
			t.Fatalf("deadline was not an actionable, saved failure: %+v", current)
		}
		events := drainResolve(t, s, run.ID)
		result := lastResult(events)
		if result == nil || !result.IsError || result.Cancelled || result.Text != current.Error {
			t.Fatalf("stream and saved outcome disagree: %+v", result)
		}
		for _, event := range events {
			if strings.Contains(event.Error, "signal: killed") {
				t.Fatal("raw kill error leaked before the configured timeout explanation")
			}
		}
		retry := queuePrompt(t, s, a, "retry", "Continue")
		s.Wait()
		continued, _ := s.store.GetRun(retry.ID)
		if continued.Status != "done" || s.store.Session(a.ID(), run.ChatID) != "saved-native-session" {
			t.Fatal("deadline lost the conversation or left it locked")
		}
		messages := s.store.Messages(run.ChatID)
		if len(messages) != 4 || len(messages[1].Parts) == 0 || messages[1].Parts[0].Text != "Partial work is retained" {
			t.Fatalf("retry lost partial history: %+v", messages)
		}
	})
}

func TestStopAfterLongTurnDoesNotKillOtherChatsOrReportFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, a, _ := inputSupervisor(t, func(ctx context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
			emit(agent.Event{Type: agent.EventText, Text: "Saved work"})
			<-ctx.Done()
			emit(agent.Event{Type: agent.EventError, Error: "signal: killed"})
			return agent.TurnResult{}, errors.New("signal: killed")
		})
		first := queuePrompt(t, s, a, "first", "Work")
		second, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "another-chat", Message: "Independent work"})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		time.Sleep(48 * time.Hour)
		if !s.Cancel(first.ID) {
			t.Fatal("Stop no longer works for a long turn")
		}
		synctest.Wait()
		stopped, _ := s.store.GetRun(first.ID)
		other, _ := s.store.GetRun(second.ID)
		if stopped.Status != "cancelled" || stopped.Error != "" || stopped.ReplyID == "" || other.Status != "running" {
			t.Fatalf("Stop leaked a failure or cancelled another chat: stopped=%+v other=%+v", stopped, other)
		}
		for _, event := range drainResolve(t, s, first.ID) {
			if event.Type == agent.EventError || event.Result != nil && event.Result.IsError {
				t.Fatalf("Stop reported an error: %+v", event)
			}
		}
		s.Cancel(second.ID)
		s.Wait()
	})
}
