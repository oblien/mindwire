package gitops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func TestMissingAuthorRequestsSetupAndRetryCommitsExactlyOnce(t *testing.T) {
	s, store, root := fixture(t)
	git(t, root, "config", "--unset", "user.name")
	git(t, root, "config", "--unset", "user.email")
	git(t, root, "config", "user.useConfigOnly", "true")
	put(t, root, "file", "staged contents\n")
	git(t, root, "add", "file")
	put(t, root, "file", "working changes stay\n")
	failed := run(t, s, root, "missing-author", "commit", nil, "Preserve my message")
	if failed.Status != "failed" || failed.ErrorCode != "git_identity_required" {
		t.Fatalf("missing actionable author failure: %+v", failed)
	}
	if git(t, root, "ls-files") != "file" || contents(t, root, "file") != "working changes stay\n" {
		t.Fatal("author setup modified the index or working copy")
	}
	stored, err := store.GitOperation(failed.ID)
	if err != nil || stored.ErrorCode != failed.ErrorCode || stored.Message != "Preserve my message" {
		t.Fatalf("reconnection lost setup state: %+v %v", stored, err)
	}
	identity := &registry.GitIdentity{Name: "Chosen User", Email: "chosen@example.invalid"}
	spec := registry.GitSpec{ID: "author-confirmed", ProjectID: "project", Path: root, Action: "commit", Message: failed.Message, Identity: identity}
	if _, err := s.Start(spec, nil, nil); err != nil {
		t.Fatal(err)
	}
	result := wait(t, s, spec.ID)
	if result.Status != "succeeded" {
		t.Fatalf("commit after author setup: %+v", result)
	}
	if _, err := s.Start(spec, nil, nil); err != nil {
		t.Fatal(err)
	}
	if git(t, root, "rev-list", "--count", "HEAD") != "1" {
		t.Fatal("retry duplicated commit")
	}
	if git(t, root, "show", "HEAD:file") != "staged contents" || contents(t, root, "file") != "working changes stay\n" {
		t.Fatal("author setup committed unstaged changes")
	}
	if git(t, root, "log", "-1", "--format=%an <%ae>") != "Chosen User <chosen@example.invalid>" {
		t.Fatal("wrong attribution")
	}
	if git(t, root, "config", "--local", "user.name") != identity.Name {
		t.Fatal("author wasn't saved to repository")
	}
	spec.Identity = &registry.GitIdentity{Name: "Different User", Email: "different@example.invalid"}
	if _, err := s.Start(spec, nil, nil); !errors.Is(err, registry.ErrConflict) {
		t.Fatal("retry changed author intent")
	}
}

func TestExplicitAuthorNeverChangesGlobalIdentityOrMistakesHookFailureForSetup(t *testing.T) {
	s, _, root := fixture(t)
	global := filepath.Join(t.TempDir(), "gitconfig")
	original := "[user]\n\tname = Global User\n\temail = global@example.invalid\n"
	if err := os.WriteFile(global, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	hook := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'Author identity unknown: from a failing hook' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	put(t, root, "file", "contents")
	git(t, root, "add", "file")
	spec := registry.GitSpec{ID: "hook-failure", ProjectID: "project", Path: root, Action: "commit", Message: "message",
		Identity: &registry.GitIdentity{Name: "Repository User", Email: "repository@example.invalid"}}
	if _, err := s.Start(spec, nil, nil); err != nil {
		t.Fatal(err)
	}
	result := wait(t, s, spec.ID)
	if result.Status != "failed" || result.ErrorCode != "git_command_failed" || !strings.Contains(result.Error, "failing hook") {
		t.Fatalf("hook failure misclassified: %+v", result)
	}
	data, err := os.ReadFile(global)
	if err != nil || string(data) != original {
		t.Fatal("changed global Git identity")
	}
}

func TestCommitIdentityValidation(t *testing.T) {
	for _, identity := range []registry.GitIdentity{
		{Name: "", Email: "u@example.invalid"}, {Name: "User\nOther", Email: "u@example.invalid"},
		{Name: "User", Email: "not-an-email"}, {Name: "User", Email: "a@b@c"},
		{Name: "User", Email: "u@\u00a0example.invalid"}, {Name: "<User>", Email: "u@example.invalid"},
	} {
		spec := registry.GitSpec{ID: "invalid", ProjectID: "project", Action: "commit", Message: "message", Identity: &identity}
		if err := Normalize(&spec); !errors.Is(err, registry.ErrInvalid) {
			t.Fatalf("accepted %+v", identity)
		}
	}
	spec := registry.GitSpec{ID: "invalid-action", ProjectID: "project", Action: "fetch",
		Identity: &registry.GitIdentity{Name: "User", Email: "u@example.invalid"}}
	if err := Normalize(&spec); !errors.Is(err, registry.ErrInvalid) {
		t.Fatal("identity accepted for non-commit action")
	}
}
