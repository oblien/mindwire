package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--git-credential" {
		if err := gitaccess.Helper(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestManagedCloneForwardsCredentialAndKeepsBindingWithoutChangingOrigin(t *testing.T) {
	old, st, dir := fixture(t)
	old.Close()
	access, err := gitaccess.New(st.Directory())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	s, err := New(st, nil, access)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	source := repository(t, dir, false)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// Replace only the network clone with a local fixture. The production project
	// workflow, credential helper subprocess, and registration all run unchanged.
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = clone ]; then\n" +
		"  credential=$(printf 'protocol=https\\nhost=github.com\\npath=octocat/app.git\\n\\n' | " + shellQuote(realGit) + " credential fill) || exit 1\n" +
		"  case \"$credential\" in *password=managed-clone-fixture*) ;; *) exit 2;; esac\n" +
		"  for destination do :; done\n" +
		"  " + shellQuote(realGit) + " clone -- " + shellQuote(source) + " \"$destination\" || exit 3\n" +
		"  exec " + shellQuote(realGit) + " -C \"$destination\" remote set-url origin https://github.com/octocat/app.git\n" +
		"fi\nexec " + shellQuote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := &gitaccess.Connection{ID: "personal", Login: "octocat", Mode: "token", Lifetime: "workspace"}
	request := Request{ProjectSpec: registry.ProjectSpec{ID: "managed", Source: "clone", Name: "App", Path: filepath.Join(dir, "app"), RepoURL: "https://github.com/octocat/app.git", GitConnection: c},
		Auth: Auth{ConnectionID: c.ID, Kind: "token", Token: "managed-clone-fixture"}}
	operation, err := s.Start(request)
	if err != nil {
		t.Fatal(err)
	}
	operation = terminal(t, s, operation.ID)
	if operation.Status != "succeeded" {
		t.Fatalf("clone: %s %s", operation.Status, operation.Error)
	}
	p, err := st.Project(operation.ProjectID)
	if err != nil || p.GitConnection == nil || *p.GitConnection != *c {
		t.Fatalf("project binding: %+v %v", p, err)
	}
	data, _ := json.Marshal(operation)
	if strings.Contains(string(data), request.Auth.Token) || git(t, "-C", request.Path, "remote", "get-url", "origin") != request.RepoURL {
		t.Fatal("clone persisted a credential or changed its origin")
	}
	s.Close()
	access.Close()
	// Terminal Git uses the saved include even after the phone and daemon stop.
	cmd := exec.Command(realGit, "-C", p.Path, "credential", "fill")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\npath=octocat/app.git\n\n")
	output, err := cmd.Output()
	if err != nil || !strings.Contains(string(output), "password="+request.Auth.Token) {
		t.Fatalf("saved clone credential helper failed: %v", err)
	}
}

func TestRecoveryInstallsSavedGitAccessBeforeCompletingRegistration(t *testing.T) {
	s, st, dir := fixture(t)
	s.Close()
	access, err := gitaccess.New(st.Directory())
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	c := &gitaccess.Connection{ID: "personal", Login: "octocat", Mode: "token", Lifetime: "workspace"}
	if err := access.Save(*c, &gitaccess.Auth{Kind: "token", Token: "recovery-fixture"}); err != nil {
		t.Fatal(err)
	}
	spec := registry.ProjectSpec{ID: "recover-git", Source: "clone", Name: "App", Path: filepath.Join(dir, "app"), RepoURL: "https://github.com/octocat/app.git", AuthKind: "token", GitConnection: c}
	o, _, err := st.ReserveProject(spec)
	if err != nil {
		t.Fatal(err)
	}
	o, err = st.ChangeProjectOperation(o.ID, func(o *registry.ProjectOperation) error { o.Status, o.Phase = "running", "registering"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	git(t, "init", o.Path)
	git(t, "-C", o.Path, "remote", "add", "origin", o.RepoURL)
	if err := writeMarker(o.Path, o); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(st, nil, access)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	o, _ = st.ProjectOperation(o.ID)
	if o.Status != "succeeded" || git(t, "-C", o.Path, "config", "--local", "--get", "include.path") == "" {
		t.Fatal("recovery completed without installing saved Git access")
	}
	lease, err := access.Prepare(context.Background(), o.RepoURL, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
}
