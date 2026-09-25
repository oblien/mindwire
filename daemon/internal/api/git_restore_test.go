package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreHTTPPersistsGuardsAndKeepsOlderActionEnumsCompatible(t *testing.T) {
	h, a, dir := gitHTTPFixture(t)
	apiGit(t, "-C", dir, "config", "user.email", "fixture@example.invalid")
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("earlier"), 0600); err != nil {
		t.Fatal(err)
	}
	apiGit(t, "-C", dir, "add", ".")
	apiGit(t, "-C", dir, "commit", "-qm", "earlier")
	target := apiGit(t, "-C", dir, "rev-parse", "HEAD")
	if err := os.WriteFile(path, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	apiGit(t, "-C", dir, "commit", "-qam", "current")
	head := apiGit(t, "-C", dir, "rev-parse", "HEAD")
	branch := apiGit(t, "-C", dir, "branch", "--show-current")
	request, _ := json.Marshal(map[string]string{"id": "restore-http", "action": "restore_commit", "commitId": target,
		"expectedHead": head, "expectedBranch": branch})
	created := serve(t, h, "POST", "/workspace/projects/project/git/operations", string(request))
	if created.Code != http.StatusAccepted {
		t.Fatalf("restore not accepted: %d %s", created.Code, created.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	operation, err := a.gitJobs.Wait(ctx, "restore-http")
	if err != nil || operation.Status != "succeeded" || operation.CommitID != target || operation.ExpectedHead != head || operation.ExpectedBranch != branch {
		t.Fatalf("restore lost intent or failed: %+v %v", operation, err)
	}
	for _, query := range []string{"", "?actionsVersion=2", "?actionsVersion=3", "?actionsVersion=4"} {
		response := serve(t, h, "GET", "/workspace/projects/project/git/operations"+query, "")
		var body struct {
			Operations []json.RawMessage `json:"operations"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK {
			t.Fatalf("history: %s", response.Body.String())
		}
		want := 0
		if query == "?actionsVersion=4" {
			want = 1
		}
		if len(body.Operations) != want {
			t.Fatalf("wrong history for %q: %s", query, response.Body.String())
		}
	}
	if apiGit(t, "-C", dir, "rev-parse", "HEAD") != head {
		t.Fatal("HTTP restore moved HEAD")
	}
}
