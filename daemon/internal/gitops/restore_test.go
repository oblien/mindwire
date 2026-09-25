package gitops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func restoreFixture(t *testing.T) (*Service, string, registry.GitSpec) {
	t.Helper()
	s, _, root := fixture(t)
	put(t, root, "tracked", "earlier contents\n")
	put(t, root, "removed", "bring this file back\n")
	put(t, root, ".gitignore", "private\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "earlier version")
	target := git(t, root, "rev-parse", "HEAD")
	put(t, root, "tracked", "current contents\n")
	put(t, root, "added", "newer file\n")
	git(t, root, "rm", "removed")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "current version")
	return s, root, registry.GitSpec{ID: "restore", ProjectID: "project", Path: root, Action: "restore_commit",
		CommitID: target, ExpectedHead: git(t, root, "rev-parse", "HEAD"), ExpectedBranch: git(t, root, "branch", "--show-current")}
}

func TestRestoreStagesTheExactVersionAndPreservesHistoryAndRetryIdentity(t *testing.T) {
	s, root, spec := restoreFixture(t)
	put(t, root, "private", "keep ignored local data\n")
	if _, err := s.Start(spec, nil, nil); err != nil {
		t.Fatal(err)
	}
	result := wait(t, s, spec.ID)
	if result.Status != "succeeded" {
		t.Fatalf("restore failed: %+v", result)
	}
	if got := git(t, root, "write-tree"); got != git(t, root, "rev-parse", spec.CommitID+"^{tree}") {
		t.Fatalf("index does not match the selected version: %s", got)
	}
	if git(t, root, "rev-parse", "HEAD") != spec.ExpectedHead || git(t, root, "branch", "--show-current") != spec.ExpectedBranch {
		t.Fatal("restore rewrote commit history or changed the branch")
	}
	if git(t, root, "diff", "--name-only") != "" || contents(t, root, "tracked") != "earlier contents\n" ||
		contents(t, root, "removed") != "bring this file back\n" || contents(t, root, "private") != "keep ignored local data\n" {
		t.Fatal("restore did not update tracked contents or preserve ignored files")
	}
	if _, err := os.Stat(filepath.Join(root, "added")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("newer tracked file was not removed")
	}
	put(t, root, "tracked", "edited after restore\n")
	replayed, err := s.Start(spec, nil, nil)
	if err != nil || replayed.Sequence != result.Sequence || contents(t, root, "tracked") != "edited after restore\n" {
		t.Fatalf("retry executed restoration again: %+v %v", replayed, err)
	}
	spec.CommitID = spec.ExpectedHead
	if _, err := s.Start(spec, nil, nil); !errors.Is(err, registry.ErrConflict) {
		t.Fatalf("changed retry accepted: %v", err)
	}
}

func TestRestoreRejectsDirtyOrChangedRepositoriesWithoutWriting(t *testing.T) {
	for _, kind := range []string{"working", "staged", "untracked", "head", "branch", "merge", "rebase", "missing-commit", "blob"} {
		t.Run(kind, func(t *testing.T) {
			s, root, spec := restoreFixture(t)
			wantCode := "git_restore_dirty"
			switch kind {
			case "working":
				put(t, root, "tracked", "keep local edits\n")
			case "staged":
				put(t, root, "tracked", "keep staged edits\n")
				git(t, root, "add", "tracked")
			case "untracked":
				put(t, root, "removed", "keep untracked data\n")
			case "head":
				git(t, root, "commit", "--allow-empty", "-qm", "new tip")
				wantCode = "git_restore_changed"
			case "branch":
				git(t, root, "switch", "-c", "another")
				wantCode = "git_restore_changed"
			case "merge":
				put(t, root, ".git/MERGE_HEAD", spec.CommitID+"\n")
				wantCode = "git_restore_in_progress"
			case "rebase":
				if err := os.Mkdir(filepath.Join(root, ".git/rebase-merge"), 0700); err != nil {
					t.Fatal(err)
				}
				wantCode = "git_restore_in_progress"
			case "missing-commit":
				spec.CommitID = strings.Repeat("a", 40)
				wantCode = "git_restore_commit_unavailable"
			case "blob":
				spec.CommitID = git(t, root, "rev-parse", "HEAD:tracked")
				wantCode = "git_restore_commit_unavailable"
			}
			head, index, before := git(t, root, "rev-parse", "HEAD"), git(t, root, "write-tree"), contents(t, root, "tracked")
			if _, err := s.Start(spec, nil, nil); err != nil {
				t.Fatal(err)
			}
			result := wait(t, s, spec.ID)
			if result.Status != "failed" || result.ErrorCode != wantCode {
				t.Fatalf("unexpected rejection: %+v", result)
			}
			if git(t, root, "rev-parse", "HEAD") != head || git(t, root, "write-tree") != index || contents(t, root, "tracked") != before {
				t.Fatal("rejected restoration changed the repository")
			}
			if kind == "untracked" && contents(t, root, "removed") != "keep untracked data\n" {
				t.Fatal("untracked data was replaced")
			}
		})
	}
}

func TestRestoreRefusesIgnoredFileCollisionsAndExistingIndexLocks(t *testing.T) {
	for _, kind := range []string{"ignored-file", "ignored-directory", "ignored-case", "index-lock"} {
		t.Run(kind, func(t *testing.T) {
			s, root, spec := restoreFixture(t)
			collision := kind != "index-lock"
			localPath := "private"
			if collision {
				// The old version tracks a file that is now ignored and local-only.
				git(t, root, "switch", "--detach", spec.CommitID)
				targetPath := "private"
				if kind == "ignored-case" {
					targetPath = "PRIVATE"
					git(t, root, "config", "core.ignorecase", "true")
				}
				put(t, root, targetPath, "older tracked data\n")
				git(t, root, "add", "-f", targetPath)
				git(t, root, "commit", "-qm", "tracked private file")
				spec.CommitID = git(t, root, "rev-parse", "HEAD")
				git(t, root, "switch", spec.ExpectedBranch)
				if kind == "ignored-directory" {
					if err := os.Mkdir(filepath.Join(root, "private"), 0700); err != nil {
						t.Fatal(err)
					}
					localPath = "private/local-data"
				}
				put(t, root, localPath, "keep local private data\n")
			} else {
				put(t, root, ".git/index.lock", "another Git command\n")
			}
			if _, err := s.Start(spec, nil, nil); err != nil {
				t.Fatal(err)
			}
			result := wait(t, s, spec.ID)
			if result.Status != "failed" || contents(t, root, "tracked") != "current contents\n" ||
				git(t, root, "diff", "--cached", "--name-only") != "" {
				t.Fatalf("unsafe restoration: %+v", result)
			}
			if collision && contents(t, root, localPath) != "keep local private data\n" {
				t.Fatal("ignored data was overwritten")
			}
		})
	}
}

func TestRestoreObjectIDsCannotBeOptionsOrRevisionExpressions(t *testing.T) {
	for _, value := range []string{"", "HEAD", "HEAD~1", "--reset", strings.Repeat("g", 40), strings.Repeat("a", 39)} {
		spec := registry.GitSpec{ID: "invalid", ProjectID: "project", Action: "restore_commit", CommitID: value, ExpectedHead: strings.Repeat("b", 40)}
		if err := Normalize(&spec); !errors.Is(err, registry.ErrInvalid) {
			t.Fatalf("accepted commit %q", value)
		}
	}
	spec := registry.GitSpec{ID: "invalid", ProjectID: "project", Action: "push", CommitID: strings.Repeat("a", 40)}
	if err := Normalize(&spec); !errors.Is(err, registry.ErrInvalid) {
		t.Fatal("restore fields accepted on another action")
	}
}
