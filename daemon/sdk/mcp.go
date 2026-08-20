package mindwire

import (
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// MCP is the persistent MCP-server sub-API, reachable as Client.MCP. It reads and writes the MCP servers
// an agent loads on every run from its own on-disk config — Claude's JSON stores (project .mcp.json +
// user .claude.json) and Codex's config.toml [mcp_servers.*] tables — as opposed to a turn's per-turn
// Options.MCPServers passthrough. Each call maps one-for-one to a /mcp HTTP route and enforces the same
// gates: an agent without the underlying optional module surfaces as APIError{400}; a missing server on
// Get is APIError{404}; an invalid name or unsupported scope is 400. It is scoped to its client's default
// agent and rebinds under WithAgent; override per call with ForAgent.
//
// A server definition carries an env-var NAME for HTTP bearer auth (BearerTokenEnvVar), never a secret
// value — no credential ever crosses this surface. dir selects the project-scope working directory —
// empty resolves to the client's cwd (else the process cwd).
type MCP struct{ c *Client }

// mcpModule resolves the scoped agent and type-asserts its optional MCPServerModule, returning
// APIError{400} when the agent doesn't expose one (mirroring the /mcp handler's capability gate).
func (m *MCP) mcpModule(op string, opts []ScopedOption) (agent.MCPServerModule, error) {
	ag, err := m.c.resolve(opts)
	if err != nil {
		return nil, err
	}
	mod, ok := ag.Adapter.(agent.MCPServerModule)
	if !ok {
		return nil, &APIError{Message: "agent does not support persistent MCP config", Status: http.StatusBadRequest, Op: op}
	}
	return mod, nil
}

// dir resolves the project directory a project-scope op targets: an explicit dir wins, else the client's
// cwd (else the process cwd). Mirrors the HTTP dirParam helper.
func (m *MCP) dir(dir string) string {
	return agent.ResolveDir(dir, m.c.core.sup.CWD())
}

// List returns the scoped agent's persistent MCP servers across every supported scope (Claude: project +
// user; Codex: user only), keyed scope→name→server. A missing config file yields an empty map for that
// scope, not an error; any other failure is APIError{500}, matching GET /mcp. An agent without the module
// is APIError{400}.
func (m *MCP) List(dir string, opts ...ScopedOption) (map[MemoryScope]map[string]MCPServer, error) {
	mod, err := m.mcpModule("MCP.List", opts)
	if err != nil {
		return nil, err
	}
	resolved := m.dir(dir)
	out := map[MemoryScope]map[string]MCPServer{}
	for _, scope := range mod.MCPScopes() {
		servers, lerr := mod.ListMCPServers(scope, resolved)
		if lerr != nil {
			return nil, &APIError{Message: lerr.Error(), Status: http.StatusInternalServerError, Op: "MCP.List", Cause: lerr}
		}
		if servers == nil {
			servers = map[string]MCPServer{}
		}
		out[scope] = servers
	}
	return out, nil
}

// Get returns one server by name at a scope. An empty scope defaults to user. A server that isn't
// configured maps to APIError{404}; an unsupported scope to APIError{400}, matching GET /mcp/{name}.
func (m *MCP) Get(scope MemoryScope, dir, name string, opts ...ScopedOption) (MCPServer, error) {
	mod, err := m.mcpModule("MCP.Get", opts)
	if err != nil {
		return MCPServer{}, err
	}
	servers, lerr := mod.ListMCPServers(orScope(scope), m.dir(dir))
	if lerr != nil {
		return MCPServer{}, &APIError{Message: lerr.Error(), Status: http.StatusBadRequest, Op: "MCP.Get", Cause: lerr}
	}
	server, ok := servers[name]
	if !ok {
		return MCPServer{}, &APIError{Message: "mcp server not found", Status: http.StatusNotFound, Op: "MCP.Get"}
	}
	return server, nil
}

// Set writes one server at a scope and returns it (echoed, as PUT /mcp/{name} does). An empty scope
// defaults to user. An invalid name or unsupported scope is APIError{400}. HTTP auth is expressed via
// BearerTokenEnvVar (an env-var name) or HTTPHeaders — never a secret value.
func (m *MCP) Set(scope MemoryScope, dir, name string, server MCPServer, opts ...ScopedOption) (MCPServer, error) {
	mod, err := m.mcpModule("MCP.Set", opts)
	if err != nil {
		return MCPServer{}, err
	}
	if werr := mod.SetMCPServer(orScope(scope), m.dir(dir), name, server); werr != nil {
		return MCPServer{}, &APIError{Message: werr.Error(), Status: http.StatusBadRequest, Op: "MCP.Set", Cause: werr}
	}
	return server, nil
}

// Delete removes one server at a scope. An empty scope defaults to user. Deleting an absent server (or
// from a missing config) is not an error (idempotent), matching DELETE /mcp/{name}. An unsupported scope
// is APIError{400}.
func (m *MCP) Delete(scope MemoryScope, dir, name string, opts ...ScopedOption) error {
	mod, err := m.mcpModule("MCP.Delete", opts)
	if err != nil {
		return err
	}
	if derr := mod.DeleteMCPServer(orScope(scope), m.dir(dir), name); derr != nil {
		return &APIError{Message: derr.Error(), Status: http.StatusBadRequest, Op: "MCP.Delete", Cause: derr}
	}
	return nil
}
