package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestMCPRoundTrip verifies opencode's persistent MCP config end-to-end: Set writes local + remote
// servers into opencode.json's `mcp` object (a remote server's bearer env-var becomes an
// `Authorization: Bearer {env:VAR}` header, never a literal secret) while preserving every unrelated
// top-level key — including a `provider` block owned by providers.go — byte-for-byte; List reads them
// back in canonical shape; and Delete removes only that entry, dropping `mcp` entirely when it empties.
func TestMCPRoundTrip(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := mcpConfigPath()

	// Seed opencode.json with unrelated top-level keys AND a provider block (proving the two subtree
	// writers coexist without clobbering each other).
	seed := `{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4",
  "provider": {
    "my-llm": { "npm": "@ai-sdk/openai-compatible", "options": { "baseURL": "https://llm.example/v1" } }
  }
}
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir opencode config home: %v", err)
	}
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed opencode.json: %v", err)
	}

	a := adapter{}

	// A local/stdio server with args, env, and cwd.
	local := agent.MCPServer{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-everything"},
		Env:     map[string]string{"MY_ENV_VAR": "value"},
		Cwd:     "/work/repo",
	}
	if err := a.SetMCPServer(agent.MemoryUser, "", "everything", local); err != nil {
		t.Fatalf("SetMCPServer local: %v", err)
	}
	// A remote/HTTP server whose auth is expressed as an env-var NAME.
	remote := agent.MCPServer{
		URL:               "https://mcp.example.com",
		BearerTokenEnvVar: "MCP_TOKEN",
		HTTPHeaders:       map[string]string{"X-Extra": "1"},
	}
	if err := a.SetMCPServer(agent.MemoryUser, "", "remote-one", remote); err != nil {
		t.Fatalf("SetMCPServer remote: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	// The bearer token's *value* must never appear — only its env-var name, inside the placeholder.
	if strings.Contains(string(raw), "Bearer MCP_TOKEN\"") {
		t.Fatalf("opencode.json wrote a literal bearer value instead of a placeholder:\n%s", raw)
	}
	if !strings.Contains(string(raw), "Bearer {env:MCP_TOKEN}") {
		t.Fatalf("opencode.json missing the {env:VAR} auth placeholder:\n%s", raw)
	}

	// Sibling top-level keys — including the provider block — survive the mcp writes.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("parse opencode.json: %v", err)
	}
	for _, k := range []string{"$schema", "model", "provider", "mcp"} {
		if _, ok := top[k]; !ok {
			t.Fatalf("mcp write dropped top-level key %q; file:\n%s", k, raw)
		}
	}

	// The local block is `type:"local"` with the binary as command[0].
	var blocks map[string]ocMCP
	if err := json.Unmarshal(top["mcp"], &blocks); err != nil {
		t.Fatalf("parse mcp subtree: %v", err)
	}
	if b := blocks["everything"]; b.Type != "local" || len(b.Command) != 3 || b.Command[0] != "npx" || b.Cwd != "/work/repo" {
		t.Fatalf("local block = %+v", b)
	}
	if b := blocks["remote-one"]; b.Type != "remote" || b.URL != "https://mcp.example.com" {
		t.Fatalf("remote block = %+v", b)
	}

	// List reads both back in canonical shape.
	list, err := a.ListMCPServers(agent.MemoryUser, "")
	if err != nil {
		t.Fatalf("ListMCPServers: %v", err)
	}
	gotLocal := list["everything"]
	if gotLocal.Command != "npx" || len(gotLocal.Args) != 2 || gotLocal.Args[0] != "-y" ||
		gotLocal.Env["MY_ENV_VAR"] != "value" || gotLocal.Cwd != "/work/repo" {
		t.Fatalf("List local = %+v", gotLocal)
	}
	gotRemote := list["remote-one"]
	if gotRemote.URL != "https://mcp.example.com" ||
		gotRemote.HTTPHeaders["Authorization"] != "Bearer {env:MCP_TOKEN}" ||
		gotRemote.HTTPHeaders["X-Extra"] != "1" {
		t.Fatalf("List remote = %+v", gotRemote)
	}

	// Delete removes one server; the other and the provider block remain.
	if err := a.DeleteMCPServer(agent.MemoryUser, "", "everything"); err != nil {
		t.Fatalf("DeleteMCPServer: %v", err)
	}
	list, _ = a.ListMCPServers(agent.MemoryUser, "")
	if _, ok := list["everything"]; ok {
		t.Fatalf("Delete left the server behind")
	}
	if _, ok := list["remote-one"]; !ok {
		t.Fatalf("Delete removed the wrong server")
	}

	// Deleting the last server drops the `mcp` key entirely, but keeps the provider block.
	if err := a.DeleteMCPServer(agent.MemoryUser, "", "remote-one"); err != nil {
		t.Fatalf("DeleteMCPServer (last): %v", err)
	}
	raw, _ = os.ReadFile(path)
	var afterDelete map[string]json.RawMessage
	if err := json.Unmarshal(raw, &afterDelete); err != nil {
		t.Fatalf("parse after delete: %v", err)
	}
	if _, ok := afterDelete["mcp"]; ok {
		t.Fatalf("Delete left an empty `mcp` key behind:\n%s", raw)
	}
	if _, ok := afterDelete["provider"]; !ok {
		t.Fatalf("Delete clobbered the sibling provider block:\n%s", raw)
	}

	// Deleting again (from a file with no `mcp`) is idempotent.
	if err := a.DeleteMCPServer(agent.MemoryUser, "", "remote-one"); err != nil {
		t.Fatalf("DeleteMCPServer (idempotent): %v", err)
	}
}

// TestMCPScopeGate confirms opencode rejects any non-user scope.
func TestMCPScopeGate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := adapter{}
	if _, err := a.ListMCPServers(agent.MemoryProject, ""); err == nil {
		t.Fatalf("ListMCPServers at project scope: want error")
	}
	if err := a.SetMCPServer(agent.MemoryProject, "", "x", agent.MCPServer{Command: "echo"}); err == nil {
		t.Fatalf("SetMCPServer at project scope: want error")
	}
}
