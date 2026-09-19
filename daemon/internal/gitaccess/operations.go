package gitaccess

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/proc"
)

// RemoteURL asks Git to resolve insteadOf/pushInsteadOf rewrites before a
// credential is granted. Multiple push destinations require explicit setup.
func RemoteURL(ctx context.Context, directory string, push bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := []string{"-C", directory, "remote", "get-url"}
	if push {
		args = append(args, "--push")
	}
	args = append(args, "--all", "origin")
	data, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", nil // folders without Git/origin can still host agent work
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		return "", fmt.Errorf("Git operations require one origin destination")
	}
	return lines[0], nil
}

type Result struct {
	Output string `json:"output"`
}

// PublicRemoteURL is metadata only. Native repositories may already contain
// credentials in their remote; never return those through a state response.
func PublicRemoteURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if u.Scheme != "ssh" {
		u.User = nil
	} else if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

func (s *Service) Run(ctx context.Context, directory, action string, c *Connection, supplied *Auth) (Result, error) {
	if action != "fetch" && action != "pull" && action != "push" {
		return Result{}, fmt.Errorf("unsupported Git operation")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	repo, err := RemoteURL(ctx, directory, action == "push")
	if err != nil {
		return Result{}, err
	}
	if repo == "" {
		return Result{}, fmt.Errorf("set an origin remote before using GitHub")
	}
	if c != nil && c.Mode != "native" {
		fetchURL, err := RemoteURL(ctx, directory, false)
		if err != nil {
			return Result{}, err
		}
		fetchRepo, err := Repository(fetchURL)
		if err != nil {
			return Result{}, err
		}
		pushRepo, err := Repository(repo)
		if err != nil {
			return Result{}, err
		}
		if pushRepo != fetchRepo {
			return Result{}, fmt.Errorf("the push destination differs from the selected GitHub repository")
		}
	}
	lease, err := s.Prepare(ctx, repo, c, supplied)
	if err != nil {
		return Result{}, err
	}
	defer lease.Close()
	if action == "push" && lease.Auth.ReadOnly {
		return Result{}, fmt.Errorf("this GitHub connection is read-only; choose a connection with write access")
	}
	args := []string{"-C", directory, action}
	switch action {
	case "fetch":
		args = append(args, "origin")
	case "pull":
		args = append(args, "--ff-only", "--no-rebase")
	case "push":
		branch, err := exec.CommandContext(ctx, "git", "-C", directory, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
		if err != nil || strings.TrimSpace(string(branch)) == "" {
			return Result{}, fmt.Errorf("check out a branch before pushing")
		}
		args = append(args, "--set-upstream", "origin", "HEAD")
	}
	// Pull's upstream is explicit so another branch's configured remote cannot
	// receive this repository's credential or merge unexpected history.
	if action == "pull" {
		branch, err := exec.CommandContext(ctx, "git", "-C", directory, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
		if err != nil {
			return Result{}, fmt.Errorf("check out a branch before pulling")
		}
		args = append(args, "origin", strings.TrimSpace(string(branch)))
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	proc.Group(cmd)
	cmd.Env = os.Environ()
	values := map[string]string{"GIT_TERMINAL_PROMPT": "0", "LC_ALL": "C"}
	for k, v := range lease.Environment {
		values[k] = v
	}
	for key, value := range values {
		prefix := key + "="
		filtered := cmd.Env[:0]
		for _, item := range cmd.Env {
			if !strings.HasPrefix(item, prefix) {
				filtered = append(filtered, item)
			}
		}
		cmd.Env = append(filtered, prefix+value)
	}
	output := &boundedOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	err = cmd.Run()
	text := Redact(output.String(), lease.Auth)
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("Git %s was interrupted; refresh the repository before retrying", action)
	}
	if err != nil {
		if text == "" {
			text = "Git " + action + " failed"
		}
		return Result{}, fmt.Errorf("%s", text)
	}
	return Result{Output: text}, nil
}

var outputURLSecret = regexp.MustCompile(`(?i)(https?://)[^\s/@]+@`)
var outputAuthHeader = regexp.MustCompile(`(?im)(authorization:\s*)[^\r\n]+`)

func Redact(text string, auth Auth) string {
	if auth.Token != "" {
		text = strings.ReplaceAll(text, auth.Token, "[redacted]")
		text = strings.ReplaceAll(text, url.QueryEscape(auth.Token), "[redacted]")
		username := auth.Username
		if username == "" {
			username = "x-access-token"
		}
		text = strings.ReplaceAll(text, base64.StdEncoding.EncodeToString([]byte(username+":"+auth.Token)), "[redacted]")
	}
	for _, line := range strings.Split(auth.PrivateKey, "\n") {
		if len(line) > 16 {
			text = strings.ReplaceAll(text, line, "[redacted]")
		}
	}
	text = outputURLSecret.ReplaceAllString(text, "${1}[redacted]@")
	text = outputAuthHeader.ReplaceAllString(text, "${1}[redacted]")
	return strings.TrimSpace(text)
}

type boundedOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, data...)
	if len(b.data) > 16384 {
		b.data = b.data[len(b.data)-16384:]
	}
	return len(data), nil
}
func (b *boundedOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }
