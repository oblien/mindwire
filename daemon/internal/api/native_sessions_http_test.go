package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func registryAuthenticated(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
}

func putNativeProject(t *testing.T, h http.Handler, id, cwd string) registry.Snapshot {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"record": map[string]any{"name": "Existing project", "path": cwd}})
	response := serve(t, h, "PUT", "/workspace/projects/"+id, string(body))
	if response.Code != 200 {
		t.Fatalf("project: %d %s", response.Code, response.Body.String())
	}
	var snapshot registry.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestNativeClaudeDiscoveryWorkspaceHistoryAndExternalContinuation(t *testing.T) {
	h, store, _ := newRegistryAPIEnv(t)
	h = registryAuthenticated(h)
	cwd := t.TempDir()
	path := claudeTranscriptPath(os.Getenv("CLAUDE_CONFIG_DIR"), cwd, "native-claude-session")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	line := func(id, role, text string) string {
		data, _ := json.Marshal(map[string]any{"type": role, "uuid": id, "cwd": cwd, "sessionId": "native-claude-session", "timestamp": "2026-09-24T00:00:00Z", "message": map[string]any{"role": role, "content": text}})
		return string(data) + "\n"
	}
	if err := os.WriteFile(path, []byte(line("native-user", "user", "Started in Claude Code")+line("native-answer", "assistant", "Native history")), 0600); err != nil {
		t.Fatal(err)
	}
	first := putNativeProject(t, h, "project", cwd)
	if len(first.Chats) != 1 || len(first.Agents) != 1 || first.Agents[0].AgentType != "claude-code" {
		t.Fatalf("native project was empty: %+v", first)
	}
	chat := first.Chats[0]
	if chat.SessionID != "native-claude-session" || len(store.Messages(chat.ID)) != 0 {
		t.Fatal("did not proxy native identity/history")
	}
	response := serve(t, h, "GET", "/chats/"+chat.ID+"/messages?limit=1", "")
	var history []agent.Message
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &history) != nil || len(history) != 1 || history[0].Text != "Native history" {
		t.Fatalf("native history: %d %s", response.Code, response.Body.String())
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(line("outside-user", "user", "Continued outside the app") + `{"type":"custom-title","sessionId":"native-claude-session","customTitle":"Renamed in Claude"}` + "\n")
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{"since": {strconv.FormatInt(first.Revision, 10)}, "workspaceId": {first.WorkspaceID}, "refresh": {"true"}}
	response = serve(t, h, "GET", "/workspace/changes?"+query.Encode(), "")
	var delta registry.Snapshot
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &delta) != nil || len(delta.Chats) != 1 || delta.Chats[0].ID != chat.ID || delta.Chats[0].Title != "Renamed in Claude" {
		t.Fatalf("native refresh: %d %s", response.Code, response.Body.String())
	}
	response = serve(t, h, "GET", "/chats?cwd="+url.QueryEscape(cwd), "")
	var chats []session.ChatSummary
	if json.Unmarshal(response.Body.Bytes(), &chats) != nil || len(chats) != 1 || chats[0].ChatID != chat.ID || chats[0].Title != "Renamed in Claude" {
		t.Fatalf("scoped list: %s", response.Body.String())
	}
	response = serve(t, h, "GET", "/chats?cwd="+url.QueryEscape(cwd+"-other"), "")
	if response.Body.String() != "[]\n" {
		t.Fatalf("sibling folder leaked chats: %s", response.Body.String())
	}
	response = serve(t, h, "GET", "/chats/"+chat.ID+"/messages", "")
	if !strings.Contains(response.Body.String(), "Continued outside the app") {
		t.Fatal("history did not read the updated native transcript")
	}
	if len(store.Messages(chat.ID)) != 0 {
		t.Fatal("history read copied messages into the daemon store")
	}
}

type nativeResumeFixture struct {
	resolveFakeAdapter
	cwd    string
	inputs chan agent.TurnInput
}

func (f nativeResumeFixture) Capabilities() agent.Capabilities {
	return agent.Capabilities{History: agent.SupportNative, Sessions: agent.SupportNative}
}
func (f nativeResumeFixture) ListSessions(_ context.Context, cwd string) ([]agent.NativeSession, error) {
	if cwd != f.cwd {
		return nil, nil
	}
	return []agent.NativeSession{{ID: "native-external-session", CWD: cwd, Title: "Existing CLI conversation", CreatedAt: "2026-09-24T00:00:00Z", UpdatedAt: "2026-09-25T00:00:00Z"}}, nil
}
func (f nativeResumeFixture) History(q agent.HistoryQuery) ([]agent.Message, error) {
	if q.SessionID != "native-external-session" || q.CWD != f.cwd {
		return nil, nil
	}
	return []agent.Message{{ID: "native-message", ChatID: q.ChatID, Role: "user", Text: "Already in the harness", CreatedAt: "2026-09-24T00:00:00Z"}}, nil
}
func (f nativeResumeFixture) RunStream(_ context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	f.inputs <- in
	emit(agent.Event{Type: agent.EventSession, SessionID: in.SessionID})
	emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "Resumed", SessionID: in.SessionID}})
	return agent.TurnResult{Text: "Resumed", SessionID: in.SessionID}, nil
}

func TestDiscoveredChatResumesExactNativeIDWithProjectContext(t *testing.T) {
	cwd := t.TempDir()
	// Match the canonical path used by project ownership, including /private/var on macOS.
	cwd, _ = filepath.EvalSymlinks(cwd)
	f := nativeResumeFixture{resolveFakeAdapter: resolveFakeAdapter{id: "native-resume-fixture"}, cwd: cwd, inputs: make(chan agent.TurnInput, 1)}
	agent.Register(f)
	h, store, a := newRegistryAPIEnv(t)
	h = registryAuthenticated(h)
	first := putNativeProject(t, h, "project", cwd)
	if len(first.Chats) != 1 {
		t.Fatalf("discovery: %+v", first)
	}
	chat := first.Chats[0]
	response := serve(t, h, "GET", "/chats/"+chat.ID+"/messages", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Already in the harness") {
		t.Fatalf("saved harness was not selected: %s", response.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"chatId": chat.ID, "message": "Continue from the phone", "cwd": "/wrong/client/directory"})
	response = serve(t, h, "POST", "/turns", string(body))
	if response.Code != 202 {
		t.Fatalf("resume: %d %s", response.Code, response.Body.String())
	}
	var run session.Run
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	select {
	case in := <-f.inputs:
		if in.SessionID != "native-external-session" || in.CWD != cwd {
			t.Fatalf("wrong native resume/context: %+v", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native harness did not receive resume")
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.sup.Busy(chat.ID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.sup.Busy(chat.ID) {
		a.sup.Cancel(run.ID)
		t.Fatal("resume did not finish")
	}
	if store.Session(f.ID(), chat.ID) != "native-external-session" {
		t.Fatal("resume created a second session")
	}
}
