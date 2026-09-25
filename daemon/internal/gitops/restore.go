package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

var (
	ErrRestoreDirty      = errors.New("Commit or stash your local changes before restoring a version.")
	ErrRestoreChanged    = errors.New("The current branch changed. Review it before restoring this version.")
	ErrRestoreInProgress = errors.New("Finish the current merge, rebase, or other Git operation before restoring a version.")
	ErrRestoreCommit     = errors.New("This commit is no longer available. Refresh commit history and try again.")
	ErrRestoreLocalFiles = errors.New("Local ignored files would be replaced by this version. Move them before restoring.")
)

// ActionVersion describes the closed action enum, independently of optional fields.
func ActionVersion(action string) int {
	switch action {
	case "restore_commit":
		return 4
	case "switch_branch", "create_branch":
		return 2
	default:
		return 1
	}
}

func validObjectID(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func restoreErrorCode(err error) string {
	for _, entry := range []struct {
		err  error
		code string
	}{
		{ErrRestoreDirty, "git_restore_dirty"},
		{ErrRestoreChanged, "git_restore_changed"},
		{ErrRestoreInProgress, "git_restore_in_progress"},
		{ErrRestoreCommit, "git_restore_commit_unavailable"},
		{ErrRestoreLocalFiles, "git_restore_local_files"},
	} {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	return ""
}

// Restoration is a staged tree change. A two-tree read preserves HEAD and uses
// Git's normal index lock and overwrite checks. Ignored files need an explicit
// collision check because read-tree otherwise permits overwriting them.
// Never use reset --hard, forced checkout, or clean here.
func restoreCommit(ctx context.Context, spec registry.GitSpec) (gitaccess.Result, error) {
	command := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs", "-C", spec.Path}, args...)...)
		cmd.Env = gitEnvironment(nil)
		proc.Group(cmd)
		return cmd
	}
	read := func(args ...string) (string, error) {
		data, err := command(args...).Output()
		return strings.TrimSpace(string(data)), err
	}
	head, err := read("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || head != spec.ExpectedHead {
		return gitaccess.Result{}, ErrRestoreChanged
	}
	branch, err := read("symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return gitaccess.Result{}, ErrRestoreChanged
		}
	}
	expectedBranch := ""
	if spec.ExpectedBranch != "" {
		expectedBranch = "refs/heads/" + spec.ExpectedBranch
	}
	if branch != expectedBranch {
		return gitaccess.Result{}, ErrRestoreChanged
	}
	objectType, err := read("cat-file", "-t", spec.CommitID)
	if err != nil || objectType != "commit" {
		return gitaccess.Result{}, ErrRestoreCommit
	}
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer", "BISECT_LOG"} {
		path, err := read("rev-parse", "--git-path", marker)
		if err != nil {
			return gitaccess.Result{}, fmt.Errorf("could not inspect the current Git operation")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(spec.Path, path)
		}
		if _, err = os.Lstat(path); err == nil {
			return gitaccess.Result{}, ErrRestoreInProgress
		} else if !errors.Is(err, os.ErrNotExist) {
			return gitaccess.Result{}, err
		}
	}
	// --ignore-submodules=none also protects edits inside populated submodules.
	dirty, err := read("status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return gitaccess.Result{}, fmt.Errorf("could not inspect local changes before restoring")
	}
	if dirty != "" {
		return gitaccess.Result{}, ErrRestoreDirty
	}
	ignored, err := command("ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "--no-empty-directory", "-z").Output()
	if err != nil {
		return gitaccess.Result{}, fmt.Errorf("could not inspect ignored local files")
	}
	if len(ignored) > 0 {
		target, err := command("ls-tree", "--full-tree", "--name-only", "-r", "-z", spec.CommitID).Output()
		if err != nil {
			return gitaccess.Result{}, ErrRestoreCommit
		}
		ignoredPaths, targetPaths := string(ignored), string(target)
		ignoreCase, err := read("config", "--bool", "--get", "core.ignorecase")
		if err != nil {
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				return gitaccess.Result{}, fmt.Errorf("could not inspect filesystem case handling")
			}
		}
		if ignoreCase == "true" {
			ignoredPaths, targetPaths = strings.ToLower(ignoredPaths), strings.ToLower(targetPaths)
		}
		if ignoredRestoreCollision(ignoredPaths, targetPaths) {
			return gitaccess.Result{}, ErrRestoreLocalFiles
		}
	}
	output := &tailOutput{}
	cmd := command("read-tree", "-m", "-u", "--no-recurse-submodules", head, spec.CommitID)
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return gitaccess.Result{}, ctx.Err()
		}
		text := gitaccess.Redact(output.String(), gitaccess.Auth{})
		if text == "" {
			text = err.Error()
		}
		return gitaccess.Result{}, fmt.Errorf("could not restore this version: %s", text)
	}
	return gitaccess.Result{}, nil
}

func ignoredRestoreCollision(ignored, target string) bool {
	files, directories := map[string]bool{}, map[string]bool{}
	for _, name := range strings.Split(target, "\x00") {
		if name == "" {
			continue
		}
		files[name] = true
		for i := range name {
			if name[i] == '/' {
				directories[name[:i]] = true
			}
		}
	}
	for _, name := range strings.Split(ignored, "\x00") {
		name = strings.TrimSuffix(name, "/")
		if name == "" {
			continue
		}
		if files[name] || directories[name] {
			return true
		}
		// A tracked file may replace a directory that contains ignored local data.
		for i := range name {
			if name[i] == '/' && files[name[:i]] {
				return true
			}
		}
	}
	return false
}
