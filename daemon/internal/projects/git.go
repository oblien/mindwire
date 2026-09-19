package projects

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

var scpURL = regexp.MustCompile(`^(?:[a-zA-Z0-9._-]+@)?[a-zA-Z0-9._-]+:[^\s]+$`)
var gitProgress = regexp.MustCompile(`(Receiving objects|Resolving deltas|Compressing objects|Counting objects|Updating files):\s*(\d+)%`)
var urlSecret = regexp.MustCompile(`(https?://)[^\s/@]+:[^\s/@]+@`)

func validateRepo(value string) error {
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") || strings.HasPrefix(value, "-") {
		return invalid("invalid repository URL")
	}
	if filepath.IsAbs(value) {
		return nil
	}
	if !strings.Contains(value, "://") && strings.Contains(value, "::") {
		return invalid("custom Git remote helpers are not project repository URLs")
	}
	if !strings.Contains(value, "://") && scpURL.MatchString(value) {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return invalid("invalid repository URL")
	}
	switch u.Scheme {
	case "https", "http", "ssh", "file":
	default:
		return invalid("repository must use HTTPS, SSH, or a local file path")
	}
	if u.Scheme != "file" && u.Hostname() == "" {
		return invalid("repository host is required")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return invalid("repository URL cannot contain a query or fragment")
	}
	if u.User != nil {
		_, password := u.User.Password()
		if u.Scheme != "ssh" || password {
			return invalid("send repository credentials in auth, not in its URL")
		}
	}
	return nil
}

func validateAuth(repo string, auth Auth) error {
	if len(auth.Token) > 8192 || len(auth.PrivateKey) > 32768 || strings.ContainsAny(auth.Token+auth.Username, "\x00\r\n") || strings.ContainsRune(auth.PrivateKey, 0) {
		return invalid("invalid repository credential")
	}
	switch auth.Kind {
	case "":
		if auth.Token != "" || auth.PrivateKey != "" || auth.Username != "" {
			return invalid("authentication kind is required")
		}
	case "token":
		if auth.Token == "" || auth.PrivateKey != "" {
			return invalid("a repository access token is required")
		}
		if !strings.HasPrefix(repo, "https://") && !strings.HasPrefix(repo, "http://") {
			return invalid("token authentication requires an HTTP repository")
		}
	case "ssh":
		if !strings.Contains(auth.PrivateKey, "PRIVATE KEY-----") || auth.Token != "" {
			return invalid("an SSH private key is required")
		}
		if !strings.HasPrefix(repo, "ssh://") && !scpURL.MatchString(repo) {
			return invalid("SSH authentication requires an SSH repository")
		}
	case "gh":
		if auth.Token != "" || auth.PrivateKey != "" {
			return invalid("gh authentication uses the workspace's own login")
		}
		if !strings.HasPrefix(repo, "https://") {
			return invalid("gh authentication requires an HTTPS repository")
		}
	default:
		return invalid("unknown repository authentication method")
	}
	return nil
}

func gitClone(ctx context.Context, o registry.ProjectOperation, auth Auth, destination string, output func(string)) error {
	return cloneWithEnvironment(ctx, o, auth, destination, output, nil)
}

func (s *Service) authenticatedClone(ctx context.Context, o registry.ProjectOperation, auth Auth, destination string, output func(string)) error {
	if o.GitConnection == nil || o.GitConnection.Mode == "native" {
		return gitClone(ctx, o, auth, destination, output)
	}
	// Resolve Git's URL rewriting before handing a connection to the helper.
	resolved, err := exec.CommandContext(ctx, "git", "ls-remote", "--get-url", "--", o.RepoURL).Output()
	if err != nil {
		return fmt.Errorf("could not resolve the repository URL")
	}
	url := strings.TrimSpace(string(resolved))
	wanted, err := gitaccess.Repository(o.RepoURL)
	actual, actualErr := gitaccess.Repository(url)
	if err != nil || actualErr != nil || wanted != actual {
		return invalid("Git URL rewriting changes the selected GitHub repository")
	}
	lease, err := s.gitAccess.Prepare(ctx, url, o.GitConnection, &auth)
	if err != nil {
		return err
	}
	defer lease.Close()
	return cloneWithEnvironment(ctx, o, auth, destination, output, lease.Environment)
}

func cloneWithEnvironment(ctx context.Context, o registry.ProjectOperation, auth Auth, destination string, output func(string), forwarded map[string]string) error {
	args := []string{}
	env := os.Environ()
	env = setEnv(env, "GIT_TERMINAL_PROMPT", "0")
	env = setEnv(env, "LC_ALL", "C")
	// Git helpers and local configuration remain native when no auth was supplied.
	if forwarded != nil {
		for key, value := range forwarded {
			env = setEnv(env, key, value)
		}
	} else if auth.Kind == "token" {
		username := auth.Username
		if username == "" {
			username = "x-access-token"
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + auth.Token))
		// Scope the header to this repository URL; origin always stays credential-free.
		env = setEnv(env, "GIT_CONFIG_COUNT", "2")
		env = setEnv(env, "GIT_CONFIG_KEY_0", "http."+o.RepoURL+".extraheader")
		env = setEnv(env, "GIT_CONFIG_VALUE_0", "AUTHORIZATION: basic "+encoded)
		env = setEnv(env, "GIT_CONFIG_KEY_1", "credential.helper")
		env = setEnv(env, "GIT_CONFIG_VALUE_1", "")
	} else if auth.Kind == "ssh" {
		file, err := os.CreateTemp(filepath.Dir(destination), "credential-")
		if err != nil {
			return err
		}
		keyPath := file.Name()
		defer os.Remove(keyPath)
		_, err = file.WriteString(auth.PrivateKey)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		env = setEnv(env, "GIT_SSH_COMMAND", "ssh -i "+shellQuote(keyPath)+" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o BatchMode=yes")
	} else if auth.Kind == "gh" {
		args = append(args, "-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential")
	}
	args = append(args, "clone", "--progress")
	if o.Branch != "" {
		args = append(args, "--branch", o.Branch)
	}
	args = append(args, "--", o.RepoURL, destination)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	proc.Group(cmd)
	bindParent(cmd)
	reader, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	cmd.Stderr = cmd.Stdout
	if err = cmd.Start(); err != nil {
		return err
	}
	// A line scanner handles git's carriage-return progress without assuming chunks
	// align with records. Redaction runs before any line reaches persistent state.
	scanner := bufio.NewScanner(reader)
	scanner.Split(gitLines)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var last string
	for scanner.Scan() {
		line := redact(scanner.Text(), auth)
		if strings.TrimSpace(line) != "" {
			last = line
			output(line)
		}
	}
	scanErr := scanner.Err()
	if scanErr != nil && cmd.Cancel != nil {
		_ = cmd.Cancel()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanErr != nil {
		return scanErr
	}
	if waitErr != nil {
		if last != "" {
			return fmt.Errorf("clone failed: %s", last)
		}
		return fmt.Errorf("clone failed: %w", waitErr)
	}
	return nil
}

func verifyRepository(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--is-inside-work-tree")
	proc.Group(cmd)
	data, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(data)) != "true" {
		return fmt.Errorf("clone did not produce a Git working tree")
	}
	// An empty repository is valid; no working-file heuristic or runtime exit-code guess.
	return nil
}

func gitLines(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\r' || b == '\n' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
func parseProgress(line string) (string, *float64) {
	m := gitProgress.FindStringSubmatch(line)
	if len(m) != 3 {
		return "", nil
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", nil
	}
	f := float64(min(max(n, 0), 100)) / 100
	phase := map[string]string{"Receiving objects": "receiving", "Resolving deltas": "resolving", "Compressing objects": "compressing", "Counting objects": "counting", "Updating files": "checkout"}[m[1]]
	return phase, &f
}
func setEnv(env []string, key, value string) []string {
	out := env[:0]
	for _, v := range env {
		if !strings.HasPrefix(v, key+"=") {
			out = append(out, v)
		}
	}
	return append(out, key+"="+value)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func redact(s string, auth Auth) string {
	if index := strings.Index(strings.ToLower(s), "authorization:"); index >= 0 {
		s = s[:index] + "authorization: [redacted]"
	}
	if auth.Token != "" {
		s = strings.ReplaceAll(s, auth.Token, "[redacted]")
		s = strings.ReplaceAll(s, url.QueryEscape(auth.Token), "[redacted]")
		user := auth.Username
		if user == "" {
			user = "x-access-token"
		}
		s = strings.ReplaceAll(s, base64.StdEncoding.EncodeToString([]byte(user+":"+auth.Token)), "[redacted]")
	}
	for _, line := range strings.Split(auth.PrivateKey, "\n") {
		if len(strings.TrimSpace(line)) > 16 {
			s = strings.ReplaceAll(s, strings.TrimSpace(line), "[redacted]")
		}
	}
	return urlSecret.ReplaceAllString(s, "${1}[redacted]@")
}
