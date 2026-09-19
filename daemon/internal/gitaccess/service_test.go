package gitaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("MINDWIRE_SSH_AGENT_PROBE") == "1" {
		os.Exit(sshAgentProbe(os.Args[1:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "--git-credential" {
		if err := Helper(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func service(t *testing.T) *Service {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func tokenConnection(lifetime string) Connection {
	return Connection{ID: "personal", Login: "octocat", Mode: "token", Lifetime: lifetime}
}

func credential(t *testing.T, directory string, environment map[string]string, host, repository string) (string, error) {
	t.Helper()
	args := []string{}
	if directory != "" {
		args = append(args, "-C", directory)
	}
	args = append(args, "credential", "fill")
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	for key, value := range environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdin = strings.NewReader("protocol=https\nhost=" + host + "\npath=" + repository + "\n\n")
	data, err := cmd.CombinedOutput()
	return string(data), err
}

func TestTemporaryCredentialUsesRealGitHelperAndEndsWithOperation(t *testing.T) {
	s := service(t)
	connection := tokenConnection("run")
	auth := Auth{Kind: "token", Token: "test-personal-secret"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lease, err := s.Prepare(ctx, "https://github.com/octocat/app.git", &connection, &auth)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	grant := lease.Environment[leaseEnvironmentKey]
	if grant == "" {
		t.Fatal("missing private helper grant")
	}
	for key, value := range lease.Environment {
		if strings.Contains(value, auth.Token) {
			t.Fatal("token in process environment")
		}
		if key != leaseEnvironmentKey && strings.Contains(value, grant) {
			t.Fatal("bearer grant embedded in a helper command")
		}
	}
	output, err := credential(t, "", lease.Environment, "github.com", "octocat/app.git")
	if err != nil || !strings.Contains(output, "password="+auth.Token) {
		t.Fatalf("Git did not receive the selected credential: %v", err)
	}
	for _, request := range [][2]string{{"github.com", "octocat/other.git"}, {"github.com.attacker.invalid", "octocat/app.git"}, {"github.com", "octocat/app.git/other"}} {
		output, err := credential(t, "", lease.Environment, request[0], request[1])
		if err == nil || strings.Contains(output, auth.Token) {
			t.Fatal("credential escaped its repository")
		}
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("temporary credential was persisted")
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		count := len(s.leases)
		s.mu.Unlock()
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled operation retained a lease")
		}
		time.Sleep(time.Millisecond)
	}
	output, err = credential(t, "", lease.Environment, "github.com", "octocat/app.git")
	if err == nil || strings.Contains(output, auth.Token) {
		t.Fatal("ended operation still authenticates")
	}
}

func TestSimultaneousAccountsDoNotReplaceEachOthersCredentials(t *testing.T) {
	s := service(t)
	one, two := tokenConnection("run"), tokenConnection("run")
	two.ID, two.Login = "work", "work-user"
	a, err := s.Prepare(context.Background(), "https://github.com/octocat/app.git", &one, &Auth{Kind: "token", Token: "one-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := s.Prepare(context.Background(), "https://github.com/octocat/app.git", &two, &Auth{Kind: "token", Token: "two-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, test := range []struct {
		lease       *Lease
		want, other string
	}{{a, "one-secret", "two-secret"}, {b, "two-secret", "one-secret"}} {
		out, err := credential(t, "", test.lease.Environment, "github.com", "octocat/app.git")
		if err != nil || !strings.Contains(out, test.want) || strings.Contains(out, test.other) {
			t.Fatal("account isolation failed")
		}
	}
	a.Close()
	if _, err := credential(t, "", b.Environment, "github.com", "octocat/app.git"); err != nil {
		t.Fatal("ending one run broke another")
	}
}

func TestSavedAccessSurvivesRestartAndSupportsTerminalGitWithoutEmbeddingSecrets(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	connection := tokenConnection("workspace")
	auth := Auth{Kind: "token", Token: "saved-token-fixture"}
	if err = s.SetDefault(&connection, &auth); err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(dir, "project")
	git(t, "init", "--initial-branch=main", working)
	git(t, "-C", working, "remote", "add", "origin", "https://github.com/octocat/app.git")
	git(t, "-C", working, "config", "user.name", "Existing User")
	if err = s.ConfigureRepository(context.Background(), working, "https://github.com/octocat/app.git", &connection); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfigureRepository(context.Background(), working, "https://github.com/octocat/app.git", &connection); err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(filepath.Join(working, ".git", "config"))
	if strings.Contains(string(config), auth.Token) || strings.Count(string(config), "path =") != 1 {
		t.Fatal("Git config contains a secret or duplicate helper include")
	}
	if git(t, "-C", working, "config", "user.name") != "Existing User" {
		t.Fatal("existing Git identity changed")
	}
	metadata, _ := json.Marshal(s.State())
	if strings.Contains(string(metadata), auth.Token) || strings.Contains(string(metadata), "auth") {
		t.Fatal("credential in metadata")
	}
	info, _ := os.Stat(s.path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("saved credential permissions are not private")
	}
	s.Close()
	// A terminal helper reads the protected saved credential without a phone or
	// a running daemon. Restarting the daemon preserves the same connection ID.
	output, err := credential(t, working, nil, "github.com", "octocat/app.git")
	if err != nil || !strings.Contains(output, auth.Token) {
		t.Fatal("saved terminal authentication failed")
	}
	s, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resolved, err := s.Resolve(s.State().Default, nil)
	if err != nil || resolved.Token != auth.Token {
		t.Fatal("saved connection did not survive restart")
	}
	if err = s.Forget(connection.ID); err != nil {
		t.Fatal(err)
	}
	output, err = credential(t, working, nil, "github.com", "octocat/app.git")
	if err == nil || strings.Contains(output, auth.Token) {
		t.Fatal("removed credential still works")
	}
	if err = s.ConfigureRepository(context.Background(), working, "https://github.com/octocat/app.git", nil); err != nil {
		t.Fatal(err)
	}
	if git(t, "-C", working, "config", "user.name") != "Existing User" {
		t.Fatal("removing managed access changed native Git settings")
	}
}

func TestExpiredAndOblienCredentialsCannotBecomePersistentWriteAccess(t *testing.T) {
	s := service(t)
	connection := tokenConnection("run")
	expired := Auth{Kind: "token", Token: "expired", ExpiresAt: time.Now().Add(-time.Minute).Format(time.RFC3339)}
	if _, err := s.Resolve(&connection, &expired); err == nil {
		t.Fatal("accepted expired credential")
	}
	connection.Mode = "oblienApp"
	auth, err := s.Resolve(&connection, &Auth{Kind: "token", Token: "read-token"})
	if err != nil || !auth.ReadOnly {
		t.Fatal("Oblien access lost its read-only restriction")
	}
	connection.Lifetime = "workspace"
	if err := s.Save(connection, &auth); err == nil {
		t.Fatal("persisted a non-renewable Oblien clone token")
	}
}

func TestGitHubRepositoryValidation(t *testing.T) {
	for _, value := range []string{"https://github.com/octocat/app.git", "git@github.com:octocat/app.git", "ssh://git@github.com/octocat/app"} {
		if actual, err := Repository(value); err != nil || actual != "octocat/app" {
			t.Errorf("valid repository %q: %v", value, err)
		}
	}
	for _, value := range []string{"https://github.com.evil.invalid/octocat/app", "https://evil.invalid/github.com/octocat/app", "http://github.com/octocat/app", "https://token@github.com/octocat/app", "https://github.com/octocat/app?token=secret", "https://github.com/octocat/%2e%2e", "https://github.com/octocat/app/extra", "https://github.com:444/octocat/app"} {
		if _, err := Repository(value); err == nil {
			t.Errorf("accepted unsafe repository %q", value)
		}
	}
}

func TestMisconfiguredConnectionsDoNotReplaceNativeGitSettings(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	git(t, "init", dir)
	git(t, "-C", dir, "config", "credential.helper", "!f() { echo username=existing; echo password=other-account; }; f")
	repo := "https://github.com/octocat/app.git"
	git(t, "-C", dir, "config", "http."+repo+".extraHeader", "Authorization: Basic previous-account")
	c := tokenConnection("run")
	lease, err := s.Prepare(context.Background(), repo, &c, &Auth{Kind: "token", Token: "selected-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	output, err := credential(t, dir, lease.Environment, "github.com", "octocat/app.git")
	if err != nil || !strings.Contains(output, "selected-fixture") || strings.Contains(output, "other-account") {
		t.Fatal("the selected account did not override the existing helper")
	}
	c.Mode = "sshKey"
	if err := s.ConfigureRepository(context.Background(), dir, repo, &c); err == nil {
		t.Fatal("saved an SSH connection for an HTTPS remote")
	}
	if !strings.Contains(git(t, "-C", dir, "config", "credential.helper"), "other-account") {
		t.Fatal("failed configuration changed the user's native helper")
	}
}

func TestCredentialStorageRejectsPublicFilesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-access.json")
	if err := os.WriteFile(path, []byte(`{"credentials":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if s, err := New(dir); err == nil {
		s.Close()
		t.Fatal("read a publicly accessible credential file")
	}
	if err := os.Rename(path, path+".other"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".other", path); err != nil {
		t.Fatal(err)
	}
	if s, err := New(dir); err == nil {
		s.Close()
		t.Fatal("followed a credential storage symlink")
	}
}

func git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func TestRealRemoteOperationsAndNonFastForwardProtection(t *testing.T) {
	s := service(t)
	dir := t.TempDir()
	remote, work, other := filepath.Join(dir, "remote.git"), filepath.Join(dir, "work"), filepath.Join(dir, "other")
	git(t, "init", "--bare", "--initial-branch=main", remote)
	git(t, "clone", remote, work)
	commit := func(path, name string) {
		git(t, "-C", path, "config", "user.name", "Git Fixture")
		git(t, "-C", path, "config", "user.email", "fixture@example.invalid")
		if err := os.WriteFile(filepath.Join(path, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		git(t, "-C", path, "add", name)
		git(t, "-C", path, "commit", "-m", name)
	}
	commit(work, "initial")
	if _, err := s.Run(context.Background(), work, "push", nil, nil); err != nil {
		t.Fatal(err)
	}
	git(t, "clone", remote, other)
	commit(other, "upstream")
	git(t, "-C", other, "push", "origin", "main")
	if _, err := s.Run(context.Background(), work, "fetch", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background(), work, "pull", nil, nil); err != nil {
		t.Fatal(err)
	}
	if git(t, "-C", work, "rev-parse", "HEAD") != git(t, "-C", other, "rev-parse", "HEAD") {
		t.Fatal("pull did not fast-forward")
	}
	commit(work, "local")
	commit(other, "divergent")
	git(t, "-C", other, "push", "origin", "main")
	before := git(t, "-C", work, "rev-parse", "HEAD")
	if _, err := s.Run(context.Background(), work, "pull", nil, nil); err == nil {
		t.Fatal("pull merged divergent history")
	}
	if git(t, "-C", work, "rev-parse", "HEAD") != before {
		t.Fatal("failed pull changed HEAD")
	}
	if _, err := s.Run(context.Background(), work, "push", nil, nil); err == nil {
		t.Fatal("push forced divergent history")
	}
}
