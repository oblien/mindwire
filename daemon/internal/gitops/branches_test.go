package gitops

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func manage(t *testing.T, s *Service, root, id, action, name, newName, tip string, force bool) registry.GitOperation {
	t.Helper()
	_, err := s.Start(registry.GitSpec{ID: id, ProjectID: "project", Path: root, Action: action,
		Branch: name, NewName: newName, ExpectedTip: tip, Force: force}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return wait(t, s, id)
}

func TestBranchManagementRenamesWithoutSwitchingAndReplaysReceipt(t *testing.T) {
	s, _, root := fixture(t)
	put(t, root, "tracked", "base\n")
	git(t, root, "add", "tracked")
	git(t, root, "commit", "-qm", "base")
	current := git(t, root, "branch", "--show-current")
	tip := git(t, root, "rev-parse", "HEAD")
	git(t, root, "branch", "feature/old")
	git(t, root, "config", "branch.feature/old.description", "keep my settings")
	put(t, root, "tracked", "unfinished work\n")
	renamed := manage(t, s, root, "rename", "rename_branch", "feature/old", "feature/new", tip, false)
	if renamed.Status != "succeeded" || git(t, root, "branch", "--show-current") != current ||
		git(t, root, "rev-parse", "feature/new") != tip ||
		git(t, root, "config", "branch.feature/new.description") != "keep my settings" ||
		contents(t, root, "tracked") != "unfinished work\n" {
		t.Fatalf("rename changed repository state: %+v", renamed)
	}
	// A repeated request observes the accepted result even though the old ref is gone.
	duplicate := manage(t, s, root, "rename", "rename_branch", "feature/old", "feature/new", tip, false)
	if duplicate.Sequence != renamed.Sequence {
		t.Fatal("rename was executed twice")
	}
	conflict := manage(t, s, root, "rename-conflict", "rename_branch", "feature/new", current, tip, false)
	if conflict.Status != "failed" || git(t, root, "rev-parse", "feature/new") != tip {
		t.Fatalf("rename overwrote an existing branch: %+v", conflict)
	}
	activeRename := manage(t, s, root, "rename-current", "rename_branch", current, "renamed-current", tip, false)
	if activeRename.Status != "succeeded" || git(t, root, "branch", "--show-current") != "renamed-current" {
		t.Fatalf("current branch rename: %+v", activeRename)
	}
}

func TestBranchDeletionRequiresExplicitUnmergedConfirmationAndProtectsWorktrees(t *testing.T) {
	s, _, root := fixture(t)
	put(t, root, "tracked", "base\n")
	git(t, root, "add", "tracked")
	git(t, root, "commit", "-qm", "base")
	current := git(t, root, "branch", "--show-current")
	base := git(t, root, "rev-parse", "HEAD")
	git(t, root, "switch", "-c", "unfinished")
	put(t, root, "tracked", "not merged\n")
	git(t, root, "commit", "-qam", "unmerged")
	tip := git(t, root, "rev-parse", "HEAD")
	git(t, root, "switch", current)
	blocked := manage(t, s, root, "delete-unmerged", "delete_branch", "unfinished", "", tip, false)
	if blocked.ErrorCode != "git_branch_unmerged" || git(t, root, "rev-parse", "unfinished") != tip {
		t.Fatalf("unmerged work was deleted: %+v", blocked)
	}
	stale := manage(t, s, root, "delete-stale", "delete_branch", "unfinished", "", base, true)
	if stale.ErrorCode != "git_branch_changed" {
		t.Fatalf("stale force confirmation accepted: %+v", stale)
	}
	forced := manage(t, s, root, "delete-confirmed", "delete_branch", "unfinished", "", tip, true)
	if forced.Status != "succeeded" || git(t, root, "branch", "--list", "unfinished") != "" {
		t.Fatalf("explicit deletion failed: %+v", forced)
	}
	active := manage(t, s, root, "delete-current", "delete_branch", current, "", base, true)
	if active.ErrorCode != "git_branch_in_use" {
		t.Fatalf("current branch deleted: %+v", active)
	}
	git(t, root, "branch", "worktree-branch")
	git(t, root, "worktree", "add", filepath.Join(t.TempDir(), "other"), "worktree-branch")
	for _, action := range []string{"rename_branch", "delete_branch"} {
		name := ""
		if action == "rename_branch" {
			name = "another-name"
		}
		o := manage(t, s, root, action+"-worktree", action, "worktree-branch", name, base, false)
		if o.ErrorCode != "git_branch_in_use" {
			t.Fatalf("branch in another worktree was modified: %+v", o)
		}
	}
	git(t, root, "branch", "merged")
	deleted := manage(t, s, root, "delete-merged", "delete_branch", "merged", "", base, false)
	if deleted.Status != "succeeded" || git(t, root, "branch", "--list", "merged") != "" {
		t.Fatalf("merged branch deletion failed: %+v", deleted)
	}
}

func TestBranchManagementRejectsUnrelatedOptions(t *testing.T) {
	valid := registry.GitSpec{ID: "request", ProjectID: "project", Action: "rename_branch", Branch: "old", NewName: "new", ExpectedTip: strings.Repeat("a", 40)}
	for _, change := range []func(*registry.GitSpec){
		func(s *registry.GitSpec) { s.Force = true },
		func(s *registry.GitSpec) { s.Remote = true },
		func(s *registry.GitSpec) { s.NewName = "--force" },
		func(s *registry.GitSpec) { s.NewName = s.Branch },
		func(s *registry.GitSpec) { s.ExpectedTip = "HEAD" },
		func(s *registry.GitSpec) { s.Action = "stage"; s.Paths = []string{"file"} },
	} {
		spec := valid
		change(&spec)
		if err := Normalize(&spec); !errors.Is(err, registry.ErrInvalid) {
			t.Fatalf("accepted %+v: %v", spec, err)
		}
	}
}

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
