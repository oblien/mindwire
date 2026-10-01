package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/conversations"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func openGlobalNativeChat(t *testing.T, h http.Handler, a *API, harness string) registry.Snapshot {
	t.Helper()
	before, _ := a.registry.Snapshot(nil)
	response := serve(t, h, "GET", "/workspace/conversations?limit=50", "")
	var page conversations.Page
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil {
		t.Fatalf("global list: %d %s", response.Code, response.Body.String())
	}
	after, _ := a.registry.Snapshot(nil)
	if after.Revision != before.Revision || len(after.Projects) != len(before.Projects) || len(after.Chats) != len(before.Chats) {
		t.Fatal("global browsing changed project/chat membership")
	}
	var selected conversations.Conversation
	for _, row := range page.Items {
		if row.AgentType == harness {
			selected = row
			break
		}
	}
	if selected.ID == "" || selected.CWD == "" {
		t.Fatalf("missing native conversation: %+v", page)
	}
	body, _ := json.Marshal(conversations.OpenRequest{ID: selected.ID})
	var opened conversations.OpenResult
	for i := 0; i < 2; i++ {
		response = serve(t, h, "POST", "/workspace/conversations/open", string(body))
		var result conversations.OpenResult
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatalf("global open: %d %s", response.Code, response.Body.String())
		}
		if i > 0 && result.ChatID != opened.ChatID {
			t.Fatal("repeated open duplicated the native chat")
		}
		opened = result
	}
	return opened.Snapshot
}

func TestGlobalConversationHTTPBrowseOpenHistoryAndResume(t *testing.T) {
	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	f := nativeResumeFixture{resolveFakeAdapter: resolveFakeAdapter{id: "global-resume-fixture"}, cwd: cwd, inputs: make(chan agent.TurnInput, 1)}
	agent.Register(f)
	h, store, a := newRegistryAPIEnv(t)
	a.conversations = conversations.New(a.registry, store, []agent.Adapter{f}, &a.registryMu, a.sup.Busy)
	if response := serve(t, h, "GET", "/workspace/conversations", ""); response.Code != 401 {
		t.Fatal("native folder metadata was not authenticated")
	}
	h = registryAuthenticated(h)
	first := openGlobalNativeChat(t, h, a, f.ID())
	if len(first.Chats) != 1 || len(first.Projects) != 1 || first.Projects[0].Path != cwd {
		t.Fatal(first)
	}
	chat := first.Chats[0]
	response := serve(t, h, "GET", "/chats/"+chat.ID+"/messages?paged=true&limit=1", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Already in the harness") {
		t.Fatalf("original history: %d %s", response.Code, response.Body.String())
	}
	if len(store.Messages(chat.ID)) != 0 {
		t.Fatal("browser copied transcript messages")
	}
	body, _ := json.Marshal(map[string]any{"chatId": chat.ID, "message": "Continue from the global list", "cwd": "/wrong/client/directory"})
	response = serve(t, h, "POST", "/turns", string(body))
	if response.Code != 202 {
		t.Fatalf("resume: %d %s", response.Code, response.Body.String())
	}
	var run session.Run
	_ = json.Unmarshal(response.Body.Bytes(), &run)
	defer a.sup.Cancel(run.ID)
	select {
	case input := <-f.inputs:
		if input.CWD != cwd || input.SessionID != "native-external-session" {
			t.Fatalf("wrong native resume: %+v", input)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native session was not resumed")
	}
	for _, query := range []string{"?limit=-1", "?limit=101", "?cursor=invalid", "?agentId=missing"} {
		response = serve(t, h, "GET", "/workspace/conversations"+query, "")
		if response.Code != 400 && response.Code != 404 {
			t.Fatalf("invalid query accepted: %s %d", query, response.Code)
		}
	}
}
