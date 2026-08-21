package agent

// Persistent MCP-server config. MCP (Model Context Protocol) servers can be passed PER TURN
// (TurnOptions.MCPServers), but the agents also read PERSISTENT MCP config from disk — Claude from its
// JSON stores (<base>/.claude.json at user scope, <dir>/.mcp.json at project scope), Codex from
// $CODEX_HOME/config.toml `[mcp_servers.*]`. This file adds the persistent surface so a client can
// list, set, and delete the on-disk MCP servers an agent loads on every run.
//
// Exposed through an OPTIONAL adapter capability (type-asserted like MemoryModule, NOT bolted onto the
// mandatory Adapter interface): an agent with no persistent MCP store simply doesn't implement it and
// the API answers 400. DISTINCT from the per-turn MCPServers passthrough (Capabilities.MCPServers /
// TurnOptions.MCPServers): that honors a one-shot payload for a single turn; this reads/writes the
// persistent config files that apply to every run.
//
// SECURITY: an MCPServer carries a command/args/url and, for HTTP auth, only the NAME of the env var
// holding a bearer token (BearerTokenEnvVar) — never a secret value. Env is plain (name→value) config
// the server process receives; a client that puts a secret there does so explicitly, exactly as it
// would in the agent's own config file. mindwire never injects its own auth secrets here. On Claude,
// only the `mcpServers` subtree of the store file is ever read or written — every sibling key is
// preserved byte-for-byte and never surfaced.

// MCPServer is one persistent MCP-server definition, the canonical agent-agnostic shape (promoted from
// the codex per-turn transcode). Two forms: stdio (Command/Args/Env/Cwd) and streamable-HTTP
// (URL/BearerTokenEnvVar/HTTPHeaders). Every field is omitempty so a round-trip carries only what a
// server actually sets.
type MCPServer struct {
	Command           string            `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	Cwd               string            `json:"cwd,omitempty"`
	URL               string            `json:"url,omitempty"`
	BearerTokenEnvVar string            `json:"bearerTokenEnvVar,omitempty"`
	HTTPHeaders       map[string]string `json:"httpHeaders,omitempty"`
}

// MCPServerModule is an OPTIONAL adapter capability (type-asserted like MemoryModule): list, set, and
// delete the agent's persistent MCP-server config across canonical scopes. An implementer sets
// Capabilities.MCPConfig=true; the API type-asserts this interface as the authoritative gate before
// serving /mcp. Claude (project + user) and Codex (user only) implement it. A single-server GET is
// served by listing the scope and indexing the map, so there is no separate read method.
type MCPServerModule interface {
	// MCPScopes lists the scopes this agent's persistent MCP config supports (Claude: project + user;
	// Codex: user only).
	MCPScopes() []MemoryScope
	// ListMCPServers returns the servers configured at a scope, keyed by name. dir is the resolved
	// project directory (used for project scope; ignored for user scope). A missing config file yields
	// an empty map — reads are forgiving.
	ListMCPServers(scope MemoryScope, dir string) (map[string]MCPServer, error)
	// SetMCPServer writes one server under name at a scope (creating the config file / directory as
	// needed), preserving every other server AND all sibling config. Replaces an existing entry of the
	// same name.
	SetMCPServer(scope MemoryScope, dir, name string, server MCPServer) error
	// DeleteMCPServer removes one server by name at a scope. Removing an absent server is not an error
	// (idempotent), so the caller can DELETE without a prior existence check.
	DeleteMCPServer(scope MemoryScope, dir, name string) error
}
