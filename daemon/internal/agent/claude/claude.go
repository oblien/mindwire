// Package claude is the Claude Code adapter. It declares the agent's capabilities,
// dynamic settings, install toolchain, and runs a turn via `claude -p` with
// structured streaming JSON, mapping the output to unified events. It registers
// itself on import (blank-imported by cmd/daemon).
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/driver"
)

func init() { agent.Register(adapter{}) }

type adapter struct{}

var _ agent.Adapter = adapter{}

func (adapter) ID() string { return "claude-code" }

func (adapter) Meta() agent.CatalogEntry {
	return agent.CatalogEntry{ID: "claude-code", Name: "Claude Code", Tagline: "Anthropic's agentic coding CLI"}
}

func (adapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Protocol:   agent.ProtocolCLI,          // driven via the `claude` CLI (driver.CLI or driver.Persistent)
		Output:     agent.OutputStructuredJSON, // claude emits stream-json
		History:    agent.SupportNative,        // transcript in ~/.claude/projects/.../<sid>.jsonl
		Sessions:   agent.SupportNative,        // --session-id / --resume
		Resume:     true,
		ToolEvents: true,
		Cancel:     true,
		// One-shot per turn by default (bypass mode); a non-bypass turn upgrades to the persistent
		// stdin transport for that turn so it can pause on approvals — see RunStream.
		Persistent: false,
		Models:     true,
		// Image attachments are delivered as true vision content blocks over a one-shot stream-json input
		// transport (the model sees the image), not as path references — see RunStream/materialize.
		ImageInput: true,
		// User-in-loop: claude answers permission asks (control_response), takes follow-up input, and
		// interrupts over the stream-json control protocol on the persistent transport.
		Respond:   true,
		Input:     true,
		Interrupt: true,
		// Runtime control over the same control protocol: switch the model / permission mode of a live
		// (persistent) turn. On a one-shot bypass turn the CLI doesn't read stdin, so the call is a
		// best-effort no-op (still 202) — see the route handlers.
		SetModel:          true,
		SetPermissionMode: true,
		// Turn-option support: claude honors a full system-prompt override (--system-prompt), appending to
		// the default (--append-system-prompt), and per-turn MCP servers (--mcp-config).
		SystemPrompt:       true,
		AppendSystemPrompt: true,
		MCPServers:         true,
		// Per-turn subagents (--agents, inline JSON) and a settings/hooks bundle (--settings, a temp
		// file). Verified on claude-cli 2.1.220: --agents takes inline JSON ({name:{description,prompt}});
		// --settings takes a file path (where hooks/permissions/env live). See buildCommand/materialize.
		Subagents:      true,
		ClaudeSettings: true,
		// Persistent prompt/memory surface (see memory.go): CLAUDE.md at project + user scope, and
		// slash-command templates under .claude/commands (project) / <base>/commands (user).
		Memory:          true,
		PromptTemplates: true,
		// Persistent subagent definitions (see subagents.go): .claude/agents/*.md at project + user scope.
		SubagentDefs: true,
		// Persistent MCP-server config (see mcp.go): the `mcpServers` subtree of .claude.json (user) and
		// .mcp.json (project). Distinct from the per-turn MCPServers overlay above.
		MCPConfig: true,
		// On-demand compaction (see Compact below): the /compact slash command over the one-shot resume path.
		CompactNow: true,
		// Global-resolve: claude's terminal `result` carries a subtype (success / error_max_turns /
		// error_max_budget_usd), and it resumes via --resume — everything the daemon's resolve loop drives on.
		Resolve: true,
	}
}

// optSrc says where a field's option VALUES come from. We never hardcode values:
type optSrc int

const (
	srcNone   optSrc = iota // free text
	srcHelp                 // enum parsed from `claude --help` (e.g. --effort, --permission-mode)
	srcTools                // built-in tool names learned from the system/init event
	srcModels               // model list from the Anthropic Models API (when creds exist)
)

// fieldSpec is a setting we expose. We OWN these references — the key, the `claude`
// flag, the label/help, and the field TYPE. We do NOT own the option values; those are
// discovered (src) from the CLI at runtime.
type fieldSpec struct {
	key, flag, label, help, placeholder, section string
	typ                                          agent.FieldType
	scope                                        agent.Scope // unified | custom (taxonomy)
	canon                                        string      // cross-agent key; "" ⇒ falls back to key (custom)
	src                                          optSrc
	emptyLabel                                   string // label for the "unset" option on a select ("" = none)
	skipFlag                                     bool   // handled specially in buildCommand (permission-mode)
}

// claudeSpecs is the single source of truth for which settings exist and which flag
// each maps to (used by both Settings and buildCommand, so the two can't drift). Every spec
// here maps a Claude flag onto a UNIFIED cross-agent concept (its canon); agent-specific
// props, when added, are Scope custom with canon == key.
var claudeSpecs = []fieldSpec{
	{key: "model", flag: "--model", label: "Model", section: "Model & reasoning", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonModel, src: srcModels, emptyLabel: "Default", placeholder: "opus", help: "Available models for your account (from the Anthropic API); leave at Default to use the CLI's default."},
	{key: "effort", flag: "--effort", label: "Reasoning effort", section: "Model & reasoning", typ: agent.FieldSelect, scope: agent.ScopeUnified, canon: agent.CanonReasoningEffort, src: srcHelp, emptyLabel: "Default", help: "How hard the model thinks per turn."},
	{key: "fallback-model", flag: "--fallback-model", label: "Fallback model", section: "Model & reasoning", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonFallbackModel, placeholder: "sonnet,haiku", help: "Comma-separated; used when the primary is overloaded."},

	{key: "permission-mode", flag: "--permission-mode", label: "Permission mode", section: "Permissions & tools", typ: agent.FieldSelect, scope: agent.ScopeUnified, canon: agent.CanonPermissionMode, src: srcHelp, emptyLabel: "Default (bypass)", skipFlag: true, help: "Non-bypass modes pause for approval (needs the approval flow)."},
	{key: "tools", flag: "--tools", label: "Allowed built-in tools", section: "Permissions & tools", typ: agent.FieldMulti, scope: agent.ScopeUnified, canon: agent.CanonAllowedTools, src: srcTools, help: "Restrict to these built-in tools (none = all)."},
	{key: "allowed-tools", flag: "--allowedTools", label: "Allow rules", section: "Permissions & tools", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonAllowRules, placeholder: "Bash(git *) Edit", help: "Permission allow rules (advanced)."},
	{key: "disallowed-tools", flag: "--disallowedTools", label: "Deny rules", section: "Permissions & tools", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonDenyRules, placeholder: "Bash(rm *)", help: "Permission deny rules (advanced)."},

	{key: "system-prompt", flag: "--system-prompt", label: "System prompt (override)", section: "Prompt & context", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonSystemPrompt, skipFlag: true, placeholder: "You are a senior Go reviewer.", help: "Replaces Claude's default system prompt entirely (advanced). A per-turn systemPrompt option overrides this."},
	{key: "append-system-prompt", flag: "--append-system-prompt", label: "Append system prompt", section: "Prompt & context", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonAppendSystemPrompt, placeholder: "Always write tests.", help: "Appended to Claude's default system prompt."},
	{key: "add-dir", flag: "--add-dir", label: "Extra directory", section: "Prompt & context", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonExtraDirs, placeholder: "/path/to/repo", help: "Additional directory the agent may access."},

	{key: "max-budget-usd", flag: "--max-budget-usd", label: "Max spend (USD)", section: "Limits", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonMaxSpendUSD, placeholder: "5.00", help: "Hard cap on API spend for the turn."},
	{key: "max-turns", flag: "--max-turns", label: "Max turns", section: "Limits", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonMaxTurns, placeholder: "10", help: "Cap the number of agent turns (tool-use cycles) before stopping."},
	{key: "autocompact", flag: "--autocompact", label: "Auto-compact window", section: "Limits", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonAutoCompactTokens, placeholder: "auto", help: "Auto-compact the conversation at this context-window size: 'auto' (let Claude decide) or a token count (100k–1M). Requires Claude Code ≥ 2.1.221."},
}

// Settings builds Claude's settings schema. We own the field references; the VALUES
// (enum choices, tool names) are resolved from the CLI — nothing is hardcoded. A select
// whose CLI source yields no values degrades to a free-text field rather than guessing.
func (adapter) Settings() agent.SettingsSchema {
	var sections []agent.Section
	idx := map[string]int{}
	for _, s := range claudeSpecs {
		// canon defaults to the key so custom fields (canon:"") satisfy the taxonomy invariant.
		canon := s.canon
		if canon == "" {
			canon = s.key
		}
		f := agent.Field{Key: s.key, Label: s.label, Type: s.typ, Scope: s.scope, Canon: canon, Help: s.help, Placeholder: s.placeholder}
		switch s.src {
		case srcHelp:
			if vals := choicesFor(s.flag); len(vals) > 0 {
				f.Type = agent.FieldSelect
				f.Options = options(vals)
				if s.emptyLabel != "" {
					f.Options = append([]agent.Option{{Value: "", Label: s.emptyLabel}}, f.Options...)
				}
			} else {
				f.Type = agent.FieldText // CLI gave no choices → don't guess
			}
		case srcTools:
			if vals := knownTools(); len(vals) > 0 {
				f.Type = agent.FieldMulti
				f.Options = options(vals)
			} else {
				f.Type = agent.FieldText
				f.Help = s.help + " Comma-separated; the picker fills in once the agent reports its tools on the first run."
			}
		case srcModels:
			if vals := knownModels(); len(vals) > 0 {
				f.Type = agent.FieldSelect
				f.Options = []agent.Option{{Value: "", Label: s.emptyLabel}}
				for _, m := range vals {
					f.Options = append(f.Options, agent.Option{Value: m.ID, Label: m.Label})
				}
			} else {
				// No creds yet / offline → don't guess a list; free text, and say why.
				f.Type = agent.FieldText
				f.Help = "Type a model alias or id. Configure auth to pick from your account's models."
			}
		}
		i, ok := idx[s.section]
		if !ok {
			i = len(sections)
			idx[s.section] = i
			sections = append(sections, agent.Section{Title: s.section})
		}
		sections[i].Fields = append(sections[i].Fields, f)
	}
	return agent.SettingsSchema{Sections: sections}
}

func options(vals []string) []agent.Option {
	out := make([]agent.Option, 0, len(vals))
	for _, v := range vals {
		out = append(out, agent.Option{Value: v, Label: v})
	}
	return out
}

func (adapter) InstallSteps() []agent.Step {
	// The CLI ships via npm, so it requires node (a shared catalog tool) first. git comes in
	// as a base requirement. The resolver orders node → Claude Code and dedups shared tools.
	return []agent.Step{{
		Name: "Claude Code", Check: "claude --version",
		Install: "npm i -g @anthropic-ai/claude-code", Requires: []string{"node"},
	}}
}

func (adapter) VersionCommand() string { return "claude --version" }

// configBase is Claude's config directory: CLAUDE_CONFIG_DIR if set, else ~/.claude. Single source of
// truth for ConfigPath and History.
func configBase() string { return agent.ConfigDir("CLAUDE_CONFIG_DIR", ".claude") }

// ConfigPath is the agent's user-editable settings file (absolute), surfaced so the app can open
// it in its code editor. Empty if the home directory can't be resolved.
func (adapter) ConfigPath() string {
	base := configBase()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "settings.json")
}

// Doctor runs Claude-specific health checks the daemon's doctor appends to its generic ones: is the
// CLI installed (and which version), and is Node present (needed to install/update it).
func (adapter) Doctor(ctx context.Context) []agent.Check {
	return agent.CLIDoctor(ctx, "Claude CLI", "claude --version", "@anthropic-ai/claude-code")
}

func (adapter) Auth(store agent.CredStore) agent.AuthModule { return newAuth(store) }

// buildCommand assembles the `claude` invocation for a turn from the message, the applied
// settings (in.Config), and resume state — returned as a shell string (run via bash -lc).
// It iterates the SAME claudeSpecs as Settings, so a key↔flag pair can't drift. Every
// interpolated value is single-quoted. Pure + side-effect-free so it's unit-tested.
// permissionMode is the effective --permission-mode for a turn (default: bypassPermissions).
func permissionMode(in agent.TurnInput) string {
	if m := strings.TrimSpace(in.Config["permission-mode"]); m != "" {
		return m
	}
	return "bypassPermissions"
}

// materialized holds per-turn temp-file paths the adapter creates in RunStream (Stage 3:
// --json-schema, --mcp-config, --settings). Passing resolved paths keeps buildCommand pure/side-effect-free
// and unit-testable; it does no IO of its own. Zero value = no structured files (the Stage 2 hot path).
// (--agents is inline JSON, read from Options in buildCommand, so it needs no path here.)
type materialized struct {
	jsonSchemaPath string
	mcpConfigPath  string
	settingsPath   string // --settings <file> (per-turn hooks/permissions/env bundle)
}

// transport selects how the turn's input reaches the CLI. All three read stream-json OUTPUT; they
// differ in how the message is delivered and whether the bidirectional control channel is armed.
type transport int

const (
	// transportOneShot: the message is a `claude -p <msg>` argument. The hot path — no stdin.
	transportOneShot transport = iota
	// transportStreamInput: `claude -p --input-format stream-json`, the message delivered over stdin as
	// a single user message whose content carries true image content blocks. No control arming (bypass).
	transportStreamInput
	// transportPersistent: stream-json in AND out plus --permission-prompt-tool stdio; the initialize
	// handshake in the preamble arms `can_use_tool` routing so the turn can pause for approvals.
	transportPersistent
)

func buildCommand(in agent.TurnInput, files materialized, tr transport) string {
	// Stream-json input transports (persistent + one-shot-with-images) take NO `-p <msg>` argument — the
	// message arrives over stdin — and add --input-format stream-json. Only the persistent transport adds
	// the permission-prompt tool that arms `can_use_tool` routing. One-shot passes the message as `-p <msg>`.
	var cli string
	switch tr {
	case transportPersistent:
		cli = "claude -p --input-format stream-json" +
			" --output-format stream-json --verbose --include-partial-messages --permission-prompt-tool stdio"
	case transportStreamInput:
		cli = "claude -p --input-format stream-json" +
			" --output-format stream-json --verbose --include-partial-messages"
	default:
		cli = "claude -p " + agent.ShellQuote(in.Message) +
			" --output-format stream-json --verbose --include-partial-messages"
	}

	cli += " --permission-mode " + agent.ShellQuote(permissionMode(in))

	for _, s := range claudeSpecs {
		if s.skipFlag {
			continue // permission-mode / system-prompt handled explicitly below
		}
		if v := strings.TrimSpace(in.Config[s.key]); v != "" {
			cli += " " + s.flag + " " + agent.ShellQuote(v)
		}
	}

	// System-prompt full override: the per-turn Options value wins over the sticky setting; either
	// REPLACES the default prompt (distinct from --append-system-prompt, which augments it).
	sysPrompt := strings.TrimSpace(in.Options.SystemPrompt)
	if sysPrompt == "" {
		sysPrompt = strings.TrimSpace(in.Config["system-prompt"])
	}
	if sysPrompt != "" {
		cli += " --system-prompt " + agent.ShellQuote(sysPrompt)
	}

	// Structured per-turn files (Stage 3), materialized by RunStream and passed in as paths.
	if files.jsonSchemaPath != "" {
		cli += " --json-schema " + agent.ShellQuote(files.jsonSchemaPath)
	}
	if files.mcpConfigPath != "" {
		cli += " --mcp-config " + agent.ShellQuote(files.mcpConfigPath)
	}
	if files.settingsPath != "" {
		cli += " --settings " + agent.ShellQuote(files.settingsPath)
	}
	// Subagents: --agents takes INLINE JSON (verified — a file path is not loaded), so the raw object is
	// single-quoted onto the command directly. These are subagent definitions (descriptions/prompts),
	// never secrets, so the shell-string invariant (no secrets on argv) is preserved.
	if agents := strings.TrimSpace(string(in.Options.Subagents)); agents != "" {
		cli += " --agents " + agent.ShellQuote(agents)
	}

	// Session controls, highest precedence first: an explicit per-turn session id pins the session;
	// ContinueLatest resumes the newest session in the cwd; otherwise the chat's stored session id
	// resumes in place. ForkOnResume branches a new id from whichever base applies.
	resuming := true
	switch {
	case in.Options.SessionID != "":
		cli += " --session-id " + agent.ShellQuote(in.Options.SessionID)
	case in.Options.ContinueLatest:
		cli += " --continue"
	case in.SessionID != "":
		cli += " --resume " + agent.ShellQuote(in.SessionID)
	default:
		resuming = false
	}
	if in.Options.ForkOnResume && resuming {
		cli += " --fork-session"
	}
	return cli
}

// materialize writes a turn's structured options (JSON output schema, MCP config, inline attachment
// blobs) to temp files and returns their paths, any text to append to the message (so path-reference
// attachments are visible to the CLI), the image content blocks to deliver inline over stream-json
// (true vision), and a cleanup func the caller MUST defer. All IO lives here so buildCommand stays
// pure. cleanup is always non-nil (safe to defer even on error).
//
// Attachment routing: an image (by MIME or extension, with readable bytes) becomes a base64 image
// block the model actually SEES; everything else — non-images, and any image whose bytes can't be
// read — becomes a path reference the model can open with its Read tool. So an image is never
// double-delivered, and a turn with image blocks is run on a stream-json input transport by the caller.
func materialize(opts agent.TurnOptions) (files materialized, msgAppend string, imageBlocks []any, cleanup func(), err error) {
	var tmp agent.TempFiles
	cleanup = tmp.Cleanup

	if len(opts.OutputSchema) > 0 {
		p, e := tmp.Write("mindwire-schema-*.json", opts.OutputSchema)
		if e != nil {
			return files, "", nil, cleanup, fmt.Errorf("write output schema: %w", e)
		}
		files.jsonSchemaPath = p
	}
	if len(opts.MCPServers) > 0 {
		p, e := tmp.Write("mindwire-mcp-*.json", opts.MCPServers)
		if e != nil {
			return files, "", nil, cleanup, fmt.Errorf("write mcp config: %w", e)
		}
		files.mcpConfigPath = p
	}
	// Settings bundle (hooks/permissions/env) → --settings <file>. A file (over the inline-JSON form)
	// keeps a potentially large bundle off argv; --settings accepts either.
	if len(opts.ClaudeSettings) > 0 {
		p, e := tmp.Write("mindwire-settings-*.json", opts.ClaudeSettings)
		if e != nil {
			return files, "", nil, cleanup, fmt.Errorf("write settings: %w", e)
		}
		files.settingsPath = p
	}

	// Attachments: images ride as inline vision blocks; anything else is referenced by path (writing
	// inline Data to a temp file first when it has no on-disk Path).
	var refs []string
	for i, at := range opts.Attachments {
		if blk, ok := imageBlock(at); ok {
			imageBlocks = append(imageBlocks, blk)
			continue
		}
		path := at.Path
		if path == "" && len(at.Data) > 0 {
			p, e := tmp.Write("mindwire-attachment-*", at.Data)
			if e != nil {
				return files, "", nil, cleanup, fmt.Errorf("write attachment %d: %w", i, e)
			}
			path = p
		}
		if path == "" {
			continue // nothing to reference (no path, no data)
		}
		if name := strings.TrimSpace(at.Name); name != "" {
			refs = append(refs, fmt.Sprintf("- %s (%s)", name, path))
		} else {
			refs = append(refs, "- "+path)
		}
	}
	if len(refs) > 0 {
		msgAppend = "\n\nAttached files:\n" + strings.Join(refs, "\n")
	}
	return files, msgAppend, imageBlocks, cleanup, nil
}

// RunStream runs one turn with `claude -p … --output-format stream-json` and maps the NDJSON event
// stream to unified events. It's a CLI-protocol agent, so it composes driver.CLI (the universal
// fallback driver): the adapter supplies the command + the stream parser, the driver owns the
// process/stderr/error plumbing. Auth env comes from in.Env; settings from in.Config (buildCommand).
func (adapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	go ensureModels(in.Env) // opportunistic, non-blocking refresh of the model list

	// Materialize structured per-turn options (output schema, MCP config, inline attachments) to temp
	// files, cleaned up when the process exits. buildCommand consumes the resolved paths and stays pure.
	// Image attachments come back as inline vision blocks; non-images are path-referenced in msgAppend.
	files, msgAppend, imageBlocks, cleanup, err := materialize(in.Options)
	defer cleanup()
	if err != nil {
		emit(agent.Event{Type: agent.EventError, Error: err.Error()})
		return agent.TurnResult{Text: err.Error(), IsError: true}, nil
	}
	message := in.Message + msgAppend // path-reference attachments so the CLI can open them

	// Transport selection:
	//   - persistent: a non-bypass permission mode means the turn can PAUSE for the user (approvals), so
	//     it runs on the persistent stdin transport with an inbound channel to pump answers/input/interrupts.
	//     A channel is a precondition (without one there's no way to answer a pause), so a non-bypass turn
	//     lacking it stays one-shot.
	//   - stream-input: a bypass turn carrying image attachments delivers a single stream-json user message
	//     with true image content blocks over stdin (no control arming), then EOF.
	//   - one-shot: the default hot path — the message is a `-p <msg>` argument.
	persistent := in.Inbound != nil && permissionMode(in) != "bypassPermissions"
	tr := transportOneShot
	switch {
	case persistent:
		tr = transportPersistent
	case len(imageBlocks) > 0:
		tr = transportStreamInput
	}

	in.Message = message
	full := buildCommand(in, files, tr)
	if in.CWD != "" {
		full = "cd " + agent.ShellQuote(in.CWD) + " && " + full
	}
	env := map[string]string{}
	for k, v := range in.Env {
		env[k] = v
	}

	if tr == transportPersistent {
		return driver.Persistent{
			Command:  full,
			Env:      env,
			Parse:    parseStream,
			Encode:   encodeInbound,
			Preamble: persistentPreamble(message, imageBlocks),
			Inbound:  in.Inbound,
		}.Run(ctx, in, emit)
	}

	// bypassPermissions is refused when running as root unless IS_SANDBOX=1 marks the environment
	// as an isolated sandbox (which ours is). Only set it for that mode — the prompting modes don't
	// need the root bypass.
	if permissionMode(in) == "bypassPermissions" {
		env["IS_SANDBOX"] = "1"
	}

	if tr == transportStreamInput {
		// One-shot stream-json input: pipe the single user message (text + image blocks) to stdin, then
		// EOF (nil Inbound → the driver closes stdin as soon as the turn's result is seen). No initialize
		// handshake: a bypass turn never pauses, so it doesn't arm the control channel.
		line, _ := json.Marshal(userMessageBlocks(message, imageBlocks))
		return driver.Persistent{
			Command:  full,
			Env:      env,
			Parse:    parseStream,
			Encode:   encodeInbound,
			Preamble: [][]byte{line},
			Inbound:  nil,
		}.Run(ctx, in, emit)
	}

	return driver.CLI{Command: full, Env: env, Parse: parseStream}.Run(ctx, in, emit)
}
