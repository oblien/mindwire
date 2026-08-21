// Package agent is the daemon CORE: the generic, agent-agnostic contracts every
// agent adapter implements (capabilities, dynamic settings/auth, unified events)
// plus the registry. ONE daemon per sandbox hosts EVERY registered adapter; a request
// selects one (?agent=<type>) and the client only ever speaks this one generic protocol.
// Adding an agent (Codex, Copilot, …) = one new adapter, zero core or client changes.
package agent

import (
	"context"
	"encoding/json"
	"sort"
)

// Version identifies this daemon binary's agent definitions. The app caches the
// fetched schema/catalog keyed by this, and refreshes when it changes.
const Version = "0.1.3"

// TurnInput is one chat turn's parameters.
type TurnInput struct {
	Message   string
	SessionID string            // the agent's own session id (for resume), if any
	CWD       string            // project dir inside the sandbox
	Config    map[string]string // settings (non-secret), e.g. model — per-turn overrides already folded in
	Env       map[string]string // runtime env from the AuthModule (e.g. ANTHROPIC_API_KEY)
	Options   TurnOptions       // per-turn structured options (Settings already resolved into Config)
	// Inbound carries the user's mid-turn ingress — permission answers, follow-up input, interrupts —
	// for a turn that pauses (user-in-loop). It is receive-only for the adapter and nil for the one-shot
	// hot path; an adapter that pauses picks a persistent transport and pumps these to the live process.
	// The supervisor owns the send end (Respond/SendInput/Interrupt); it is closed at turn end.
	Inbound <-chan Inbound
}

// Inbound is one piece of mid-turn ingress from the user, addressed to a running turn. It is the
// generic, agent-agnostic counterpart to the Interaction the adapter surfaces: the client answers an
// interaction (or steers/interrupts the turn) and the daemon hands the adapter this value to encode
// onto its native control channel. The adapter reads the correlators it stashed in Interaction.Meta
// back out of Meta here (echoed unchanged by the supervisor), so the encoding stays stateless.
type Inbound struct {
	// Kind selects the ingress: "response" answers an interaction (approval / question / plan),
	// "input" injects a follow-up user message mid-turn, "interrupt" asks the agent to stop the
	// current action while keeping the turn open (softer than a hard cancel), "set_model" switches
	// the live turn's model (Text = model, empty ⇒ reset to default), "set_permission_mode" switches
	// its permission mode (Text = mode). The last two are runtime control, not a reply to any
	// interaction, and only reach a persistent transport.
	Kind string `json:"kind"`
	// InteractionID is the interaction being answered (Kind=="response"): the Interaction.ID the
	// adapter emitted — a control-request id for approvals, a tool_use id for questions/plans.
	InteractionID string `json:"interactionId,omitempty"`
	// Decision is the chosen action id: "allow"/"deny" for an approval, "approve"/"reject" for a plan,
	// or a single selected option id for a choice.
	Decision string `json:"decision,omitempty"`
	// Options are the selected option ids for a multi-select question.
	Options []string `json:"options,omitempty"`
	// Text is free-text input, a deny reason, or a free-text answer.
	Text string `json:"text,omitempty"`
	// Meta is the adapter's own correlators, copied verbatim from the interaction's Meta by the
	// supervisor and handed back so the adapter can encode the native response without server state
	// (e.g. respondVia, toolUseId).
	Meta map[string]string `json:"meta,omitempty"`
}

// TurnOptions carries per-turn parameters distinct from the agent's sticky settings. It is a value
// type on TurnInput, so existing one-shot callers that leave it zero are unchanged. Two layers:
//
//	(a) Settings — per-turn setting OVERRIDES addressed by CANON (see canon.go). The runner resolves
//	    each canon→the selected agent's field key, drops secrets/unknowns, and overlays them onto the
//	    sticky Config (overrides win). By the time an adapter sees TurnInput, these are already folded
//	    into Config and Options.Settings is cleared — adapters read overrides from Config, never here.
//	(b) Typed structured unified fields the flat string map can't express — system prompt, session
//	    controls (and, from Stage 3, output schema / MCP config / attachments).
type TurnOptions struct {
	// Settings are per-turn overrides addressed by CANONICAL key. Consumed by the runner (the single
	// security choke point); cleared before the adapter sees TurnInput. A client says "reasoningEffort"
	// regardless of which agent runs.
	Settings map[string]string `json:"settings,omitempty"`

	// SystemPrompt fully REPLACES the agent's default system prompt for this turn (distinct from the
	// sticky appendSystemPrompt, which augments it). Per-turn overrides the sticky system-prompt setting.
	SystemPrompt string `json:"systemPrompt,omitempty"`

	// Session controls (precedence resolved by the adapter): SessionID pins an explicit session id for
	// this turn; ContinueLatest resumes the most recent session in the cwd; ForkOnResume branches a new
	// session id from the resumed one instead of continuing it in place.
	SessionID      string `json:"sessionId,omitempty"`
	ContinueLatest bool   `json:"continueLatest,omitempty"`
	ForkOnResume   bool   `json:"forkOnResume,omitempty"`

	// Structured unified fields the flat string map can't express. The runner passes these through
	// untouched; the adapter materializes them to per-turn temp files in RunStream (defer-cleaned).
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"` // JSON Schema for structured output
	MCPServers   json.RawMessage `json:"mcpServers,omitempty"`   // MCP servers config object

	// Subagents defines per-turn subagents in the selected agent's native shape (e.g. Claude's
	// `--agents` inline JSON: {name:{description,prompt,…}}). ClaudeSettings is a per-turn settings
	// bundle in that agent's native format (e.g. Claude's `--settings`, where hooks/permissions/env
	// live). Both are opaque, agent-native passthroughs like MCPServers — an agent that can't honor the
	// shape declares the matching capability false, so api.turn 400s rather than silently dropping.
	Subagents      json.RawMessage `json:"subagents,omitempty"`      // subagent definitions (Claude --agents)
	ClaudeSettings json.RawMessage `json:"claudeSettings,omitempty"` // settings/hooks bundle (Claude --settings)

	Attachments []Attachment `json:"attachments,omitempty"` // files referenced from the message
}

// Attachment is a file made available to a turn. Path-reference only for now: if Path is set the
// adapter references that on-disk file; if Data is set it is written to a per-turn temp file first,
// then referenced. Inline image content blocks require the persistent stream-json transport (a
// follow-on), so this stays a filesystem reference the one-shot CLI can read.
type Attachment struct {
	Name string `json:"name,omitempty"` // display/file name (shown to the agent when referenced)
	Path string `json:"path,omitempty"` // absolute path already on disk (preferred)
	Mime string `json:"mime,omitempty"`
	Data []byte `json:"data,omitempty"` // inline bytes (base64 on the wire); written to a file, then referenced
}

// TurnResult is the final outcome of a turn (the stream carries the detail).
type TurnResult struct {
	Text      string
	SessionID string
	IsError   bool
	// Subtype is the agent's terminal-result classifier when it has one (Claude's result subtype).
	// Empty for agents whose terminal event is always settled (Codex). Continuable(Subtype) reports
	// whether it means "stopped short but resumable" — the signal the resolve loop drives on.
	Subtype string
}

// Message is a chat message in the unified shape served to the client. `Text` stays the joined
// visible text (back-compat: chat titles, old clients). `Parts` is the ordered rich transcript —
// text / thinking / tool — that the app renders as inline cards; omitempty so it's absent for user
// and text-only messages and pre-parts clients simply ignore it.
type Message struct {
	ID        string `json:"id"`
	ChatID    string `json:"chatId"`
	Role      string `json:"role"` // "user" | "assistant"
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
	Parts     []Part `json:"parts,omitempty"`
}

// Part is one ordered piece of an assistant turn. A `tool` part pairs the tool_use and its
// tool_result on one `Tool` payload (keyed by tool-use id) so the wire shape matches the app's
// DaemonMessagePart/DaemonToolCall exactly.
type Part struct {
	Type        string          `json:"type"`                 // "text" | "thinking" | "tool" | "interaction" | "compaction"
	Text        string          `json:"text,omitempty"`       // body for text / thinking
	DurationMs  int             `json:"durationMs,omitempty"` // thinking window, or tool duration
	Tokens      int             `json:"tokens,omitempty"`     // thinking-token estimate (Claude Code's preview counter)
	Tool        *ToolPart       `json:"tool,omitempty"`
	Interaction *Interaction    `json:"interaction,omitempty"` // structured prompt (todos / plan / choice / …)
	Compaction  *CompactionInfo `json:"compaction,omitempty"`  // set on a "compaction" part (a conversation boundary)
	At          string          `json:"at,omitempty"`
}

// ToolPart is a paired tool call (use + result).
type ToolPart struct {
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Output  string          `json:"output,omitempty"`
	IsError bool            `json:"isError,omitempty"`
	// Action is the deep-normalized view of the tool call (same pointer type as ToolEvent.Action). The
	// runner sets it from the tool_use, then overwrites with the completed action (with output) at the
	// tool_result. Best-effort/optional; nil when unclassified. See toolaction.go.
	Action *ToolAction `json:"action,omitempty"`
}

// HistoryQuery locates a chat's native transcript (used when History == Native).
type HistoryQuery struct {
	ChatID    string
	SessionID string
	CWD       string
}

// CredStore persists an agent's credentials/settings (backed by the daemon's state file).
type CredStore interface {
	Get(key string) string
	Set(key, val string) error
	All() map[string]string
}

// Adapter is everything the daemon needs to own ONE agent's lifecycle generically:
// catalog identity, capabilities (the hybrid switch), dynamic settings, auth module,
// install toolchain + verify, a streaming turn, and native history.
type Adapter interface {
	ID() string
	Meta() CatalogEntry
	Capabilities() Capabilities
	Settings() SettingsSchema
	InstallSteps() []Step
	VersionCommand() string
	// ConfigPath is the agent's user-editable config file (absolute), so the app can open it in
	// its editor. Empty if the agent has none.
	ConfigPath() string
	Auth(store CredStore) AuthModule
	RunStream(ctx context.Context, in TurnInput, emit Emit) (TurnResult, error)
	History(q HistoryQuery) ([]Message, error)
	// Notifications declares the conditions this agent produces + their UX.
	Notifications() NotificationSpec
	// Doctor runs agent-specific health checks (CLI installed, toolchain, …) that the
	// daemon's generic doctor appends to its own checks.
	Doctor(ctx context.Context) []Check
}

// Titler is an OPTIONAL adapter capability: the agent owns an auto-generated chat title in its
// native data (e.g. Claude Code writes an `ai-title` record into its transcript). When an adapter
// implements this, the API prefers its title over the daemon's derived first-message snippet, so
// the list shows the agent's own naming. Adapters without it simply keep the derived fallback.
type Titler interface {
	// Title returns the agent's current title for the chat, or "" if none yet (best-effort;
	// any lookup error → "" so the caller keeps its fallback).
	Title(q HistoryQuery) string
}

// HistoryDeleter is an OPTIONAL adapter capability: the agent can delete a chat's native transcript —
// its source of truth on disk (Claude's projects/<slug>/<sid>.jsonl, Codex's rollout file). The API's
// DELETE /chats/{id} type-asserts this and, for every session the chat mapped to, removes the native
// file so a true delete leaves nothing behind. Adapters without it simply skip native deletion (the
// bookkeeping is still purged); a delete of an absent transcript is not an error.
//
// The bool reports whether a transcript ACTUALLY existed and was removed. The caller lists an agent
// under `nativePurged` only when it returns (true, nil); a genuinely-absent transcript is (false, nil)
// and belongs to neither the purged nor the failed list — so a delete never falsely claims to have
// removed a file that was never there.
type HistoryDeleter interface {
	DeleteHistory(q HistoryQuery) (removed bool, err error)
}

var registry = map[string]Adapter{}

// Register adds an adapter (called from each adapter's init()).
func Register(a Adapter) { registry[a.ID()] = a }

// All returns every registered adapter, ordered by ID — the daemon hosts them all and routes
// each request to the one the client selects. Ordering is stable across restarts (map iteration
// is randomized) so the catalog and any default-selection fallback are deterministic.
func All() []Adapter {
	out := make([]Adapter, 0, len(registry))
	for _, a := range registry {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Catalog lists every agent this binary supports (for the app's picker), ID-ordered.
func Catalog() []CatalogEntry {
	all := All()
	out := make([]CatalogEntry, 0, len(all))
	for _, a := range all {
		out = append(out, a.Meta())
	}
	return out
}
