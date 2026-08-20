package agent

// Persistent prompt/memory surface. mindwire already unifies the SESSION prompt (per-turn
// TurnOptions.SystemPrompt) and the STICKY system prompt (a canonical config key). This file adds the
// two PERSISTENT layers so the whole prompt surface is fetch+set across agents:
//
//   - memory files — Claude's CLAUDE.md and Codex's AGENTS.md (this file / memfile.go);
//   - saved prompt templates — Claude slash-commands and Codex saved prompts (prompts.go).
//
// Both are exposed through OPTIONAL adapter capabilities (type-asserted like Titler, NOT bolted onto
// the mandatory Adapter interface), so an agent that has no such artifact simply doesn't implement
// them and the API answers 400. No secret ever flows through here — memory files and templates are
// plain user content, same trust level as a working directory.

// MemoryScope is the canonical, agent-agnostic location of a persistent artifact. It is a DIFFERENT
// axis from Scope (schema.go), which is the settings-field taxonomy (unified|custom) — hence the
// distinct name. A scope answers "whose file is this": tied to a working directory (project) or the
// agent's own config home (user).
type MemoryScope string

const (
	// MemoryProject is an artifact inside a working directory — Claude's CLAUDE.md, Codex's AGENTS.md,
	// a project slash-command — shared with anyone working in that tree.
	MemoryProject MemoryScope = "project"
	// MemoryUser is an artifact in the agent's config home (~/.claude, ~/.codex) — it applies to every
	// project the user runs that agent in.
	MemoryUser MemoryScope = "user"
)

// MemoryDoc is one persistent memory file (Claude's CLAUDE.md / Codex's AGENTS.md) at a canonical
// scope. Path is ALWAYS populated — even when the file is absent — so a client can show where a
// memory would be written; Exists distinguishes an empty file from a missing one.
type MemoryDoc struct {
	Scope   MemoryScope `json:"scope"`
	Path    string      `json:"path"`
	Exists  bool        `json:"exists"`
	Content string      `json:"content"`
}

// MemoryModule is an OPTIONAL adapter capability (type-asserted like Titler, not part of the
// mandatory Adapter interface): read and write the agent's persistent memory file across canonical
// scopes. An adapter that implements it sets Capabilities.Memory=true; the API type-asserts this
// interface as the authoritative gate before serving /memory.
type MemoryModule interface {
	// MemoryScopes lists the scopes this agent's memory supports (project and/or user).
	MemoryScopes() []MemoryScope
	// ReadMemory returns the memory doc for a scope. dir is the resolved project directory (used for
	// project scope; ignored for user scope). A MISSING file is not an error — Exists is false and
	// Content is "".
	ReadMemory(scope MemoryScope, dir string) (MemoryDoc, error)
	// WriteMemory writes content to the scope's memory file (creating the user config home if needed;
	// the project dir must already exist) and returns the resulting doc.
	WriteMemory(scope MemoryScope, dir, content string) (MemoryDoc, error)
	// DeleteMemory removes the scope's memory file and returns the resulting doc (Exists=false).
	// Removing an absent file is not an error (idempotent) — the post-state is "no memory" either way.
	DeleteMemory(scope MemoryScope, dir string) (MemoryDoc, error)
}
