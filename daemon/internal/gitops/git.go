package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

func Network(action string) bool { return action == "fetch" || action == "pull" || action == "push" }

var ErrIdentityRequired = errors.New("Set your Git author name and email before committing.")

func Normalize(spec *registry.GitSpec) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", registry.ErrInvalid, reason) }
	if spec.ID == "" || len(spec.ID) > 128 || spec.ProjectID == "" {
		return invalid("an operation ID and project are required")
	}
	for _, c := range spec.ID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return invalid("invalid operation ID")
		}
	}
	if len(spec.Paths) > 2000 {
		return invalid("too many Git paths")
	}
	for _, path := range spec.Paths {
		if path == "" || len(path) > 4096 || filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
			return invalid("Git paths must be relative files inside the repository")
		}
		for _, part := range strings.Split(filepath.ToSlash(path), "/") {
			if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
				return invalid("Git paths must stay inside the working tree")
			}
		}
	}
	paths := append([]string(nil), spec.Paths...)
	sort.Strings(paths)
	spec.Paths = nil
	for _, p := range paths {
		if len(spec.Paths) == 0 || p != spec.Paths[len(spec.Paths)-1] {
			spec.Paths = append(spec.Paths, p)
		}
	}
	spec.Message = strings.TrimSpace(spec.Message)
	if len(spec.Message) > 65536 || strings.ContainsRune(spec.Message, 0) {
		return invalid("invalid commit message")
	}
	if spec.Identity != nil {
		identity := *spec.Identity
		identity.Name, identity.Email = strings.TrimSpace(identity.Name), strings.TrimSpace(identity.Email)
		if spec.Action != "commit" || !validIdentity(identity) {
			return invalid("a commit author needs a valid name and email")
		}
		spec.Identity = &identity
	}
	manageBranch := spec.Action == "rename_branch" || spec.Action == "delete_branch"
	branchAction := spec.Action == "switch_branch" || spec.Action == "create_branch" || manageBranch
	if !branchAction && (spec.Branch != "" || spec.Remote) {
		return invalid("this Git operation does not accept a branch")
	}
	if !manageBranch && (spec.NewName != "" || spec.ExpectedTip != "" || spec.Force) {
		return invalid("only branch management accepts a new name, expected tip or force flag")
	}
	if spec.Action != "restore_commit" && (spec.CommitID != "" || spec.ExpectedHead != "" || spec.ExpectedBranch != "") {
		return invalid("only commit restoration accepts a source commit and expected repository state")
	}
	switch spec.Action {
	case "stage", "unstage", "discard":
		if len(spec.Paths) == 0 || spec.Message != "" {
			return invalid("select files for this Git operation")
		}
	case "commit":
		if spec.Message == "" || len(spec.Paths) != 0 {
			return invalid("a commit message is required")
		}
	case "fetch", "pull", "push":
		if len(spec.Paths) != 0 || spec.Message != "" {
			return invalid("network Git operations do not accept files or messages")
		}
	case "switch_branch", "create_branch":
		if len(spec.Paths) != 0 || spec.Message != "" || !validBranchName(spec.Branch) {
			return invalid("select a valid Git branch name")
		}
		if spec.Remote {
			_, local, found := strings.Cut(spec.Branch, "/")
			if spec.Action != "switch_branch" || !found || !validBranchName(local) {
				return invalid("select a remote tracking branch")
			}
		}
	case "rename_branch", "delete_branch":
		if len(spec.Paths) != 0 || spec.Message != "" || spec.Remote || !validBranchName(spec.Branch) || !validObjectID(spec.ExpectedTip) {
			return invalid("branch management requires a local branch and its full expected commit ID")
		}
		if spec.Action == "rename_branch" && (!validBranchName(spec.NewName) || spec.NewName == spec.Branch || spec.Force) {
			return invalid("rename requires a different valid branch name and cannot force an overwrite")
		}
		if spec.Action == "delete_branch" && spec.NewName != "" {
			return invalid("branch deletion does not accept a new name")
		}
		spec.ExpectedTip = strings.ToLower(spec.ExpectedTip)
	case "restore_commit":
		if len(spec.Paths) != 0 || spec.Message != "" || !validObjectID(spec.CommitID) || !validObjectID(spec.ExpectedHead) ||
			(spec.ExpectedBranch != "" && !validBranchName(spec.ExpectedBranch)) {
			return invalid("restoration requires full commit IDs and the current branch")
		}
		spec.CommitID = strings.ToLower(spec.CommitID)
		spec.ExpectedHead = strings.ToLower(spec.ExpectedHead)
	default:
		return invalid("unsupported Git operation")
	}
	return nil
}

// Validate literal branch names, never revision expressions such as @{-1} or command flags.
func validBranchName(name string) bool {
	if name == "" || name == "HEAD" || name == "@" || len(name) > 1024 || strings.HasPrefix(name, "-") ||
		strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") ||
		strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.Contains(name, "//") ||
		strings.ContainsAny(name, " ~^:?*[\\") {
		return false
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func Root(ctx context.Context, directory string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "git", "-C", directory, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%w: this project is not a Git working tree", registry.ErrInvalid)
	}
	return workspacepath.Canonical(strings.TrimSuffix(string(data), "\n"))
}

func (s *Service) execute(ctx context.Context, spec registry.GitSpec, c *gitaccess.Connection, auth *gitaccess.Auth) (gitaccess.Result, error) {
	if Network(spec.Action) {
		return s.access.Run(ctx, spec.Path, spec.Action, c, auth)
	}
	if spec.Action == "restore_commit" {
		return restoreCommit(ctx, spec)
	}
	if spec.Action == "rename_branch" || spec.Action == "delete_branch" {
		return manageBranch(ctx, spec)
	}
	output := &tailOutput{}
	command := func(input string, args ...string) error {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs", "-C", spec.Path}, args...)...)
		proc.Group(cmd)
		cmd.Env = gitEnvironment(spec.Identity)
		if input != "" {
			cmd.Stdin = strings.NewReader(input)
		}
		cmd.Stdout, cmd.Stderr = output, output
		return cmd.Run()
	}
	var err error
	switch spec.Action {
	case "stage":
		err = command("", append([]string{"add", "-A", "--"}, spec.Paths...)...)
	case "unstage":
		// An unborn branch has an index but no HEAD to reset to. Removing index
		// entries with --cached preserves every file in the working tree.
		probe := exec.CommandContext(ctx, "git", "-C", spec.Path, "rev-parse", "--verify", "--quiet", "HEAD")
		if check := probe.Run(); check == nil {
			err = command("", append([]string{"reset", "--quiet", "HEAD", "--"}, spec.Paths...)...)
		} else if exit, ok := check.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			err = command("", append([]string{"rm", "--cached", "-r", "-f", "--ignore-unmatch", "--"}, spec.Paths...)...)
		} else {
			err = fmt.Errorf("could not inspect this repository's HEAD")
		}
	case "discard":
		var tracked, untracked []string
		for _, path := range spec.Paths {
			data, check := exec.CommandContext(ctx, "git", "--literal-pathspecs", "-C", spec.Path, "ls-files", "-z", "--", path).Output()
			if check != nil {
				err = fmt.Errorf("could not check whether the selected files are tracked")
				break
			}
			if len(data) == 0 {
				untracked = append(untracked, path)
			} else {
				tracked = append(tracked, path)
			}
		}
		if err == nil && len(tracked) > 0 {
			err = command("", append([]string{"checkout", "--"}, tracked...)...)
		}
		if err == nil && len(untracked) > 0 {
			// Git rechecks that files are untracked and not ignored. No recursive
			// rm, force-twice, ignored-file deletion, or fallback after a failure.
			err = command("", append([]string{"clean", "-f", "--"}, untracked...)...)
		}
	case "commit":
		if spec.Identity != nil {
			// Both writes and the commit share the durable repository reservation. Never
			// change global Git configuration or derive attribution from an app account.
			if err = command("", "config", "--local", "--replace-all", "user.name", spec.Identity.Name); err == nil {
				err = command("", "config", "--local", "--replace-all", "user.email", spec.Identity.Email)
			}
			if err != nil {
				break
			}
		}
		for _, ident := range []string{"GIT_AUTHOR_IDENT", "GIT_COMMITTER_IDENT"} {
			probe := exec.CommandContext(ctx, "git", "-C", spec.Path, "var", ident)
			probe.Env = gitEnvironment(spec.Identity)
			data, check := probe.CombinedOutput()
			if check != nil {
				if ctx.Err() != nil {
					return gitaccess.Result{}, ctx.Err()
				}
				if missingIdentity(string(data)) {
					return gitaccess.Result{}, ErrIdentityRequired
				}
				return gitaccess.Result{}, fmt.Errorf("could not read Git author: %s", gitaccess.Redact(strings.TrimSpace(string(data)), gitaccess.Auth{}))
			}
		}
		err = command(spec.Message, "commit", "--file=-")
	case "switch_branch":
		if spec.Remote {
			err = command("", "switch", "--track", "--", "refs/remotes/"+spec.Branch)
		} else {
			err = command("", "switch", "--no-guess", "--", spec.Branch)
		}
	case "create_branch":
		// -c creates the ref only if switching succeeds; never reset an existing branch.
		err = command("", "switch", "-c", spec.Branch)
	}
	text := gitaccess.Redact(output.String(), gitaccess.Auth{})
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return gitaccess.Result{}, fmt.Errorf("%s", text)
	}
	return gitaccess.Result{Output: text}, nil
}

func validIdentity(identity registry.GitIdentity) bool {
	if identity.Name == "" || len(identity.Name) > 256 || len(identity.Email) > 320 ||
		strings.Count(identity.Email, "@") != 1 || strings.HasPrefix(identity.Email, "@") || strings.HasSuffix(identity.Email, "@") {
		return false
	}
	for _, value := range []string{identity.Name, identity.Email} {
		for _, c := range value {
			if c < 32 || c == 127 || c == '<' || c == '>' {
				return false
			}
		}
	}
	return strings.IndexFunc(identity.Email, unicode.IsSpace) == -1
}

func gitEnvironment(identity *registry.GitIdentity) []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if identity != nil {
		env = append(env, "GIT_AUTHOR_NAME="+identity.Name, "GIT_COMMITTER_NAME="+identity.Name,
			"GIT_AUTHOR_EMAIL="+identity.Email, "GIT_COMMITTER_EMAIL="+identity.Email)
	}
	return env
}

// Only attribution failures become a setup request; hooks/signing/config errors remain failures.
func missingIdentity(output string) bool {
	return strings.Contains(output, "Author identity unknown") || strings.Contains(output, "Committer identity unknown") ||
		strings.Contains(output, "Please tell me who you are") || strings.Contains(output, "unable to auto-detect email address") ||
		strings.Contains(output, "empty ident name") || strings.Contains(output, "no email was given") ||
		strings.Contains(output, "no name was given") || strings.Contains(output, "empty ident email")
}

// Keep a bounded diagnostic tail even when a hook or remote is unusually noisy.
type tailOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 32768
	n := len(p)
	if len(p) > limit {
		p = p[len(p)-limit:]
	}
	if extra := len(b.data) + len(p) - limit; extra > 0 {
		b.data = b.data[extra:]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *tailOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
