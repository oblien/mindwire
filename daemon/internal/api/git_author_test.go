package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthorSettingsDoNotSubmitACommit(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	h, a, root := gitHTTPFixture(t)
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("staged"), 0600); err != nil {
		t.Fatal(err)
	}
	apiGit(t, "-C", root, "add", "file")
	before := apiGit(t, "-C", root, "write-tree")
	url := "/workspace/git/identity?projectId=project"
	response := serve(t, h, "PUT", url, `{"scope":"repository","identity":{"name":"App User","email":"app@example.invalid"}}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"App User"`) {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	read := serve(t, h, "GET", url, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"email":"app@example.invalid"`) {
		t.Fatal(read.Body.String())
	}
	if apiGit(t, "-C", root, "write-tree") != before {
		t.Fatal("author settings changed the index")
	}
	if exec.Command("git", "-C", root, "rev-parse", "--verify", "HEAD").Run() == nil {
		t.Fatal("saving author created a commit")
	}
	operations, err := a.gitJobs.List("project", false)
	if err != nil || len(operations) != 0 {
		t.Fatalf("settings submitted a Git operation: %+v %v", operations, err)
	}
	response = serve(t, h, "PUT", "/workspace/git/identity", `{"scope":"all_workspaces","identity":{"name":"Default","email":"default@example.invalid"},"requestId":"default-1","expectedRevision":""}`)
	if response.Code != http.StatusOK {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	if apiGit(t, "-C", root, "config", "user.name") != "App User" {
		t.Fatal("shared default overwrote explicit repository author")
	}
}
