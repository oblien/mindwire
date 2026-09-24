package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommitAuthorSetupHTTPReportsRemediationAndPersistsIdentity(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	h, a, dir := gitHTTPFixture(t)
	apiGit(t, "-C", dir, "config", "user.useConfigOnly", "true")
	apiGit(t, "-C", dir, "config", "--unset", "user.name")
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("staged content"), 0600); err != nil {
		t.Fatal(err)
	}
	apiGit(t, "-C", dir, "add", "file")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response := serve(t, h, "POST", "/workspace/projects/project/git/operations", `{"id":"needs-author","action":"commit","message":"Keep this draft"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	failed, err := a.gitJobs.Wait(ctx, "needs-author")
	if err != nil || failed.ErrorCode != "git_identity_required" {
		t.Fatalf("%+v %v", failed, err)
	}
	state := serve(t, h, "GET", "/workspace/git/operations/needs-author", "")
	if !strings.Contains(state.Body.String(), `"errorCode":"git_identity_required"`) {
		t.Fatal(state.Body.String())
	}
	response = serve(t, h, "POST", "/workspace/projects/project/git/operations",
		`{"id":"save-author","action":"commit","message":"Keep this draft","identity":{"name":"App User","email":"app@example.invalid"}}`)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"identity":{"name":"App User"`) {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	result, err := a.gitJobs.Wait(ctx, "save-author")
	if err != nil || result.Status != "succeeded" || apiGit(t, "-C", dir, "config", "--local", "user.name") != "App User" {
		t.Fatalf("author was not saved: %+v %v", result, err)
	}
}
