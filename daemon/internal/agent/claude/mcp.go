package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Persistent MCP-server config for Claude Code, exposed through the optional agent.MCPServerModule
// (type-asserted by the API, never part of the mandatory Adapter). Claude reads persistent MCP servers
// from two JSON stores: user scope at `<CLAUDE_CONFIG_DIR>/.claude.json` (else `~/.claude.json`) and
// project scope at `<dir>/.mcp.json`. Each stores them under a top-level `mcpServers` object. This is
// DISTINCT from the per-turn --mcp-config passthrough: here we read/write the config Claude loads on
// every run.
//
// SURGICAL IO: the user store (`.claude.json`) holds MANY unrelated top-level keys (oauth, machine id,
// project state, …). We decode the whole file to map[string]json.RawMessage, mutate ONLY the
// `mcpServers` subtree, and re-marshal — so every sibling key's value is preserved byte-for-byte (only
// JSON key order / indentation is normalized, which is insignificant). We NEVER surface any key other
// than `mcpServers`, and mindwire never injects an auth secret here. HTTP auth is expressed through
// Claude's own `headers` (Claude supports `${VAR}` expansion), so a canonical bearerTokenEnvVar maps to
// an `Authorization: Bearer ${VAR}` header — the env-var NAME, never a value.
var _ agent.MCPServerModule = adapter{}

// claudeServer is Claude's native MCP-server JSON shape. stdio (command/args/env) and http/sse
// (url/headers) forms; `type` is derived on write and ignored (tolerated) on read.
type claudeServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// MCPScopes: Claude has both a project store (.mcp.json) and a user store (.claude.json).
func (adapter) MCPScopes() []agent.MemoryScope {
	return []agent.MemoryScope{agent.MemoryProject, agent.MemoryUser}
}

// mcpStorePath resolves the store file for a scope: user → <CLAUDE_CONFIG_DIR>/.claude.json (else
// ~/.claude.json), project → <dir>/.mcp.json. An unknown scope or unresolvable path is an error.
func mcpStorePath(scope agent.MemoryScope, dir string) (string, error) {
	switch scope {
	case agent.MemoryUser:
		if base := os.Getenv("CLAUDE_CONFIG_DIR"); strings.TrimSpace(base) != "" {
			return filepath.Join(base, ".claude.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		return filepath.Join(home, ".claude.json"), nil
	case agent.MemoryProject:
		if strings.TrimSpace(dir) == "" {
			return "", fmt.Errorf("project scope needs a working directory")
		}
		return filepath.Join(dir, ".mcp.json"), nil
	default:
		return "", fmt.Errorf("unsupported MCP scope %q", scope)
	}
}

// ListMCPServers returns the servers under the scope's `mcpServers` object, translated to the canonical
// shape. A missing file or absent/malformed subtree yields an empty map (forgiving).
func (adapter) ListMCPServers(scope agent.MemoryScope, dir string) (map[string]agent.MCPServer, error) {
	path, err := mcpStorePath(scope, dir)
	if err != nil {
		return nil, err
	}
	top, err := readStore(path)
	if err != nil {
		return nil, err
	}
	out := map[string]agent.MCPServer{}
	raw, ok := top["mcpServers"]
	if !ok || len(raw) == 0 {
		return out, nil
	}
	var native map[string]claudeServer
	if err := json.Unmarshal(raw, &native); err != nil {
		return out, nil // a malformed subtree degrades to empty rather than erroring
	}
	for name, cs := range native {
		out[name] = fromClaude(cs)
	}
	return out, nil
}

// SetMCPServer writes one server into the scope's `mcpServers` object, preserving every other server
// AND all sibling top-level keys.
func (adapter) SetMCPServer(scope agent.MemoryScope, dir, name string, server agent.MCPServer) error {
	if err := agent.ValidatePromptName(name); err != nil {
		return err
	}
	path, err := mcpStorePath(scope, dir)
	if err != nil {
		return err
	}
	return mutateStore(path, func(servers map[string]json.RawMessage) error {
		raw, err := json.Marshal(toClaude(server))
		if err != nil {
			return err
		}
		servers[name] = raw
		return nil
	})
}

// DeleteMCPServer removes one server from the scope's `mcpServers` object. Removing an absent server (or
// from a missing file) is not an error (idempotent).
func (adapter) DeleteMCPServer(scope agent.MemoryScope, dir, name string) error {
	path, err := mcpStorePath(scope, dir)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return nil
	}
	return mutateStore(path, func(servers map[string]json.RawMessage) error {
		delete(servers, name)
		return nil
	})
}

// readStore decodes the store file's top-level object into raw key→value bytes. A missing or empty file
// yields an empty map (not an error).
func readStore(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	top := map[string]json.RawMessage{}
	if strings.TrimSpace(string(data)) == "" {
		return top, nil
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return top, nil
}

// mutateStore applies fn to the store's `mcpServers` subtree and writes the file back, leaving every
// other top-level key's value untouched. The file is written 0600 (it may hold user secrets in sibling
// keys and in server env/headers).
func mutateStore(path string, fn func(servers map[string]json.RawMessage) error) error {
	top, err := readStore(path)
	if err != nil {
		return err
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := top["mcpServers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			servers = map[string]json.RawMessage{} // replace a malformed subtree
		}
	}
	if err := fn(servers); err != nil {
		return err
	}
	subtree, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	top["mcpServers"] = subtree
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare config dir: %w", err)
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// toClaude maps the canonical server to Claude's native JSON. A Command means stdio; otherwise HTTP,
// where a canonical bearerTokenEnvVar becomes an `Authorization: Bearer ${VAR}` header (the env-var
// NAME via Claude's own `${VAR}` expansion — never a secret value), unless an Authorization header is
// already set.
func toClaude(s agent.MCPServer) claudeServer {
	if s.Command != "" {
		return claudeServer{Type: "stdio", Command: s.Command, Args: s.Args, Env: s.Env}
	}
	cs := claudeServer{Type: "http", URL: s.URL}
	if len(s.HTTPHeaders) > 0 {
		cs.Headers = map[string]string{}
		for k, v := range s.HTTPHeaders {
			cs.Headers[k] = v
		}
	}
	if s.BearerTokenEnvVar != "" {
		if cs.Headers == nil {
			cs.Headers = map[string]string{}
		}
		if _, ok := cs.Headers["Authorization"]; !ok {
			cs.Headers["Authorization"] = "Bearer ${" + s.BearerTokenEnvVar + "}"
		}
	}
	return cs
}

// fromClaude maps Claude's native JSON to the canonical shape. Claude expresses HTTP auth through
// literal/`${VAR}` headers, so HTTPHeaders round-trips verbatim and BearerTokenEnvVar stays empty.
func fromClaude(cs claudeServer) agent.MCPServer {
	return agent.MCPServer{
		Command:     cs.Command,
		Args:        cs.Args,
		Env:         cs.Env,
		URL:         cs.URL,
		HTTPHeaders: cs.Headers,
	}
}
