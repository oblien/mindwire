package orchestrator

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/gitaccess"
)

func TestGitAuthorizationAdmissionAndCancellation(t *testing.T) {
	input := make(chan agent.TurnInput, 1)
	s, a, state := questionSupervisor(t, func(ctx context.Context, in agent.TurnInput, _ agent.Emit) (agent.TurnResult, error) {
		input <- in
		<-ctx.Done()
		return agent.TurnResult{}, ctx.Err()
	})
	var closed atomic.Int32
	s.SetGitPreparation(func(_ context.Context, chat, cwd string, auth *gitaccess.Auth) (*gitaccess.Lease, error) {
		if auth == nil {
			return nil, gitaccess.ErrCredential
		}
		if chat != "chat" || auth.Token != "private-test-token" {
			t.Error("authorization did not reach run admission")
		}
		return &gitaccess.Lease{Environment: map[string]string{"GIT_CONFIG_COUNT": "4"}, Close: func() { closed.Add(1) }}, nil
	})
	if _, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "work"}); !errors.Is(err, gitaccess.ErrCredential) {
		t.Fatalf("missing authorization: %v", err)
	}
	if len(s.store.Messages("chat")) != 0 || s.Busy("chat") {
		t.Fatal("rejected authorization left a message or busy chat")
	}
	run, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "work", GitAuth: &gitaccess.Auth{Kind: "token", Token: "private-test-token"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case in := <-input:
		if in.Env["GIT_CONFIG_COUNT"] != "4" || strings.Contains(in.Message, "private-test-token") {
			t.Fatal("helper did not reach the adapter separately from the message")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not reach the adapter")
	}
	if !s.Cancel(run.ID) {
		t.Fatal("could not cancel run")
	}
	s.Wait()
	if closed.Load() != 1 {
		t.Fatalf("cancel closed %d leases", closed.Load())
	}
	data, err := os.ReadFile(state)
	if err != nil || strings.Contains(string(data), "private-test-token") {
		t.Fatal("run credentials reached durable state")
	}
}

func TestAsyncGitQuestionRequiresFreshAuthorizationWithoutConsumingFailedReply(t *testing.T) {
	var turns, closed atomic.Int32
	s, a, _ := questionSupervisor(t, func(_ context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
		turns.Add(1)
		if in.Env["GIT_CONFIG_COUNT"] != "4" {
			t.Error("resumed turn lost Git environment")
		}
		if in.Message == "ask" {
			question := messageQuestion()
			emit(agent.Event{Type: agent.EventInteraction, Interaction: &question})
		}
		return finishQuestionTurn(emit)
	})
	s.SetGitPreparation(func(_ context.Context, _, _ string, auth *gitaccess.Auth) (*gitaccess.Lease, error) {
		if auth == nil {
			return nil, gitaccess.ErrCredential
		}
		return &gitaccess.Lease{Environment: map[string]string{"GIT_CONFIG_COUNT": "4"}, Close: func() { closed.Add(1) }}, nil
	})
	auth := &gitaccess.Auth{Kind: "token", Token: "fixture"}
	run, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "ask", GitAuth: auth})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if closed.Load() != 1 {
		t.Fatal("completed question turn retained its credentials")
	}
	if err := s.RespondInteraction(run.ID, messageAnswer()); !errors.Is(err, gitaccess.ErrCredential) {
		t.Fatalf("resumed without fresh authorization: %v", err)
	}
	if err := s.RespondInteractionWithGitAuth(run.ID, messageAnswer(), auth); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if turns.Load() != 2 || closed.Load() != 2 {
		t.Fatalf("turns=%d closed=%d", turns.Load(), closed.Load())
	}
	if err := s.RespondInteractionWithGitAuth(run.ID, messageAnswer(), auth); err != nil {
		t.Fatal("a lost reply acknowledgement was not idempotent:", err)
	}
	if turns.Load() != 2 {
		t.Fatal("retry started another turn")
	}
}
