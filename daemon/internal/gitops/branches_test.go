package gitops

import (
	"errors"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func branch(t *testing.T, s *Service, root, id, action, name string, remote bool) registry.GitOperation {
	t.Helper()
	_, err := s.Start(registry.GitSpec{ID: id, ProjectID: "project", Path: root, Action: action, Branch: name, Remote: remote}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return wait(t, s, id)
}

func TestBranchesPreserveUncommittedFilesAndDeduplicateIntent(t *testing.T) {
	s, _, root := fixture(t)
	put(t, root, "tracked", "base\n")
	git(t, root, "add", "tracked")
	git(t, root, "commit", "-qm", "base")
	original := git(t, root, "branch", "--show-current")
	put(t, root, "tracked", "unsaved changes\n")
	put(t, root, "untracked", "keep this too\n")
	created := branch(t, s, root, "create-branch", "create_branch", "feature/native-review", false)
	if created.Status != "succeeded" || git(t, root, "branch", "--show-current") != created.Branch {
		t.Fatalf("create and switch: %+v", created)
	}
	switched := branch(t, s, root, "switch-back", "switch_branch", original, false)
	if switched.Status != "succeeded" || contents(t, root, "tracked") != "unsaved changes\n" || contents(t, root, "untracked") != "keep this too\n" {
		t.Fatalf("switch lost uncommitted work: %+v", switched)
	}
	// Replaying the accepted creation must not switch the repository a second time.
	duplicate := branch(t, s, root, "create-branch", "create_branch", "feature/native-review", false)
	if duplicate.Sequence != created.Sequence || git(t, root, "branch", "--show-current") != original {
		t.Fatal("duplicate branch request executed again")
	}
	_, err := s.Start(registry.GitSpec{ID: created.ID, ProjectID: "project", Path: root, Action: "create_branch", Branch: "another"}, nil, nil)
	if !errors.Is(err, registry.ErrConflict) {
		t.Fatalf("reused ID accepted another branch: %v", err)
	}
	failed := branch(t, s, root, "duplicate-name", "create_branch", created.Branch, false)
	if failed.Status != "failed" || git(t, root, "branch", "--show-current") != original || contents(t, root, "tracked") != "unsaved changes\n" {
		t.Fatalf("existing branch was reset: %+v", failed)
	}
}

func TestBranchSwitchRefusesConflictsAndCanTrackRemoteRefs(t *testing.T) {
	s, _, root := fixture(t)
	put(t, root, "tracked", "base\n")
	git(t, root, "add", "tracked")
	git(t, root, "commit", "-qm", "base")
	original := git(t, root, "branch", "--show-current")
	git(t, root, "switch", "-c", "different")
	put(t, root, "tracked", "other branch\n")
	git(t, root, "commit", "-qam", "different contents")
	git(t, root, "switch", original)
	put(t, root, "tracked", "my unfinished work\n")
	failed := branch(t, s, root, "conflict", "switch_branch", "different", false)
	if failed.Status != "failed" || !strings.Contains(failed.Error, "overwritten") ||
		git(t, root, "branch", "--show-current") != original || contents(t, root, "tracked") != "my unfinished work\n" {
		t.Fatalf("branch conflict was not preserved: %+v", failed)
	}
	git(t, root, "remote", "add", "origin", root)
	git(t, root, "update-ref", "refs/remotes/origin/release", "HEAD")
	tracked := branch(t, s, root, "track-remote", "switch_branch", "origin/release", true)
	if tracked.Status != "succeeded" || git(t, root, "branch", "--show-current") != "release" ||
		git(t, root, "rev-parse", "--abbrev-ref", "@{upstream}") != "origin/release" ||
		contents(t, root, "tracked") != "my unfinished work\n" {
		t.Fatalf("remote tracking branch: %+v", tracked)
	}
}

func TestBranchNamesAreLiteralAndCannotBecomeFlagsOrRevisionExpressions(t *testing.T) {
	for _, name := range []string{"", "HEAD", "@", "-C", "--discard-changes", "@{-1}", "main~1", "../outside", "a..b", "a b", "a\nb", "a\x00b", "/main", "main/", "a//b", ".hidden", "a/.hidden", "a.lock", "a\\b"} {
		spec := registry.GitSpec{ID: "invalid", ProjectID: "project", Action: "switch_branch", Branch: name}
		if err := Normalize(&spec); !errors.Is(err, registry.ErrInvalid) {
			t.Fatalf("accepted invalid branch %q: %v", name, err)
		}
	}
	for _, name := range []string{"main", "feature/review", "feature/日本語", "literal;name", "literal$(value)"} {
		spec := registry.GitSpec{ID: "valid", ProjectID: "project", Action: "create_branch", Branch: name}
		if err := Normalize(&spec); err != nil {
			t.Fatalf("rejected literal branch %q: %v", name, err)
		}
	}
}
