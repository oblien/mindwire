package agent

// Support says whether an agent provides a feature natively, needs the core to
// emulate it, or doesn't have it. The hybrid rule: Native → use the agent;
// Emulated → the core implements it (e.g. records the relayed stream for history).
type Support string

const (
	SupportNone     Support = "none"
	SupportNative   Support = "native"
	SupportEmulated Support = "emulated"
)

// Protocol is how the daemon DRIVES the agent for a turn — the control channel, orthogonal to
// Output (the result format). Declared per adapter; the adapter's RunStream composes the matching
// driver (internal/driver). CLI is the universal fallback — any agent that ships only a command
// line uses it; agents with a native REST/SSE API or a persistent protocol declare those and
// implement driver.Driver directly. The core never branches on this; it only reads unified events.
type Protocol string

const (
	ProtocolCLI        Protocol = "cli"        // spawn the agent CLI per turn (Claude: claude -p)
	ProtocolHTTP       Protocol = "http"       // call the agent's native REST/SSE API
	ProtocolPersistent Protocol = "persistent" // a long-lived process spoken to over a protocol
)

// OutputMode is how an agent emits a turn's output.
type OutputMode string

const (
	// OutputStructuredJSON: the CLI emits structured JSON we map to unified events.
	OutputStructuredJSON OutputMode = "structured_json"
	// OutputTerminal: plain/ANSI terminal output → a terminal-to-structured converter (later).
	OutputTerminal OutputMode = "terminal"
)

// Capabilities is the per-agent feature matrix. Two audiences read it:
//   - the CORE switches behavior on some fields (native-vs-emulated). Today: History
//     (Native → read the agent's transcript; else serve the recorded log), Cancel
//     (gates POST /runs/{id}/cancel), the user-in-loop ingress trio Respond/Input/
//     Interrupt, and the runtime-control pair SetModel/SetPermissionMode (each gates its
//     route exactly like Cancel). As emulation lands, more move here.
//   - the CLIENT reads the rest as capability HINTS to shape its UI (Output, Sessions,
//     Resume, ToolEvents, Persistent, Models). They are declared per agent so the app
//     hardcodes nothing; the daemon does not yet branch on them.
//
// Keeping this split explicit avoids the trap of a field that looks like a switch but
// nothing consults — add core branching when the emulation for that field exists.
type Capabilities struct {
	Protocol   Protocol   `json:"protocol"` // how the daemon drives the agent (cli | http | persistent)
	Output     OutputMode `json:"output"`
	History    Support    `json:"history"`    // CORE switch (messages endpoint)
	Sessions   Support    `json:"sessions"`   // client hint
	Resume     bool       `json:"resume"`     // client hint
	ToolEvents bool       `json:"toolEvents"` // client hint
	Cancel     bool       `json:"cancel"`     // CORE switch (cancel endpoint)
	Persistent bool       `json:"persistent"` // client hint: holds a live stdin process (else one-shot per turn)
	Models     bool       `json:"models"`     // client hint
	// ImageInput is a client hint: the agent delivers image attachments as TRUE vision content (the
	// model sees pixels), not just a path the model must open with a Read tool. Attachments themselves
	// are ungated (any agent may receive them); this only tells a UI whether images are seen natively.
	ImageInput bool `json:"imageInput"` // client hint: image attachments are sent as real vision input
	// User-in-loop ingress — each a CORE switch gating its route. The supervisor allocates a run's
	// inbound channel when any of these (or the runtime-control pair below) is set; the adapter must
	// pick a transport that can pump it.
	Respond   bool `json:"respond"`   // CORE switch: POST /runs/{id}/respond (answer an approval/question/plan)
	Input     bool `json:"input"`     // CORE switch: POST /runs/{id}/input (inject a follow-up message mid-turn)
	Interrupt bool `json:"interrupt"` // CORE switch: POST /runs/{id}/interrupt (soft-stop, keeping the turn open)
	// Runtime control — change the model / permission mode of a LIVE turn (only meaningful on a
	// persistent transport; a one-shot turn accepts the call as a best-effort no-op). Each a CORE
	// switch gating its route, like the ingress trio.
	SetModel          bool `json:"setModel"`          // CORE switch: POST /runs/{id}/set-model (switch model mid-turn)
	SetPermissionMode bool `json:"setPermissionMode"` // CORE switch: POST /runs/{id}/set-permission-mode (switch permission mode mid-turn)
	// Turn-option support — whether the agent honors these per-turn inputs. Each a CORE switch gating
	// api.turn: a request carrying an option the selected agent can't honor gets a 400 (an honest
	// rejection) rather than a silent drop. SystemPrompt/AppendSystemPrompt cover the systemPrompt /
	// appendSystemPrompt inputs (typed TurnOptions.SystemPrompt OR the canon-addressed setting);
	// MCPServers covers TurnOptions.MCPServers. Declared per adapter so clients can also read them as
	// hints before sending.
	SystemPrompt       bool `json:"systemPrompt"`       // CORE switch (api.turn): honors a full system-prompt override
	AppendSystemPrompt bool `json:"appendSystemPrompt"` // CORE switch (api.turn): honors appending to the default system prompt
	MCPServers         bool `json:"mcpServers"`         // CORE switch (api.turn): honors per-turn MCP server config
	// Subagents / ClaudeSettings gate the two per-turn agent-native passthroughs (TurnOptions.Subagents,
	// TurnOptions.ClaudeSettings) exactly like MCPServers: an agent that can't honor the shape declares
	// false and api.turn 400s. Subagents = per-turn subagent defs (Claude --agents); ClaudeSettings = a
	// per-turn settings/hooks bundle (Claude --settings).
	Subagents      bool `json:"subagents"`      // CORE switch (api.turn): honors per-turn subagent definitions
	ClaudeSettings bool `json:"claudeSettings"` // CORE switch (api.turn): honors a per-turn settings/hooks bundle
	// Persistent prompt/memory surface — CLIENT hints that the agent exposes fetch+set for its
	// persistent artifacts, surfaced by GET /agent so a UI can show/hide the panels. The authoritative
	// gate is the type assertion in the API handler (agent.MemoryModule / agent.PromptsModule), not
	// these flags — an adapter that implements the module sets the matching flag true.
	Memory          bool `json:"memory"`          // agent exposes persistent memory files (CLAUDE.md / AGENTS.md) via /memory
	PromptTemplates bool `json:"promptTemplates"` // agent exposes saved prompt templates (slash-commands / saved prompts) via /prompts
	// SubagentDefs gates the persistent subagent-definition store (/subagents), type-asserted via
	// agent.SubagentsModule. DISTINCT from the per-turn Subagents passthrough above: Subagents honors a
	// per-turn --agents payload; SubagentDefs reads/writes the on-disk .claude/agents/*.md files. Claude-only.
	SubagentDefs bool `json:"subagentDefs"` // agent exposes persistent subagent definition files via /subagents
	// MCPConfig gates the persistent MCP-server config store (/mcp), type-asserted via
	// agent.MCPServerModule. DISTINCT from the per-turn MCPServers passthrough above: MCPServers honors a
	// per-turn --mcp-config payload for one turn; MCPConfig reads/writes the on-disk config the agent
	// loads on every run (Claude .claude.json / .mcp.json, Codex config.toml [mcp_servers.*]).
	MCPConfig bool `json:"mcpConfig"` // agent exposes persistent MCP-server config via /mcp
	// CustomProviders gates the custom-LLM-provider control plane (/providers), type-asserted via
	// agent.CustomProvidersModule. An agent that can point at a custom OpenAI-compatible endpoint
	// (opencode's opencode.json provider block, Codex's config.toml [model_providers.*]) implements the
	// module and sets this true; the registered provider's models then appear in /models (Custom:true)
	// and the secret flows only through EnvForRun (config carries an {env:VAR} placeholder, never the
	// key). Claude declares false — its custom-endpoint story is the gateway auth lane, not a provider
	// registry — so /providers 400s for it.
	CustomProviders bool `json:"customProviders"` // agent exposes custom-provider registration via /providers
	// CompactNow is a client hint that the agent supports on-demand conversation compaction via
	// POST /chats/{id}/compact. The authoritative gate is the type assertion agent.CompactModule in the
	// API handler, not this flag — an adapter that implements the module sets it true.
	CompactNow bool `json:"compactNow"` // agent supports on-demand compaction (POST /chats/{id}/compact)
	// Resolve is a client hint that the agent can run in GLOBAL-RESOLVE mode (POST /turns {mode:"resolve"}):
	// the daemon holds the run open and auto-continues the agent's own multi-step work until the task is
	// globally complete. UNLIKE the other CORE switches, resolve is NOT type-asserted or gated in the API —
	// it is pure daemon logic (the supervisor loops over the existing RunStream + session resume every
	// adapter already has), so every agent that can resume can be resolved. The flag is a UI hint only.
	Resolve bool `json:"resolve"` // agent supports global-resolve runs (POST /turns {mode:"resolve"})
}
