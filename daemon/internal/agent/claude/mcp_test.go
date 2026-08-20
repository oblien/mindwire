package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestClaudeMCPModule exercises the persistent MCP config surface at both scopes against a temp
// CLAUDE_CONFIG_DIR (user store = <home>/.claude.json) and a temp project (.mcp.json). The load-bearing
// invariant: writing/deleting a server mutates ONLY the mcpServers subtree — every sibling top-level key
// in .claude.json survives byte-for-byte.
func TestClaudeMCPModule(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	a := adapter{}

	if !a.Capabilities().MCPConfig {
		t.Fatal("claude Capabilities.MCPConfig = false, want true")
	}
	if scopes := a.MCPScopes(); len(scopes) != 2 {
		t.Fatalf("MCPScopes = %v, want project+user", scopes)
	}

	// Seed the user store with unrelated top-level keys the surgical writer must preserve, including a
	// nested object we compare byte-for-byte.
	userStore := filepath.Join(home, ".claude.json")
	seed := map[string]json.RawMessage{
		"numStartups":  json.RawMessage(`42`),
		"oauthAccount": json.RawMessage(`{"emailAddress":"a@b.co","organizationUuid":"org-123"}`),
		"projects":     json.RawMessage(`{"/some/path":{"lastCost":1.5}}`),
	}
	seedBytes, _ := json.Marshal(seed)
	if err := os.WriteFile(userStore, seedBytes, 0o600); err != nil {
		t.Fatalf("seed user store: %v", err)
	}

	// Write a stdio server at user scope and an HTTP server (with bearer env-var) at project scope.
	stdio := agent.MCPServer{Command: "srv", Args: []string{"--x"}, Env: map[string]string{"K": "v"}}
	if err := a.SetMCPServer(agent.MemoryUser, proj, "local", stdio); err != nil {
		t.Fatalf("Set(user/local): %v", err)
	}
	remote := agent.MCPServer{URL: "https://mcp.example.com", BearerTokenEnvVar: "TOK", HTTPHeaders: map[string]string{"X-A": "1"}}
	if err := a.SetMCPServer(agent.MemoryProject, proj, "remote", remote); err != nil {
		t.Fatalf("Set(project/remote): %v", err)
	}

	// User-scope list returns the stdio server round-tripped exactly (Command form has no bearer mapping).
	userList, err := a.ListMCPServers(agent.MemoryUser, proj)
	if err != nil {
		t.Fatalf("List(user): %v", err)
	}
	if !reflect.DeepEqual(userList["local"], stdio) {
		t.Errorf("user round-trip = %+v, want %+v", userList["local"], stdio)
	}

	// Project store lives at <dir>/.mcp.json; the bearer env-var maps to an Authorization: Bearer ${TOK}
	// header (the NAME, never a secret) and comes back through HTTPHeaders (documented asymmetry).
	projList, err := a.ListMCPServers(agent.MemoryProject, proj)
	if err != nil {
		t.Fatalf("List(project): %v", err)
	}
	got := projList["remote"]
	if got.URL != remote.URL || got.HTTPHeaders["X-A"] != "1" {
		t.Errorf("project round-trip = %+v", got)
	}
	if auth := got.HTTPHeaders["Authorization"]; auth != "Bearer ${TOK}" {
		t.Errorf("bearer mapping = %q, want 'Bearer ${TOK}'", auth)
	}
	if _, err := os.Stat(filepath.Join(proj, ".mcp.json")); err != nil {
		t.Errorf("project .mcp.json missing: %v", err)
	}

	// Sibling top-level keys in the user store are preserved byte-for-byte.
	assertSiblingsPreserved(t, userStore, seed)

	// Get one server; a missing name is fs.ErrNotExist-style absence (handled at the API as 404).
	if _, ok := userList["nope"]; ok {
		t.Error("unexpected server 'nope'")
	}

	// Delete removes only the named server; siblings and the store's other contents remain.
	if err := a.DeleteMCPServer(agent.MemoryUser, proj, "local"); err != nil {
		t.Fatalf("Delete(user/local): %v", err)
	}
	if err := a.DeleteMCPServer(agent.MemoryUser, proj, "local"); err != nil {
		t.Fatalf("Delete idempotent: %v", err)
	}
	after, _ := a.ListMCPServers(agent.MemoryUser, proj)
	if len(after) != 0 {
		t.Fatalf("after delete = %+v, want empty", after)
	}
	assertSiblingsPreserved(t, userStore, seed)
}

// assertSiblingsPreserved re-reads the store and asserts every seeded sibling key still decodes to its
// original bytes (JSON-equal), i.e. the mcpServers mutation touched nothing else.
func assertSiblingsPreserved(t *testing.T, path string, seed map[string]json.RawMessage) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read store: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("parse store: %v", err)
	}
	for k, want := range seed {
		raw, ok := top[k]
		if !ok {
			t.Errorf("sibling key %q was dropped", k)
			continue
		}
		if !jsonEqual(t, raw, want) {
			t.Errorf("sibling key %q changed: got %s want %s", k, raw, want)
		}
	}
}

// jsonEqual compares two raw JSON values structurally (indifferent to key order / whitespace).
func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}
