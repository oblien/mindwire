package opencode

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Persistent MCP-server config for opencode, exposed through the optional agent.MCPServerModule
// (type-asserted by the API, never part of the mandatory Adapter). opencode reads persistent MCP servers
// from the top-level `mcp` object of its user config, opencode.json, loading them on every run. This is
// DISTINCT from a per-turn MCP passthrough (opencode has none): here we read/write the config opencode
// applies to every session. User scope only — opencode.json is a single user-level file.
//
// SURGICAL IO: opencode.json holds unrelated top-level keys ($schema, model, theme, provider, …). We
// reuse providers.go's subtree-preserving reader/writer (readSubtree/mutateSubtree) to mutate ONLY the
// `mcp` subtree, so every sibling key — including a `provider` block written by providers.go — is
// preserved byte-for-byte. We NEVER surface any key other than `mcp`.
//
// SCHEMA (verified against opencode's config.json schema / docs, opencode 1.14.x): each entry is keyed
// by name under `mcp` and is one of two shapes —
//
//	local  : {type:"local",  command:[bin, ...args], environment?:{K:V}, cwd?:string, enabled?:bool}
//	remote : {type:"remote", url:string, headers?:{K:V}, enabled?:bool}
//
// The canonical agent.MCPServer transcodes onto these: Command+Args → the `command` ARRAY (opencode
// carries the binary as command[0]); Env → `environment`; Cwd → `cwd`; URL → remote `url`; HTTPHeaders →
// `headers`. HTTP auth: a canonical BearerTokenEnvVar becomes an `Authorization: Bearer {env:VAR}` header
// using opencode's OWN `{env:VAR}` config-substitution (the same mechanism providers.go uses for a custom
// provider's apiKey) — the env-var NAME, never a secret value. `enabled` is omitted on write (opencode
// treats an absent flag as enabled), matching opencode's own minimal examples.
var _ agent.MCPServerModule = adapter{}

// ocMCP is the subset of an opencode.json `mcp.<name>` block mindwire reads and writes. Unknown fields
// within a block we own are not preserved (Set replaces the whole entry, mirroring providers.go and the
// Claude/Codex MCP writers); sibling servers and all other top-level keys are preserved.
type ocMCP struct {
	Type        string            `json:"type,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// mcpConfigPath is opencode.json (the same file ConfigPath / providers.go surface). Empty when the config
// home can't be resolved.
func mcpConfigPath() string {
	base := configDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "opencode.json")
}

// MCPScopes: opencode's persistent MCP config is user-scope only (opencode.json under the config home).
func (adapter) MCPScopes() []agent.MemoryScope { return []agent.MemoryScope{agent.MemoryUser} }

// ListMCPServers returns every server under the `mcp` object, translated to the canonical shape. A
// missing file or absent/malformed subtree yields an empty map (forgiving). dir is ignored — user-only.
func (adapter) ListMCPServers(scope agent.MemoryScope, _ string) (map[string]agent.MCPServer, error) {
	if scope != agent.MemoryUser {
		return nil, fmt.Errorf("opencode supports MCP config only at user scope")
	}
	path := mcpConfigPath()
	if path == "" {
		return nil, fmt.Errorf("cannot resolve opencode config home")
	}
	blocks, err := readSubtree(path, "mcp")
	if err != nil {
		return nil, err
	}
	out := map[string]agent.MCPServer{}
	for name, raw := range blocks {
		var m ocMCP
		if err := json.Unmarshal(raw, &m); err != nil {
			continue // skip a malformed sibling rather than fail the whole list
		}
		out[name] = fromOpencode(m)
	}
	return out, nil
}

// SetMCPServer writes one server under name into the `mcp` object, preserving every other server AND all
// sibling top-level keys. Replaces an existing entry of the same name.
func (adapter) SetMCPServer(scope agent.MemoryScope, _ string, name string, server agent.MCPServer) error {
	if scope != agent.MemoryUser {
		return fmt.Errorf("opencode supports MCP config only at user scope")
	}
	if err := agent.ValidatePromptName(name); err != nil {
		return err
	}
	path := mcpConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve opencode config home")
	}
	raw, err := json.Marshal(toOpencode(server))
	if err != nil {
		return err
	}
	return mutateSubtree(path, "mcp", func(blocks map[string]json.RawMessage) error {
		blocks[name] = raw
		return nil
	})
}

// DeleteMCPServer removes one server by name from the `mcp` object. Removing an absent server (or from a
// missing file) is not an error — idempotent.
func (adapter) DeleteMCPServer(scope agent.MemoryScope, _ string, name string) error {
	if scope != agent.MemoryUser {
		return fmt.Errorf("opencode supports MCP config only at user scope")
	}
	path := mcpConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve opencode config home")
	}
	return mutateSubtree(path, "mcp", func(blocks map[string]json.RawMessage) error {
		delete(blocks, name)
		return nil
	})
}

// toOpencode maps the canonical server to opencode's native `mcp` block. A Command means a local/stdio
// server (command carries the binary + args as one array); otherwise a remote/HTTP server, where a
// canonical BearerTokenEnvVar becomes an `Authorization: Bearer {env:VAR}` header via opencode's own
// `{env:VAR}` expansion (env-var NAME, never a value), unless an Authorization header is already set.
func toOpencode(s agent.MCPServer) ocMCP {
	if s.Command != "" {
		return ocMCP{
			Type:        "local",
			Command:     append([]string{s.Command}, s.Args...),
			Environment: s.Env,
			Cwd:         s.Cwd,
		}
	}
	m := ocMCP{Type: "remote", URL: s.URL}
	if len(s.HTTPHeaders) > 0 {
		m.Headers = map[string]string{}
		for k, v := range s.HTTPHeaders {
			m.Headers[k] = v
		}
	}
	if s.BearerTokenEnvVar != "" {
		if m.Headers == nil {
			m.Headers = map[string]string{}
		}
		if _, ok := m.Headers["Authorization"]; !ok {
			m.Headers["Authorization"] = "Bearer {env:" + s.BearerTokenEnvVar + "}"
		}
	}
	return m
}

// fromOpencode maps opencode's native `mcp` block to the canonical shape. A remote block's headers
// round-trip verbatim (BearerTokenEnvVar stays empty — the `{env:VAR}` lives inside the header value,
// exactly as Claude's reader leaves `${VAR}` headers intact). A local block's `command` array is split
// back into Command (head) + Args (tail).
func fromOpencode(m ocMCP) agent.MCPServer {
	if m.Type == "remote" || (m.URL != "" && len(m.Command) == 0) {
		return agent.MCPServer{URL: m.URL, HTTPHeaders: m.Headers}
	}
	s := agent.MCPServer{Env: m.Environment, Cwd: m.Cwd}
	if len(m.Command) > 0 {
		s.Command = m.Command[0]
		if len(m.Command) > 1 {
			s.Args = m.Command[1:]
		}
	}
	return s
}
