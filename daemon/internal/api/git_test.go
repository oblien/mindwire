package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

func gitHTTPFixture(t *testing.T) (http.Handler, *API, string) {
	t.Helper()
	h, _, a := newRegistryAPIEnv(t)
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	for _, path := range []string{"/workspace/git", "/workspace/projects/project/git"} {
		if got := serve(t, h, "GET", path, ""); got.Code != 401 {
			t.Fatalf("unauthenticated Git state: %d", got.Code)
		}
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	apiGit(t, "init", dir)
	apiGit(t, "-C", dir, "remote", "add", "origin", "https://github.com/octocat/app.git")
	apiGit(t, "-C", dir, "config", "user.name", "Existing User")
	data, _ := json.Marshal(map[string]any{
		"projects": []map[string]string{{"id": "project", "name": "App", "path": dir}},
		"agents":   []map[string]string{{"id": "profile", "name": "Codex", "agentType": "codex"}},
		"chats":    []map[string]string{{"id": "chat", "agentId": "profile", "projectId": "project", "title": "Work"}},
	})
	if got := serve(t, authed, "POST", "/workspace/import", string(data)); got.Code != 200 {
		t.Fatalf("import: %d %s", got.Code, got.Body.String())
	}
	return authed, a, dir
}

func apiGit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git fixture: %v %s", err, data)
	}
	return strings.TrimSpace(string(data))
}

func TestGitHTTPAccountBindingPersistenceInheritanceAndRevocation(t *testing.T) {
	h, a, dir := gitHTTPFixture(t)
	personal := &gitaccess.Connection{ID: "personal", Login: "octocat", Mode: "token", Lifetime: "run"}
	work := &gitaccess.Connection{ID: "work", Login: "work-user", Mode: "token", Lifetime: "workspace"}
	request := func(method, path string, body any) string {
		t.Helper()
		data, _ := json.Marshal(body)
		got := serve(t, h, method, path, string(data))
		if got.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, got.Code, got.Body.String())
		}
		if strings.Contains(got.Body.String(), "private-work-token") {
			t.Fatal("credential returned in HTTP metadata")
		}
		return got.Body.String()
	}
	request("PUT", "/workspace/git", map[string]any{"connection": personal})
	project, _ := a.registry.Project("project")
	request("PUT", "/workspace/projects/project/git", map[string]any{"connection": work, "expectedRevision": project.Revision,
		"auth": gitaccess.Auth{ConnectionID: work.ID, Kind: "token", Token: "private-work-token"}})
	project, _ = a.registry.Project("project")
	if project.GitConnection == nil || project.GitConnection.ID != work.ID {
		t.Fatal("project override was not persisted")
	}
	state := request("GET", "/workspace/projects/project/git", nil)
	if !strings.Contains(state, `"stored":true`) || !strings.Contains(state, `"inherited":false`) {
		t.Fatal("another device cannot use the saved binding")
	}
	lease, err := a.prepareGitRun(context.Background(), "chat", "/untrusted-client-path", nil)
	if err != nil || lease.Auth.Token != "private-work-token" {
		t.Fatalf("saved run authorization: %v", err)
	}
	lease.Close()
	// An older client can rename without knowing the new field.
	request("PUT", "/workspace/projects/project", map[string]any{"expectedRevision": project.Revision,
		"record": map[string]string{"name": "Renamed", "path": dir}})
	project, _ = a.registry.Project("project")
	if project.GitConnection == nil || project.GitConnection.ID != work.ID {
		t.Fatal("old client erased the project account")
	}
	// Explicit null must survive project canonicalization and restore inheritance.
	request("PUT", "/workspace/projects/project/git", map[string]any{"connection": nil, "expectedRevision": project.Revision})
	project, _ = a.registry.Project("project")
	if project.GitConnection != nil {
		t.Fatal("explicit reset retained the old connection")
	}
	state = request("GET", "/workspace/projects/project/git", nil)
	if !strings.Contains(state, `"id":"personal"`) || !strings.Contains(state, `"inherited":true`) {
		t.Fatal("workspace default was not inherited")
	}
	if _, err := a.prepareGitRun(context.Background(), "chat", "", &gitaccess.Auth{ConnectionID: work.ID, Kind: "token", Token: "old-selection"}); !errors.Is(err, gitaccess.ErrCredential) {
		t.Fatalf("an account change accepted the previous account's token: %v", err)
	}
	lease, err = a.prepareGitRun(context.Background(), "chat", "", &gitaccess.Auth{ConnectionID: personal.ID, Kind: "token", Token: "personal-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
	request("DELETE", "/workspace/git/connections/work", nil)
	if _, err := a.gitAccess.Resolve(work, nil); !errors.Is(err, gitaccess.ErrCredential) {
		t.Fatal("removed workspace access still resolves")
	}
	if apiGit(t, "-C", dir, "remote", "get-url", "origin") != "https://github.com/octocat/app.git" || apiGit(t, "-C", dir, "config", "user.name") != "Existing User" {
		t.Fatal("Git connection settings changed origin or commit identity")
	}
}

func TestGitDefaultLeavesOtherHostsNativeAndBlocksConcurrentWork(t *testing.T) {
	h, a, dir := gitHTTPFixture(t)
	apiGit(t, "-C", dir, "remote", "set-url", "origin", "https://gitlab.example.invalid/team/app.git")
	body := `{"connection":{"id":"personal","login":"octocat","mode":"token","lifetime":"run"}}`
	if got := serve(t, h, "PUT", "/workspace/git", body); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	got := serve(t, h, "GET", "/workspace/projects/project/git", "")
	var state projectGitState
	if json.Unmarshal(got.Body.Bytes(), &state) != nil || state.Connection != nil {
		t.Fatal("GitHub default applied to another host")
	}
	lease, err := a.prepareGitRun(context.Background(), "chat", "", nil)
	if err != nil || len(lease.Environment) != 0 {
		t.Fatalf("native server Git was affected: %v", err)
	}
	lease.Close()
	startBlockingGitCommit(t, a, dir)
	if got := serve(t, h, "GET", "/workspace/git", ""); !strings.Contains(got.Body.String(), `"activeOperations":1`) {
		t.Fatal("active Git work is invisible to daemon update checks")
	}
	if got := serve(t, h, "POST", "/workspace/projects/project/git/pull", "{}"); got.Code != 409 {
		t.Fatalf("competing Git operation: %d", got.Code)
	}
	if _, err := a.prepareGitRun(context.Background(), "chat", "", nil); !errors.Is(err, registry.ErrConflict) {
		t.Fatal("agent run admitted during a Git operation")
	}
	if got := serve(t, h, "PUT", "/workspace/git", body); got.Code != 409 {
		t.Fatalf("default changed during a Git operation: %d", got.Code)
	}
}

func startBlockingGitCommit(t *testing.T, a *API, dir string) {
	t.Helper()
	apiGit(t, "-C", dir, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	apiGit(t, "-C", dir, "add", "tracked")
	hook := "#!/bin/sh\n: > .git/test-started\nwhile [ ! -f .git/test-release ]; do sleep 0.02; done\n"
	if err := os.WriteFile(filepath.Join(dir, ".git/hooks/pre-commit"), []byte(hook), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.startGitOperation(context.Background(), "project", gitOperationRequest{ID: "blocked-commit", Action: "commit", Message: "Fixture commit"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(dir, ".git/test-release"), nil, 0600) })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git/test-started")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("commit hook did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestGitHTTPDurableMutationsAndRecovery(t *testing.T) {
	h, a, dir := gitHTTPFixture(t)
	startBlockingGitCommit(t, a, dir)
	request := `{"id":"blocked-commit","action":"commit","message":"Fixture commit"}`
	got := serve(t, h, "POST", "/workspace/projects/project/git/operations", request)
	if got.Code != 202 || !strings.Contains(got.Body.String(), `"id":"blocked-commit"`) {
		t.Fatalf("duplicate start: %d %s", got.Code, got.Body.String())
	}
	for _, action := range []string{"stage", "unstage", "discard"} {
		body := `{"id":"competing","action":"` + action + `","paths":["tracked"]}`
		if got := serve(t, h, "POST", "/workspace/projects/project/git/operations", body); got.Code != 409 {
			t.Fatalf("%s bypassed coordination: %d %s", action, got.Code, got.Body.String())
		}
	}
	if got := serve(t, h, "GET", "/workspace/projects/project/git/operations?active=true", ""); got.Code != 200 || !strings.Contains(got.Body.String(), "blocked-commit") {
		t.Fatalf("reconnected client cannot find active operation: %d %s", got.Code, got.Body.String())
	}
	if err := os.WriteFile(filepath.Join(dir, ".git/test-release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.gitJobs.Wait(ctx, "blocked-commit"); err != nil {
		t.Fatal(err)
	}
	got = serve(t, h, "POST", "/workspace/projects/project/git/operations", request)
	if got.Code != 202 || !strings.Contains(got.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("completed retry: %d %s", got.Code, got.Body.String())
	}
	if apiGit(t, "-C", dir, "rev-list", "--count", "HEAD") != "1" {
		t.Fatal("retry repeated the commit")
	}
	got = serve(t, h, "GET", "/workspace/git/operations/blocked-commit", "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"status":"succeeded"`) {
		t.Fatal("operation result is not recoverable")
	}
	got = serve(t, h, "GET", "/workspace/git/operations/blocked-commit/stream", "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), "event: git_operation") {
		t.Fatal("operation stream lacks its current snapshot")
	}
}

func TestGitHTTPRejectsReadOnlyPushBeforeContactingGitHub(t *testing.T) {
	h, a, _ := gitHTTPFixture(t)
	project, _ := a.registry.Project("project")
	data, _ := json.Marshal(map[string]any{"connection": gitaccess.Connection{ID: "installation", Login: "octocat", Mode: "oblienApp", Lifetime: "run"}, "expectedRevision": project.Revision})
	if got := serve(t, h, "PUT", "/workspace/projects/project/git", string(data)); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	got := serve(t, h, "POST", "/workspace/projects/project/git/push", `{"auth":{"connectionId":"installation","kind":"token","token":"read-only-fixture"}}`)
	if got.Code != 400 || !strings.Contains(got.Body.String(), "read-only") || strings.Contains(got.Body.String(), "read-only-fixture") {
		t.Fatalf("read-only push: %d %s", got.Code, got.Body.String())
	}
}
