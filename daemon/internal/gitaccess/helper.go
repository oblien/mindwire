package gitaccess

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

const helperTimeout = 10 * time.Second
const leaseEnvironmentKey = "MINDWIRE_GIT_LEASE"

type credentialLease struct {
	connection Connection
	repository string
	auth       Auth
}

type Lease struct {
	Environment map[string]string
	Auth        Auth // for redacting operation output; never serialize a Lease
	Close       func()
}

type helperRequest struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Path     string `json:"path"`
}

func (r helperRequest) matches(repository string, ssh bool) bool {
	protocol := "https"
	if ssh {
		protocol = "ssh"
	}
	return r.Protocol == protocol && strings.EqualFold(r.Host, "github.com") &&
		strings.EqualFold(strings.TrimSuffix(r.Path, ".git"), repository)
}

func (s *Service) Prepare(ctx context.Context, repo string, c *Connection, supplied *Auth) (*Lease, error) {
	auth, err := s.Resolve(c, supplied)
	if err != nil {
		return nil, err
	}
	lease := &Lease{Environment: map[string]string{}, Auth: auth, Close: func() {}}
	if c == nil || c.Mode == "native" {
		return lease, nil
	}
	repository, err := c.CheckRepository(repo)
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("Git credential service is closed")
	}
	s.leases[id] = credentialLease{connection: *c, repository: repository, auth: auth}
	s.mu.Unlock()
	var once sync.Once
	cleanup := func() { once.Do(func() { s.mu.Lock(); delete(s.leases, id); s.mu.Unlock() }) }
	stop := context.AfterFunc(ctx, cleanup)
	lease.Close = func() { stop(); cleanup() }
	command := quote(executable) + " --git-credential lease " + quote(s.endpoint) + " " + quote(repository)
	if auth.Kind == "ssh" {
		lease.Environment["GIT_SSH_COMMAND"] = quote(executable) + " --git-ssh lease " + quote(s.endpoint) + " " + quote(repository)
		lease.Environment["GIT_SSH_VARIANT"] = "ssh"
	} else {
		lease.Environment = credentialEnvironment(command, repo)
	}
	lease.Environment[leaseEnvironmentKey] = id
	lease.Environment["GIT_TERMINAL_PROMPT"] = "0"
	return lease, nil
}

func credentialEnvironment(command, repo string) map[string]string {
	return map[string]string{
		"GIT_CONFIG_COUNT": "4",
		"GIT_CONFIG_KEY_0": "credential.https://github.com.helper", "GIT_CONFIG_VALUE_0": "",
		"GIT_CONFIG_KEY_1": "credential.https://github.com.helper", "GIT_CONFIG_VALUE_1": "!" + command,
		"GIT_CONFIG_KEY_2": "credential.https://github.com.useHttpPath", "GIT_CONFIG_VALUE_2": "true",
		"GIT_CONFIG_KEY_3": "http." + repo + ".extraHeader", "GIT_CONFIG_VALUE_3": "",
	}
}

func (s *Service) credential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	lease, ok := s.leases[id]
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req helperRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil ||
		!req.matches(lease.repository, lease.auth.Kind == "ssh") || lease.auth.Validate(lease.connection) != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(lease.auth)
}

// Helper runs in a short-lived mindwired subprocess spawned by Git. The original
// PAT never appears in its arguments or environment. Git's store/erase callbacks
// cannot persist, replace, or remove the app-owned credential.
func Helper(args []string, input io.Reader, output io.Writer) error {
	source, rest, err := helperSource(args)
	if err != nil || len(rest) != 1 {
		return fmt.Errorf("invalid Git credential helper invocation")
	}
	if rest[0] != "get" {
		return nil
	}
	values := map[string]string{}
	scanner := bufio.NewScanner(io.LimitReader(input, 8192))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	req := helperRequest{Protocol: values["protocol"], Host: values["host"], Path: values["path"]}
	if !req.matches(source[len(source)-1], false) {
		return nil
	}
	auth, err := helperAuth(source, req)
	if err != nil {
		return err
	}
	if auth.Kind == "gh" {
		ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "gh", "auth", "git-credential", "get")
		cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\npath=" + req.Path + "\n\n")
		cmd.Stdout = output
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("the workspace's GitHub CLI is not signed in")
		}
		return nil
	}
	if auth.Kind != "token" {
		return fmt.Errorf("this connection cannot authenticate HTTPS Git")
	}
	username := auth.Username
	if username == "" {
		username = "x-access-token"
	}
	_, err = fmt.Fprintf(output, "username=%s\npassword=%s\n\n", username, auth.Token)
	return err
}

func helperSource(args []string) ([]string, []string, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("missing credential source")
	}
	count := 3
	if args[0] == "stored" {
		count = 4
	} else if args[0] != "lease" {
		return nil, nil, fmt.Errorf("invalid credential source")
	}
	if len(args) < count {
		return nil, nil, fmt.Errorf("invalid credential source")
	}
	return args[:count], args[count:], nil
}

func helperAuth(args []string, req helperRequest) (Auth, error) {
	if args[0] == "stored" {
		state, err := readState(args[1])
		if err != nil {
			return Auth{}, fmt.Errorf("could not read saved GitHub access")
		}
		value, ok := state.Credentials[args[2]]
		if !ok {
			return Auth{}, fmt.Errorf("GitHub access was removed from this workspace")
		}
		if err := value.Auth.Validate(value.Connection); err != nil {
			return Auth{}, err
		}
		return value.Auth, nil
	}
	id := os.Getenv(leaseEnvironmentKey)
	if id == "" {
		return Auth{}, fmt.Errorf("invalid credential source")
	}
	client, endpoint, err := brokerClient(args[1])
	if err != nil {
		return Auth{}, err
	}
	defer client.CloseIdleConnections()
	body, _ := json.Marshal(req)
	request, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return Auth{}, fmt.Errorf("invalid credential source")
	}
	request.Header.Set("Authorization", "Bearer "+id)
	response, err := client.Do(request)
	if err != nil {
		return Auth{}, fmt.Errorf("the GitHub operation has ended; start it again from the app")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Auth{}, fmt.Errorf("GitHub access expired or was removed; reconnect the account")
	}
	var auth Auth
	if err = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&auth); err != nil {
		return Auth{}, fmt.Errorf("invalid credential response")
	}
	return auth, nil
}

// SSHHelper holds the private key in an ephemeral SSH agent. Only its public
// identity is written to disk, so even SIGKILL cannot leave a private key behind.
func SSHHelper(args []string, input io.Reader, output, diagnostic io.Writer) error {
	source, sshArgs, err := helperSource(args)
	if err != nil || len(sshArgs) < 2 {
		return fmt.Errorf("invalid Git SSH helper invocation")
	}
	host, command := sshArgs[len(sshArgs)-2], sshArgs[len(sshArgs)-1]
	if host != "git@github.com" && host != "github.com" {
		return fmt.Errorf("the SSH connection is restricted to GitHub")
	}
	op, path, ok := strings.Cut(command, " ")
	if !ok || (op != "git-upload-pack" && op != "git-receive-pack" && op != "git-upload-archive") {
		return fmt.Errorf("unsupported Git SSH command")
	}
	path = strings.Trim(path, "'")
	req := helperRequest{Protocol: "ssh", Host: "github.com", Path: path}
	if !req.matches(source[len(source)-1], true) {
		return fmt.Errorf("the SSH connection is restricted to the selected repository")
	}
	auth, err := helperAuth(source, req)
	if err != nil {
		return err
	}
	if auth.Kind != "ssh" {
		return fmt.Errorf("this connection does not contain an SSH key")
	}
	privateKey, err := ssh.ParseRawPrivateKey([]byte(auth.PrivateKey))
	if err != nil {
		return fmt.Errorf("could not read the SSH private key; use an unencrypted supported key")
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return fmt.Errorf("unsupported SSH private key")
	}
	keyring := sshagent.NewKeyring()
	if err = keyring.Add(sshagent.AddedKey{PrivateKey: privateKey}); err != nil {
		return fmt.Errorf("could not prepare SSH authentication")
	}
	defer keyring.RemoveAll()
	directory, err := os.MkdirTemp("", "mindwire-git-ssh-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	key := filepath.Join(directory, "identity.pub")
	if err = os.WriteFile(key, ssh.MarshalAuthorizedKey(signer.PublicKey()), 0600); err != nil {
		return err
	}
	socket := filepath.Join(directory, "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(socket, 0600); err != nil {
		return err
	}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { defer connection.Close(); _ = sshagent.ServeAgent(keyring, connection) }()
		}
	}()
	cmd := exec.Command("ssh", append([]string{"-i", key, "-o", "IdentityAgent=" + socket, "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=accept-new", "-o", "BatchMode=yes"}, sshArgs...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, output, diagnostic
	return cmd.Run()
}

// ConfigureRepository installs only an app-owned include. It never changes a
// user's global Git configuration or embeds a credential in origin/config.
func (s *Service) ConfigureRepository(ctx context.Context, directory, repo string, c *Connection) error {
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()
	var repository string
	if c != nil && c.Mode != "native" {
		var err error
		repository, err = c.CheckRepository(repo)
		if err != nil {
			return err
		}
	}
	digest := sha256.Sum256([]byte(directory))
	configPath := filepath.Join(filepath.Dir(s.path), "git", hex.EncodeToString(digest[:])+".config")
	get := exec.CommandContext(ctx, "git", "-C", directory, "config", "--local", "--get-all", "include.path")
	data, err := get.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return fmt.Errorf("could not read this repository's Git configuration")
		}
	}
	present := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		present = present || line == configPath
	}
	if c == nil || c.Mode == "native" || c.Lifetime != "workspace" {
		if present {
			if err := exec.CommandContext(ctx, "git", "-C", directory, "config", "--local", "--fixed-value", "--unset-all", "include.path", configPath).Run(); err != nil {
				return err
			}
		}
		_ = os.Remove(configPath)
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	var content string
	if c.Mode == "sshKey" {
		command := quote(executable) + " --git-ssh stored " + quote(s.path) + " " + quote(c.ID) + " " + quote(repository)
		content = "[core]\n\tsshCommand = " + strconv.Quote(command) + "\n[ssh]\n\tvariant = ssh\n"
	} else {
		command := quote(executable) + " --git-credential stored " + quote(s.path) + " " + quote(c.ID) + " " + quote(repository)
		if c.Mode == "ghCli" {
			command = "gh auth git-credential"
		}
		content = "[credential \"https://github.com\"]\n\tuseHttpPath = true\n\thelper =\n\thelper = " + strconv.Quote("!"+command) + "\n[http " + strconv.Quote(repo) + "]\n\textraHeader =\n"
	}
	if err = os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(configPath), ".config-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.WriteString(content); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), configPath); err != nil {
		return err
	}
	if !present {
		return exec.CommandContext(ctx, "git", "-C", directory, "config", "--local", "--add", "include.path", configPath).Run()
	}
	return nil
}

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
