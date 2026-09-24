package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBranchHTTPPersistsIntentAndKeepsOldClientHistoryCompatible(t *testing.T) {
	h, a, dir := gitHTTPFixture(t)
	apiGit(t, "-C", dir, "config", "user.email", "fixture@example.invalid")
	apiGit(t, "-C", dir, "commit", "--allow-empty", "-qm", "initial")
	created := serve(t, h, "POST", "/workspace/projects/project/git/operations",
		`{"id":"branch-http","action":"create_branch","branch":"feature/http"}`)
	if created.Code != http.StatusAccepted || !strings.Contains(created.Body.String(), `"branch":"feature/http"`) {
		t.Fatalf("branch was not accepted: %d %s", created.Code, created.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	operation, err := a.gitJobs.Wait(ctx, "branch-http")
	if err != nil || operation.Status != "succeeded" || apiGit(t, "-C", dir, "branch", "--show-current") != "feature/http" {
		t.Fatalf("branch was not applied: %+v %v", operation, err)
	}
	for _, version := range []string{"", "?actionsVersion=2"} {
		response := serve(t, h, "GET", "/workspace/projects/project/git/operations"+version, "")
		var body struct {
			Operations []json.RawMessage `json:"operations"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK {
			t.Fatalf("history: %d %s", response.Code, response.Body.String())
		}
		if version == "" && len(body.Operations) != 0 || version != "" && len(body.Operations) != 1 {
			t.Fatalf("wrong history for client %q: %s", version, response.Body.String())
		}
	}
}
