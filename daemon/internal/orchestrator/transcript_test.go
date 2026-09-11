package orchestrator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type failingTranscriptAdapter struct {
	*fakeAdapter
	events []agent.Event
}

func (f *failingTranscriptAdapter) RunStream(_ context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	for _, event := range f.events {
		emit(event)
	}
	const failure = "Deployment not found (HTTP 404)"
	emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{IsError: true, Text: failure}})
	return agent.TurnResult{IsError: true, Text: failure}, nil
}

func TestFailedTurnPersistsPartialComponentsAndItsError(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "failure-empty"
		if partial {
			name = "failure-partial"
		}
		t.Run(name, func(t *testing.T) {
			fake := &failingTranscriptAdapter{fakeAdapter: &fakeAdapter{id: name}}
			if partial {
				fake.events = []agent.Event{
					{Type: agent.EventThinking, ItemID: "reason", Text: "Checking configuration", Delta: true},
					{Type: agent.EventToolUse, Tool: &agent.ToolEvent{ID: "tool", Name: "lookup"}},
					{Type: agent.EventToolResult, Tool: &agent.ToolEvent{ID: "tool", Name: "lookup", Output: "Missing deployment", IsError: true}},
					{Type: agent.EventText, ItemID: "answer", Text: "Partial answer", Delta: true},
				}
			}
			agent.Register(fake)
			path := filepath.Join(t.TempDir(), "state.json")
			store, err := session.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			s := New(store, stream.New(), notify.Fanout(nil), t.TempDir(), name)
			ag, _ := s.Resolve(name)
			run, ok := s.StartTurn(ag, StartTurnInput{ChatID: "chat", Message: "Run it"})
			if !ok {
				t.Fatal("turn was not accepted")
			}
			drainResolve(t, s, run.ID)
			reopened, err := session.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			saved, _ := reopened.GetRun(run.ID)
			messages := reopened.Messages("chat")
			if saved.Status != "error" || saved.ReplyID == "" || len(messages) != 2 || messages[1].ID != saved.ReplyID {
				t.Fatalf("error has no persisted reply: run=%+v messages=%+v", saved, messages)
			}
			parts := messages[1].Parts
			if last := parts[len(parts)-1]; last.Interaction == nil || last.Interaction.Kind != "error" || last.Interaction.Detail != saved.Error {
				t.Fatalf("error missing from chat: %+v", parts)
			}
			if partial && (len(parts) != 4 || parts[0].Type != "thinking" || parts[1].Tool.Output != "Missing deployment" || parts[2].Text != "Partial answer" || messages[1].Text != "Partial answer") {
				t.Fatalf("partial components lost: %+v", messages[1])
			}
		})
	}
}
