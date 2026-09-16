package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func TestProjectHTTPCompletionIsDurableAndReconnectStartsWithCurrentSnapshot(t *testing.T) {
	h, _ := newRegistryEnv(t)
	if got := serve(t, h, "GET", "/workspace/operations", ""); got.Code != 401 {
		t.Fatalf("unauthenticated operations=%d", got.Code)
	}
	auth := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Mindwire-Token", "workspace-secret")
		h.ServeHTTP(w, r)
	})
	path := filepath.Join(t.TempDir(), "project")
	request, _ := json.Marshal(map[string]string{"id": "new-project", "source": "create", "name": "App", "path": path})
	response := serve(t, auth, "POST", "/workspace/projects?agent=codex", string(request))
	if response.Code != 202 {
		t.Fatalf("start %d %s", response.Code, response.Body.String())
	}
	var o registry.ProjectOperation
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response = serve(t, auth, "GET", "/workspace/operations/new-project?agent=claude-code", "")
		if err := json.Unmarshal(response.Body.Bytes(), &o); err != nil {
			t.Fatal(err)
		}
		if !o.Active() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if o.Status != "succeeded" {
		t.Fatalf("%+v", o)
	}
	var snapshot registry.Snapshot
	response = serve(t, auth, "GET", "/workspace", "")
	_ = json.Unmarshal(response.Body.Bytes(), &snapshot)
	if len(snapshot.Projects) != 1 || snapshot.Projects[0].ID != o.ProjectID {
		t.Fatal("terminal response has no project")
	}
	response = serve(t, auth, "GET", "/workspace/operations/new-project/stream", "")
	if response.Code != 200 || strings.Count(response.Body.String(), "data:") != 1 || !strings.Contains(response.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("replayed old progress: %s", response.Body.String())
	}
	response = serve(t, auth, "POST", "/workspace/projects", string(request))
	if response.Code != 202 {
		t.Fatal(response.Body.String())
	}
	var same registry.ProjectOperation
	_ = json.Unmarshal(response.Body.Bytes(), &same)
	if same.Sequence != o.Sequence {
		t.Fatal("lost acknowledgement retry re-ran completed operation")
	}
	invalid, _ := json.Marshal(map[string]any{"record": map[string]string{"name": "Duplicate", "path": path}})
	if duplicate := serve(t, auth, "PUT", "/workspace/projects/duplicate", string(invalid)); duplicate.Code != 409 {
		t.Fatalf("directory duplicate: %d %s", duplicate.Code, duplicate.Body.String())
	}
	if missing := serve(t, auth, "GET", "/workspace/operations/missing", ""); missing.Code != 404 {
		t.Fatalf("missing=%d", missing.Code)
	}
}

func TestProjectRemovalKeepsNativeSessionAndBlocksNewTurnsWhileReserved(t *testing.T) {
	h, native, api := newRegistryAPIEnv(t)
	auth := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "file.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	batch := registry.Import{
		Agents:   []registry.Agent{{Record: registry.Record{ID: "profile"}, Name: "Codex", AgentType: "codex"}},
		Projects: []registry.Project{{Record: registry.Record{ID: "project"}, Name: "App", Path: path}},
		Chats:    []registry.Chat{{Record: registry.Record{ID: "chat"}, AgentID: "profile", ProjectID: "project", Title: "Build", SessionID: "native-session"}},
	}
	body, _ := json.Marshal(batch)
	if got := serve(t, auth, "POST", "/workspace/import", string(body)); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	p, err := api.registry.Project("project")
	if err != nil {
		t.Fatal(err)
	}
	o, _, err := api.registry.ReserveRemoval("remove", p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, chatID := range []string{"chat", "legacy-chat-without-membership"} {
		turn, _ := json.Marshal(map[string]string{"chatId": chatID, "cwd": path, "message": "must not execute"})
		if got := serve(t, auth, "POST", "/turns?agent=codex", string(turn)); got.Code != 409 {
			t.Fatalf("turn started during removal: %d %s", got.Code, got.Body.String())
		}
	}
	if got := serve(t, auth, "PUT", "/workspace/chats/new-chat", `{"record":{"agentId":"profile","projectId":"project","title":"New"}}`); got.Code != 409 {
		t.Fatalf("new chat during removal: %d %s", got.Code, got.Body.String())
	}
	if got := serve(t, auth, "DELETE", "/workspace/projects/project?revision=1", ""); got.Code != 409 {
		t.Fatalf("metadata removed during operation: %d", got.Code)
	}
	// Release the deliberately paused fixture reservation, then exercise the actual endpoint.
	if _, err := api.registry.ChangeProjectOperation(o.ID, func(v *registry.ProjectOperation) error { v.Status = "cancelled"; return nil }); err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(map[string]any{"operationId": "http-removal", "expectedRevision": p.Revision})
	if got := serve(t, h, "POST", "/workspace/projects/project/remove", string(request)); got.Code != 401 {
		t.Fatalf("unauthenticated deletion=%d", got.Code)
	}
	if got := serve(t, auth, "POST", "/workspace/projects/project/remove?agent=claude-code", string(request)); got.Code != 202 {
		t.Fatalf("removal: %d %s", got.Code, got.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := serve(t, auth, "GET", "/workspace/operations/http-removal", "")
		if err := json.Unmarshal(got.Body.Bytes(), &o); err != nil {
			t.Fatal(err)
		}
		if !o.Active() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if o.Status != "succeeded" {
		t.Fatalf("%+v", o)
	}
	if native.Session("codex", "chat") != "native-session" || native.ChatCWD("chat") != path {
		t.Fatal("project removal purged native transcript references")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("files remain: %v", err)
	}
}
