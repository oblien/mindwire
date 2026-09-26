package gitops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

var (
	ErrBranchChanged  = errors.New("This branch changed or was removed. Refresh the branch list before trying again.")
	ErrBranchInUse    = errors.New("This branch is checked out. Switch to another branch before deleting it; branches open in another worktree must be managed there.")
	ErrBranchUnmerged = errors.New("This branch has unmerged commits. Merge them first, or explicitly delete the unmerged branch.")
)

func branchErrorCode(err error) string {
	for _, item := range []struct {
		err  error
		code string
	}{
		{ErrBranchChanged, "git_branch_changed"},
		{ErrBranchInUse, "git_branch_in_use"},
		{ErrBranchUnmerged, "git_branch_unmerged"},
	} {
		if errors.Is(err, item.err) {
			return item.code
		}
	}
	return ""
}

// Keep branch/config/reflog handling in native Git. The daemon reservation prevents
// overlapping Mindwire writes; the expected tip rejects stale selections. Git's own
// ref locks and worktree checks remain in force, including for an explicit -D.
func manageBranch(ctx context.Context, spec registry.GitSpec) (gitaccess.Result, error) {
	command := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", spec.Path}, args...)...)
		cmd.Env = gitEnvironment(nil)
		proc.Group(cmd)
		return cmd
	}
	ref := "refs/heads/" + spec.Branch
	tip, err := command("rev-parse", "--verify", "--quiet", ref+"^{commit}").Output()
	if err != nil || strings.TrimSpace(string(tip)) != spec.ExpectedTip {
		return gitaccess.Result{}, ErrBranchChanged
	}
	worktrees, err := command("worktree", "list", "--porcelain", "-z").Output()
	if err != nil {
		return gitaccess.Result{}, fmt.Errorf("could not check branches open in other worktrees")
	}
	var worktree string
	for _, field := range strings.Split(string(worktrees), "\x00") {
		if path, ok := strings.CutPrefix(field, "worktree "); ok {
			worktree, err = workspacepath.Canonical(path)
			if err != nil {
				return gitaccess.Result{}, err
			}
		}
		if field == "branch "+ref && (spec.Action == "delete_branch" || worktree != spec.Path) {
			return gitaccess.Result{}, ErrBranchInUse
		}
	}
	args := []string{"branch", "-m", "--", spec.Branch, spec.NewName}
	if spec.Action == "delete_branch" {
		flag := "-d"
		if spec.Force {
			flag = "-D"
		}
		args = []string{"branch", flag, "--", spec.Branch}
	}
	output := &tailOutput{}
	cmd := command(args...)
	cmd.Stdout, cmd.Stderr = output, output
	err = cmd.Run()
	if ctx.Err() != nil {
		return gitaccess.Result{}, ctx.Err()
	}
	text := gitaccess.Redact(output.String(), gitaccess.Auth{})
	if err != nil {
		if spec.Action == "delete_branch" && !spec.Force && strings.Contains(text, "is not fully merged") {
			return gitaccess.Result{}, ErrBranchUnmerged
		}
		if text == "" {
			text = err.Error()
		}
		return gitaccess.Result{}, errors.New(text)
	}
	return gitaccess.Result{Output: text}, nil
}
