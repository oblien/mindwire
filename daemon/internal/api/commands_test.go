package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type commandsHTTPAdapter struct {
	resolveFakeAdapter
	turns    atomic.Int32
	compacts atomic.Int32
}

func (a *commandsHTTPAdapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{Protocol: agent.ProtocolCLI, Commands: true, CompactNow: true}
}
func (a *commandsHTTPAdapter) Commands(_ context.Context, q agent.CommandQuery) (agent.CommandCatalog, error) {
	return agent.CommandCatalog{Commands: []agent.Command{
		{Name: "permissions", Label: "Permissions", Kind: "settings"},
		{Name: "review", Label: "Review", Kind: "prompt", Input: "$review", SkillPath: filepath.Join(q.CWD, "SKILL.md"), AcceptsArguments: true},
	}}, nil
}
func (a *commandsHTTPAdapter) RunStream(_ context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	a.turns.Add(1)
	result := agent.TurnResult{Text: in.Message, SessionID: "native-session"}
	emit(agent.Event{Type: agent.EventSession, SessionID: result.SessionID})
	emit(agent.Event{Type: agent.EventText, Text: result.Text})
	return result, nil
}
func (a *commandsHTTPAdapter) Compact(_ context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	a.compacts.Add(1)
	emit(agent.Event{Type: agent.EventCompaction, Compaction: &agent.CompactionInfo{Trigger: "manual", Summary: "Saved decisions"}})
	return agent.TurnResult{SessionID: "native-session", Text: "Compacted"}, nil
}

func TestCommandsHTTPDispatchAndCompactionReceiptsSurviveRestart(t *testing.T) {
	fake := &commandsHTTPAdapter{resolveFakeAdapter: resolveFakeAdapter{id: t.Name()}}
	agent.Register(fake)
	state, cwd := filepath.Join(t.TempDir(), "state.json"), t.TempDir()
	open := func() (http.Handler, *orchestrator.Supervisor, *session.Store) {
		store, err := session.Open(state)
		if err != nil {
			t.Fatal(err)
		}
		hub := stream.New()
		sup := orchestrator.New(store, hub, notify.Fanout(nil), cwd, fake.ID())
		mux := http.NewServeMux()
		New(store, hub, sup).Register(mux)
		return mux, sup, store
	}
	handler, sup, store := open()
	catalog := serve(t, handler, "GET", "/commands", "")
	if catalog.Code != 200 || !strings.Contains(catalog.Body.String(), `"kind":"settings"`) || strings.Contains(catalog.Body.String(), "SKILL.md") || fake.turns.Load() != 0 {
		t.Fatalf("command discovery: %d %s", catalog.Code, catalog.Body.String())
	}
	body := `{"chatId":"chat","message":"","requestId":"command-receipt","options":{"command":{"name":"review","arguments":"latest"}}}`
	response := serve(t, handler, "POST", "/turns", body)
	var run session.Run
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &run) != nil {
		t.Fatalf("command: %d %s", response.Code, response.Body.String())
	}
	sup.Wait()
	completed, _ := store.GetRun(run.ID)
	if completed.Status != "done" || fake.turns.Load() != 1 {
		t.Fatalf("command failed: %+v", completed)
	}
	history := serve(t, handler, "GET", "/chats/chat/messages", "")
	if !strings.Contains(history.Body.String(), "$review latest") {
		t.Fatalf("native invocation was not dispatched: %s", history.Body.String())
	}
	compact := `{"requestId":"compact-receipt","instructions":"Keep decisions"}`
	response = serve(t, handler, "POST", "/chats/chat/compact", compact)
	var first session.Run
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &first) != nil {
		t.Fatalf("compact: %d %s", response.Code, response.Body.String())
	}
	sup.Wait()
	handler, sup, _ = open()
	defer sup.Wait()
	for _, call := range []struct{ path, body, id string }{{"/turns", body, run.ID}, {"/chats/chat/compact", compact, first.ID}} {
		response = serve(t, handler, "POST", call.path, call.body)
		var replay session.Run
		if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &replay) != nil || replay.ID != call.id {
			t.Fatalf("receipt after restart: %d %s", response.Code, response.Body.String())
		}
	}
	if fake.turns.Load() != 1 || fake.compacts.Load() != 1 {
		t.Fatal("retry duplicated native work")
	}
	response = serve(t, handler, "POST", "/turns", `{"chatId":"chat","message":"","options":{"command":{"name":"permissions"}}}`)
	if response.Code != 202 {
		t.Fatalf("control admission: %d %s", response.Code, response.Body.String())
	}
	sup.Wait()
	if fake.turns.Load() != 1 {
		t.Fatal("settings control reached a model turn")
	}
}
