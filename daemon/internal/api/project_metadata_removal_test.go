package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func TestRemoveExistingProjectMetadataKeepsFilesAndNativeHistory(t *testing.T) {
	h, native := newRegistryEnv(t)
	client := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	folder := filepath.Join(t.TempDir(), "existing folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "keep.txt")
	if err := os.WriteFile(file, []byte("existing user files"), 0600); err != nil {
		t.Fatal(err)
	}
	addProject := func(id string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"id": id, "source": "folder", "name": "Existing project", "path": folder})
		response := serve(t, client, "POST", "/workspace/projects", string(body))
		if response.Code != http.StatusAccepted {
			t.Fatalf("register folder: %d %s", response.Code, response.Body)
		}
		var operation registry.ProjectOperation
		for i := 0; i < 200; i++ {
			if err := json.Unmarshal(response.Body.Bytes(), &operation); err != nil {
				t.Fatal(err)
			}
			if !operation.Active() {
				break
			}
			time.Sleep(10 * time.Millisecond)
			response = serve(t, client, "GET", "/workspace/operations/"+operation.ID, "")
		}
		if operation.Status != "succeeded" {
			t.Fatalf("register folder: %+v", operation)
		}
	}
	addProject("project")
	links := `{"agents":[{"id":"agent","name":"Codex","agentType":"codex"}],"chats":[{"id":"chat","agentId":"agent","projectId":"project","title":"Keep native history","sessionId":"native-session"}]}`
	response := serve(t, client, "POST", "/workspace/import", links)
	var before registry.Snapshot
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &before) != nil || len(before.Projects) != 1 {
		t.Fatalf("register chat links: %d %s", response.Code, response.Body)
	}
	path := fmt.Sprintf("/workspace/projects/project?revision=%d", before.Projects[0].Revision)
	response = serve(t, client, "DELETE", path, "")
	var after registry.Snapshot
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &after) != nil {
		t.Fatalf("metadata removal: %d %s", response.Code, response.Body)
	}
	if len(after.Projects) != 0 || len(after.Chats) != 0 || len(after.Agents) != 1 {
		t.Fatalf("project membership was not removed atomically: %+v", after)
	}
	if native.Session("codex", "chat") != "native-session" {
		t.Fatal("metadata removal purged native history")
	}
	if content, err := os.ReadFile(file); err != nil || string(content) != "existing user files" {
		t.Fatalf("metadata removal touched user files: %q %v", content, err)
	}
	response = serve(t, client, "GET", fmt.Sprintf("/workspace/changes?since=%d&workspaceId=%s", before.Revision, before.WorkspaceID), "")
	var delta registry.Snapshot
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &delta) != nil || len(delta.Deleted) != 2 {
		t.Fatalf("other clients cannot reconcile removal: %d %s", response.Code, response.Body)
	}
	stale, _ := json.Marshal(before.Import)
	if response := serve(t, client, "POST", "/workspace/import", string(stale)); response.Code != 200 {
		t.Fatalf("stale cache import: %d %s", response.Code, response.Body)
	}
	response = serve(t, client, "GET", "/workspace", "")
	if json.Unmarshal(response.Body.Bytes(), &after) != nil || len(after.Projects) != 0 || len(after.Chats) != 0 {
		t.Fatal("an old mobile cache resurrected the removed project")
	}
	// Keeping files also permits explicitly adding the same folder again as a new project.
	addProject("new-project")
	if content, err := os.ReadFile(file); err != nil || string(content) != "existing user files" {
		t.Fatalf("re-registering the folder touched user files: %q %v", content, err)
	}
}
