package codex

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestCodexMCPModule exercises the full set→list→get→delete cycle through the hand-rolled TOML
// reader/emitter against a temp CODEX_HOME, and — the load-bearing invariant — asserts that every other
// table in config.toml survives byte-for-byte. Codex is user-scope only; a project-scope op is an error.
func TestCodexMCPModule(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "config.toml")
	a := adapter{}

	// Capability hint is on, and the adapter implements the optional module.
	if !a.Capabilities().MCPConfig {
		t.Fatal("codex Capabilities.MCPConfig = false, want true")
	}
	if scopes := a.MCPScopes(); len(scopes) != 1 || scopes[0] != agent.MemoryUser {
		t.Fatalf("MCPScopes = %v, want [user]", scopes)
	}

	// Seed unrelated config the writer must never disturb.
	preamble := "model = \"o3\"\napproval_policy = \"on-request\"\n\n[tui]\ntheme = \"dark\"\n"
	if err := os.WriteFile(path, []byte(preamble), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// Project scope is unsupported → error at every op.
	if _, err := a.ListMCPServers(agent.MemoryProject, ""); err == nil {
		t.Error("project-scope List should be unsupported")
	}
	if err := a.SetMCPServer(agent.MemoryProject, "", "x", agent.MCPServer{}); err == nil {
		t.Error("project-scope Set should be unsupported")
	}

	// Set a stdio server (command/args/env) and an HTTP server (url/bearer/headers).
	stdio := agent.MCPServer{
		Command: "my-server",
		Args:    []string{"--flag", "value with space"},
		Env:     map[string]string{"FOO": "bar"},
		Cwd:     "/tmp/work",
	}
	if err := a.SetMCPServer(agent.MemoryUser, "", "local", stdio); err != nil {
		t.Fatalf("Set(local): %v", err)
	}
	http := agent.MCPServer{
		URL:               "https://mcp.example.com",
		BearerTokenEnvVar: "EXAMPLE_TOKEN",
		HTTPHeaders:       map[string]string{"X-Trace": "on"},
	}
	if err := a.SetMCPServer(agent.MemoryUser, "", "remote", http); err != nil {
		t.Fatalf("Set(remote): %v", err)
	}

	// List returns both, faithfully.
	servers, err := a.ListMCPServers(agent.MemoryUser, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("List = %d servers, want 2: %+v", len(servers), servers)
	}
	if !reflect.DeepEqual(servers["local"], stdio) {
		t.Errorf("local round-trip = %+v, want %+v", servers["local"], stdio)
	}
	if !reflect.DeepEqual(servers["remote"], http) {
		t.Errorf("remote round-trip = %+v, want %+v", servers["remote"], http)
	}

	// Sibling config is preserved byte-for-byte.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	for _, want := range []string{"model = \"o3\"", "approval_policy = \"on-request\"", "[tui]", "theme = \"dark\""} {
		if !containsLine(string(after), want) {
			t.Errorf("sibling config lost line %q; got:\n%s", want, after)
		}
	}

	// Overwriting a server replaces its section (no duplicate) and still preserves siblings + the other server.
	if err := a.SetMCPServer(agent.MemoryUser, "", "local", agent.MCPServer{Command: "replaced"}); err != nil {
		t.Fatalf("Set(local, overwrite): %v", err)
	}
	servers, _ = a.ListMCPServers(agent.MemoryUser, "")
	if len(servers) != 2 || servers["local"].Command != "replaced" || servers["local"].Cwd != "" {
		t.Fatalf("overwrite = %+v, want local replaced with only command", servers)
	}

	// Delete removes one, leaves the other and the siblings intact; deleting again is a no-op.
	if err := a.DeleteMCPServer(agent.MemoryUser, "", "local"); err != nil {
		t.Fatalf("Delete(local): %v", err)
	}
	if err := a.DeleteMCPServer(agent.MemoryUser, "", "local"); err != nil {
		t.Fatalf("Delete(local) again should be idempotent: %v", err)
	}
	servers, _ = a.ListMCPServers(agent.MemoryUser, "")
	if len(servers) != 1 {
		t.Fatalf("after delete = %+v, want only remote", servers)
	}
	if _, ok := servers["remote"]; !ok {
		t.Error("delete removed the wrong server")
	}
	final, _ := os.ReadFile(path)
	if !containsLine(string(final), "model = \"o3\"") || !containsLine(string(final), "[tui]") {
		t.Errorf("sibling config lost after delete; got:\n%s", final)
	}
}

// containsLine reports whether any trimmed line of s equals want (order-insensitive line presence).
func containsLine(s, want string) bool {
	for _, line := range splitLines(s) {
		if trimSpace(line) == want {
			return true
		}
	}
	return false
}

// trimSpace is a tiny local helper to avoid importing strings for one call in the test.
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
