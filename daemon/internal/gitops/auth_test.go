package gitops

import (
	"encoding/json"
	"fmt"
	"os"
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

func TestCommitHooksUseSelectedCredentialsAndPreserveLocalCommits(t *testing.T) {
	for _, scenario := range []string{"run", "workspace", "missing", "other-repository", "local-without-access"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, root := fixture(t)
			git(t, root, "remote", "add", "origin", "https://github.com/octocat/app.git")
			// A selected managed account must not silently use a different native login.
			git(t, root, "config", "credential.helper", "!f() { echo username=native; echo password=wrong-account; }; f")
			c := gitaccess.Connection{ID: "chosen", Login: "octocat", Mode: "token", Lifetime: "run"}
			auth := &gitaccess.Auth{ConnectionID: c.ID, Kind: "token", Token: "commit-fixture-secret"}
			if scenario == "workspace" {
				c.Lifetime = "workspace"
				if err := s.access.Save(c, auth); err != nil {
					t.Fatal(err)
				}
				auth = nil
			} else if scenario == "missing" || scenario == "local-without-access" {
				auth = nil
			}
			if scenario != "local-without-access" {
				repository := "octocat/app.git"
				if scenario == "other-repository" {
					repository = "octocat/private-other.git"
				}
				hook := "#!/bin/sh\nset -e\ncredential=$(git credential fill <<'INPUT'\nprotocol=https\nhost=github.com\npath=" + repository + "\n\nINPUT\n)\n" +
					"case \"$credential\" in *password=commit-fixture-secret*) ;; *) exit 1 ;; esac\n" +
					"printf '%s\\n' \"$credential\"\n"
				if err := os.WriteFile(filepath.Join(root, ".git/hooks/pre-commit"), []byte(hook), 0700); err != nil {
					t.Fatal(err)
				}
			}
			put(t, root, "file", "staged snapshot")
			git(t, root, "add", "file")
			put(t, root, "file", "newer unstaged work")
			spec := registry.GitSpec{ID: "commit", ProjectID: "project", Path: root, Action: "commit", Message: "Keep this draft"}
			if _, err := s.Start(spec, &c, auth); err != nil {
				t.Fatal(err)
			}
			result := wait(t, s, spec.ID)
			if scenario == "missing" || scenario == "other-repository" {
				if result.Status != "failed" || result.ErrorCode != "git_auth_required" {
					t.Fatalf("missing auth recovery: %+v", result)
				}
				if git(t, root, "show", ":file") != "staged snapshot" {
					t.Fatal("failed hook changed staged work")
				}
			} else if result.Status != "succeeded" || git(t, root, "show", "HEAD:file") != "staged snapshot" {
				t.Fatalf("commit did not preserve its staged snapshot: %+v", result)
			}
			if contents(t, root, "file") != "newer unstaged work" {
				t.Fatal("commit overwrote work in progress")
			}
			data, _ := json.Marshal(result)
			if strings.Contains(string(data), "commit-fixture-secret") || strings.Contains(string(data), "wrong-account") {
				t.Fatal("operation output exposed credentials")
			}
		})
	}
}
