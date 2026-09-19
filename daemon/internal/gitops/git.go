package gitops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

func Network(action string) bool { return action == "fetch" || action == "pull" || action == "push" }

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
	default:
		return invalid("unsupported Git operation")
	}
	return nil
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
	output := &tailOutput{}
	command := func(input string, args ...string) error {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs", "-C", spec.Path}, args...)...)
		proc.Group(cmd)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
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
		err = command(spec.Message, "commit", "--file=-")
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
