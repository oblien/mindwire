package projects

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

// Exercise Git's real HTTP authentication path, including .git/config persistence.
func TestHTTPCloneUsesCredentialWithoutPersistingIt(t *testing.T) {
	s, st, dir := fixture(t)
	source := repository(t, dir, false)
	git(t, "clone", "--bare", source, filepath.Join(dir, "repo.git"))
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{Path: gitPath, Args: []string{"http-backend"}, Dir: dir, Env: []string{"GIT_PROJECT_ROOT=" + dir, "GIT_HTTP_EXPORT_ALL=1"}}
	secret := "fixture-http-secret-not-in-config"
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+secret))
	var authenticated atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Authorization"), header) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authenticated.Add(1)
		backend.ServeHTTP(w, r)
	}))
	defer server.Close()
	url := server.URL + "/repo.git"
	o, err := s.Start(Request{ProjectSpec: registry.ProjectSpec{ID: "http-clone", Source: "clone", Name: "App", Path: filepath.Join(dir, "app"), RepoURL: url}, Auth: Auth{Kind: "token", Token: secret}})
	if err != nil {
		t.Fatal(err)
	}
	o = terminal(t, s, o.ID)
	if o.Status != "succeeded" || authenticated.Load() < 1 {
		t.Fatalf("authenticated clone: %+v", o)
	}
	config, err := os.ReadFile(filepath.Join(o.Path, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), secret) || strings.Contains(string(config), "AUTHORIZATION") {
		t.Fatal("credential persisted in cloned repository")
	}
	if git(t, "-C", o.Path, "remote", "get-url", "origin") != url {
		t.Fatal("origin includes credential")
	}
	all, err := st.ProjectOperations(false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(all)
	if strings.Contains(string(data), secret) || strings.Contains(string(data), base64.StdEncoding.EncodeToString([]byte("x-access-token:"+secret))) {
		t.Fatal("credential persisted in operations")
	}
}
