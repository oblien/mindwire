package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

func newRegistryEnv(t *testing.T) (http.Handler, *session.Store) {
	h, st, _ := newRegistryAPIEnv(t)
	return h, st
}

func newRegistryAPIEnv(t *testing.T) (http.Handler, *session.Store, *API) {
	t.Helper()
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	st, err := session.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Open(filepath.Join(dir, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	hub := stream.New()
	sup := orchestrator.New(st, hub, notify.Fanout(nil), dir, "claude-code")
	mux := http.NewServeMux()
	api := New(st, hub, sup, reg)
	if err := api.InitError(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	api.Register(mux)
	return Auth("workspace-secret", mux), st, api
}

// Requests go through the real auth/HTTP/SQLite boundary; the second client uses only snapshots.
func TestWorkspaceHTTPMigrationAndCrossClientLifecycle(t *testing.T) {
	h, st := newRegistryEnv(t)
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	if got := serve(t, h, "GET", "/workspace", ""); got.Code != 401 {
		t.Fatalf("unauthenticated snapshot: %d", got.Code)
	}
	batch := `{"agents":[{"id":"profile","name":"Codex","agentType":"codex"}],"projects":[{"id":"project","name":"App","path":"/work/app"}],"chats":[{"id":"chat","agentId":"profile","projectId":"project","title":"Hello","sessionId":"native-id"}]}`
	first := serve(t, authenticated, "POST", "/workspace/import", batch)
	if first.Code != 200 {
		t.Fatalf("import: %d %s", first.Code, first.Body.String())
	}
	var snapshot registry.Snapshot
	if err := json.Unmarshal(first.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agents) != 1 || len(snapshot.Projects) != 1 || len(snapshot.Chats) != 1 {
		t.Fatalf("snapshot: %+v", snapshot)
	}
	if st.Session("codex", "chat") != "native-id" || st.ChatCWD("chat") != "/work/app" {
		t.Fatal("native session mapping lost during migration")
	}
	second := serve(t, authenticated, "GET", "/workspace", "")
	if second.Body.String() != first.Body.String() {
		t.Fatal("a second client cannot restore the first client's registry")
	}
	if got := serve(t, authenticated, "PUT", "/workspace/agents/profile", `{"record":{"name":"Rename","agentType":"codex"}}`); got.Code != 409 {
		t.Fatalf("unversioned overwrite: %d %s", got.Code, got.Body.String())
	}
	if got := serve(t, authenticated, "PUT", "/chats/chat", `{"title":"Pinned"}`); got.Code != 200 {
		t.Fatalf("legacy rename: %d", got.Code)
	}
	changed := serve(t, authenticated, "GET", "/workspace/changes?since=1&workspaceId="+snapshot.WorkspaceID, "")
	var delta registry.Snapshot
	if err := json.Unmarshal(changed.Body.Bytes(), &delta); err != nil {
		t.Fatal(err)
	}
	if delta.Full || len(delta.Chats) != 1 || delta.Chats[0].Title != "Pinned" {
		t.Fatalf("rename not synchronized: %+v", delta)
	}
	if got := serve(t, authenticated, "GET", "/workspace/changes?since=1&workspaceId=wrong", ""); got.Code != 409 {
		t.Fatalf("wrong registry cursor: %d", got.Code)
	}
	if got := serve(t, authenticated, "DELETE", "/chats/chat", ""); got.Code != 200 {
		t.Fatalf("delete: %d %s", got.Code, got.Body.String())
	}
	if got := serve(t, authenticated, "POST", "/workspace/import", batch); got.Code != 200 {
		t.Fatalf("stale import: %d", got.Code)
	}
	after := serve(t, authenticated, "GET", "/workspace", "")
	if err := json.Unmarshal(after.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Chats) != 0 || len(snapshot.Deleted) != 1 {
		t.Fatalf("deleted chat resurrected: %+v", snapshot)
	}
	if got := serve(t, authenticated, "POST", "/turns?agent=codex", `{"chatId":"chat","message":"should never run"}`); got.Code != 410 {
		t.Fatalf("deleted chat started: %d %s", got.Code, got.Body.String())
	}
}

func TestWorkspaceHTTPParentDeleteAndUnknownHarness(t *testing.T) {
	h, _ := newRegistryEnv(t)
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	if got := serve(t, authenticated, "PUT", "/workspace/agents/a", `{"record":{"name":"A","agentType":"not-installed-in-daemon"}}`); got.Code != 400 {
		t.Fatalf("unknown harness: %d", got.Code)
	}
	batch := `{"agents":[{"id":"a","name":"A","agentType":"codex"}],"projects":[{"id":"p","name":"P","path":"/work"}],"chats":[{"id":"c","agentId":"a","projectId":"p","title":"C"}]}`
	if got := serve(t, authenticated, "POST", "/workspace/import", batch); got.Code != 200 {
		t.Fatalf("import: %s", got.Body.String())
	}
	if got := serve(t, authenticated, "DELETE", "/workspace/agents/a?revision=99", ""); got.Code != 409 {
		t.Fatalf("stale delete: %d", got.Code)
	}
	got := serve(t, authenticated, "DELETE", "/workspace/agents/a?revision=1", "")
	var s registry.Snapshot
	if err := json.Unmarshal(got.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if got.Code != 200 || len(s.Agents) != 0 || len(s.Chats) != 0 || len(s.Projects) != 1 || len(s.Deleted) != 2 {
		t.Fatalf("cascade: %d %+v", got.Code, s)
	}
}

func TestWorkspaceForkIncludesEmptyChatsAndDeletedChatsCannotCompact(t *testing.T) {
	h, st := newRegistryEnv(t)
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	parents := `{"agents":[{"id":"a","name":"Codex","agentType":"codex"}],"projects":[{"id":"p","name":"App","path":"/work"}]}`
	if got := serve(t, authenticated, "POST", "/workspace/import", parents); got.Code != 200 {
		t.Fatalf("parents: %s", got.Body.String())
	}
	if got := serve(t, authenticated, "PUT", "/workspace/chats/empty", `{"record":{"agentId":"a","projectId":"p","title":"Empty draft"}}`); got.Code != 200 {
		t.Fatalf("create: %s", got.Body.String())
	}
	if st.ChatExists("empty") {
		t.Fatal("fixture must not have native session state yet")
	}
	fork := serve(t, authenticated, "POST", "/chats/empty/fork", `{"newChatId":"fork"}`)
	var summary session.ChatSummary
	if err := json.Unmarshal(fork.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if fork.Code != 200 || summary.ChatID != "fork" || summary.Agent != "codex" || summary.Title != "Empty draft" || summary.UpdatedAt == "" {
		t.Fatalf("empty fork: %d %+v", fork.Code, summary)
	}
	for _, target := range []string{"fork", "empty"} {
		body, _ := json.Marshal(map[string]string{"newChatId": target})
		if got := serve(t, authenticated, "POST", "/chats/empty/fork", string(body)); got.Code != 400 {
			t.Fatalf("existing target %s: %d", target, got.Code)
		}
	}
	if err := st.SetSession("codex", "empty", "native-source"); err != nil {
		t.Fatal(err)
	}
	if got := serve(t, authenticated, "POST", "/chats/empty/fork", `{"newChatId":"native-fork"}`); got.Code != 200 {
		t.Fatalf("native fork: %d %s", got.Code, got.Body.String())
	}
	if st.Session("codex", "native-fork") != "native-source" || !st.TakeForkPending("codex", "native-fork") {
		t.Fatal("registered fork lost native branch semantics")
	}
	if got := serve(t, authenticated, "DELETE", "/workspace/chats/empty?revision=2", ""); got.Code != 200 {
		t.Fatalf("membership delete: %d %s", got.Code, got.Body.String())
	}
	if st.Session("codex", "empty") != "native-source" {
		t.Fatal("membership removal deleted native transcript mapping")
	}
	if got := serve(t, authenticated, "POST", "/chats/empty/compact?agent=codex", ""); got.Code != 410 {
		t.Fatalf("deleted chat compacted: %d %s", got.Code, got.Body.String())
	}
}
