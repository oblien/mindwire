// Package codex is the OpenAI Codex CLI adapter — mindwire's second agent, proving the
// architecture's promise that adding an agent is one new adapter with zero core or client changes.
// Chat turns use `codex app-server` for incremental text, reasoning, tool output and user input.
// Headless callers without an inbound channel retain `codex exec --json`. Both transports map
// into the same shared events. It registers itself on import (blank-imported by cmd/daemon).
package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/driver"
)

func init() { agent.Register(adapter{}) }

type adapter struct{}

var _ agent.Adapter = adapter{}

func (adapter) ID() string { return "codex" }

func (adapter) Meta() agent.CatalogEntry {
	return agent.CatalogEntry{ID: "codex", Name: "Codex CLI", Tagline: "OpenAI's agentic coding CLI"}
}

func (adapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Protocol:   agent.ProtocolPersistent, // chat turns hold an app-server connection for live events and input
		Output:     agent.OutputStructuredJSON,
		History:    agent.SupportNative, // rollout JSONL under $CODEX_HOME/sessions/**
		Sessions:   agent.SupportNative, // `codex exec resume <id>` / `--last`
		Resume:     true,
		ToolEvents: true,
		Cancel:     true,
		Persistent: true,
		// Codex ships no scriptable model list of its own and the daemon no longer stores the models.dev
		// catalog, so /models returns an empty native list; Codex DECLARES its provider scope (openai) and
		// the client sources the picker from the live catalog. The settings model field is free text (see
		// models.go). Models stays true so the client shows a Models surface for Codex.
		Models: true,
		// Codex feeds image attachments to `codex exec -i <file>` natively, so the model sees the image.
		ImageInput: true,
		// User-in-loop over the app-server transport: answer approvals (respond), steer mid-turn (input),
		// and interrupt. Codex has no live model/permission switch, so those two stay off (their routes 400).
		Respond:           true,
		Input:             true,
		Interrupt:         true,
		SetModel:          false,
		SetPermissionMode: false,
		// App-server carries full system instructions and MCP config in thread RPCs; exec uses a
		// temporary profile overlay. Both keep prompt/MCP credentials off the command line.
		SystemPrompt:       true,
		AppendSystemPrompt: false,
		MCPServers:         true,
		// Subagents (--agents) and a settings/hooks bundle (--settings) are Claude-native shapes with no
		// codex equivalent, so both stay false and api.turn returns an honest 400 rather than dropping.
		Subagents:      false,
		ClaudeSettings: false,
		// Persistent prompt/memory surface (see memory.go): AGENTS.md at project + user scope, and saved
		// prompts under <base>/prompts (user only — Codex has no project-scope saved-prompt convention).
		Memory:          true,
		PromptTemplates: true,
		// Persistent MCP-server config (see mcp.go): [mcp_servers.*] tables in config.toml, user scope
		// only. Distinct from the per-turn MCPServers overlay above.
		MCPConfig: true,
		// Custom-provider registration (see providers.go): [model_providers.<id>] tables in config.toml,
		// user scope only. The module type assertion is the authoritative gate.
		CustomProviders: true,
		// On-demand compaction uses the same connection settings and thread/compact/start RPC.
		CompactNow: true,
		// Global-resolve: codex's turn.completed is always fully settled (no subtype), and it resumes via
		// `codex exec resume` — so a resolve run completes in a single iteration (one probe elicits the
		// sentinel), which is correct by construction. The flag advertises the mode as available.
		Resolve: true,
	}
}

// Field keys. These are the single source of truth shared by codexSpecs (Settings) and
// buildExecCommand, so the settings a user picks and the flags a turn emits can't drift. Codex's
// flag mapping is too irregular to iterate (fresh vs resume take different flags for the same
// concept), so buildExecCommand switches on these keys explicitly rather than a per-spec flag.
const (
	keyModel        = "model"
	keyEffort       = "reasoning-effort"
	keyApproval     = "permission-mode" // maps to Codex's approval policy (when to ask a human)
	keySandbox      = "sandbox"         // Codex's orthogonal second axis (filesystem/network isolation)
	keyWorkdir      = "working-dir"
	keyAddDir       = "add-dir"
	keySystemPrompt = "system-prompt"       // sticky full system-prompt override (canon systemPrompt), threaded into the exec overlay
	keyAutoCompact  = "auto-compact-tokens" // canon autoCompactTokens → -c model_auto_compact_token_limit
)

// optSrc says where a select field's VALUES come from. We never hardcode enum values — they are
// discovered from `codex --help` / `codex exec --help` and degrade to free text when absent.
type optSrc int

const (
	srcNone     optSrc = iota // free text (no discoverable source)
	srcSandbox                // inline `[possible values: …]` from `codex exec --help` (-s/--sandbox)
	srcApproval               // multi-line `Possible values:` from `codex --help` (-a/--ask-for-approval)
	srcModels                 // model ids from modelChoices() — empty for Codex (no local list → free text)
)

// fieldSpec is a setting we expose. We OWN the references (key, label/help, field TYPE, scope,
// canon); the option VALUES are discovered from the CLI at runtime (src).
type fieldSpec struct {
	key, label, help, placeholder, section string
	typ                                    agent.FieldType
	scope                                  agent.Scope
	canon                                  string // cross-agent key; "" ⇒ falls back to key (custom)
	src                                    optSrc
	emptyLabel                             string // label for the "unset" option on a select
}

// codexSpecs is the single source of truth for which settings exist. Every unified spec maps a Codex
// surface onto a cross-agent canon; Codex-specific axes (sandbox, working directory) are custom
// (scope custom, canon == key).
var codexSpecs = []fieldSpec{
	{key: keyModel, label: "Model", section: "Model & reasoning", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonModel, src: srcModels, emptyLabel: "Default (CLI default)", placeholder: "gpt-5.5", help: "Model the agent should use; leave blank for the CLI default. Free text here (CLI-validated); browse and pick the OpenAI model list in the client's Models surface, sourced from the live models.dev catalog."},
	{key: keyEffort, label: "Reasoning effort", section: "Model & reasoning", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonReasoningEffort, src: srcNone, placeholder: "medium", help: "How much reasoning the model uses per turn (e.g. minimal, low, medium, high, xhigh), depending on the selected model."},

	{key: keyApproval, label: "Approval policy", section: "Permissions & sandbox", typ: agent.FieldSelect, scope: agent.ScopeUnified, canon: agent.CanonPermissionMode, src: srcApproval, emptyLabel: "Never (autonomous)", help: "When Codex pauses to ask you before running a command."},
	{key: keySandbox, label: "Sandbox", section: "Permissions & sandbox", typ: agent.FieldSelect, scope: agent.ScopeCustom, canon: keySandbox, src: srcSandbox, emptyLabel: "Default (workspace-write)", help: "Filesystem/network isolation for model-run commands — Codex's second permission axis, orthogonal to the approval policy."},

	{key: keyWorkdir, label: "Working directory", section: "Workspace", typ: agent.FieldText, scope: agent.ScopeCustom, canon: keyWorkdir, src: srcNone, placeholder: "/path/to/repo", help: "Directory the agent uses as its working root. Applies to fresh sessions; a resumed session keeps its original directory."},
	{key: keyAddDir, label: "Extra directory", section: "Workspace", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonExtraDirs, src: srcNone, placeholder: "/path/to/other", help: "Additional directory that should be writable alongside the primary workspace."},

	{key: keySystemPrompt, label: "System prompt (override)", section: "Prompt & context", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonSystemPrompt, src: srcNone, placeholder: "You are a terse senior engineer.", help: "Replaces Codex's default instructions entirely. A per-turn systemPrompt option overrides this."},

	{key: keyAutoCompact, label: "Auto-compact window", section: "Limits", typ: agent.FieldText, scope: agent.ScopeUnified, canon: agent.CanonAutoCompactTokens, src: srcNone, placeholder: "200000", help: "Auto-compact the conversation once its context reaches this many tokens. Enter a positive integer; non-numeric values are ignored. Codex caps the effective limit at about 90% of the model's context window."},
}

// Settings builds Codex's settings schema. We own the field references; a select whose CLI source
// yields no values degrades to free text rather than guessing (so nothing is hardcoded).
func (adapter) Settings() agent.SettingsSchema {
	var sections []agent.Section
	idx := map[string]int{}
	for _, s := range codexSpecs {
		canon := s.canon
		if canon == "" {
			canon = s.key
		}
		f := agent.Field{Key: s.key, Label: s.label, Type: s.typ, Scope: s.scope, Canon: canon, Help: s.help, Placeholder: s.placeholder}
		switch s.src {
		case srcSandbox:
			f = withChoices(f, sandboxChoices(), s.emptyLabel)
		case srcApproval:
			f = withChoices(f, approvalChoices(), s.emptyLabel)
		case srcModels:
			f = withChoices(f, modelChoices(), s.emptyLabel)
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

// withChoices turns a field into a select over discovered values, prepending an "unset" option; it
// degrades to free text when the CLI enumerated no values (don't guess).
func withChoices(f agent.Field, vals []string, emptyLabel string) agent.Field {
	if len(vals) == 0 {
		f.Type = agent.FieldText
		return f
	}
	f.Type = agent.FieldSelect
	opts := make([]agent.Option, 0, len(vals)+1)
	if emptyLabel != "" {
		opts = append(opts, agent.Option{Value: "", Label: emptyLabel})
	}
	for _, v := range vals {
		opts = append(opts, agent.Option{Value: v, Label: v})
	}
	f.Options = opts
	return f
}

func (adapter) InstallSteps() []agent.Step {
	// The CLI ships via npm, so it requires node (a shared catalog tool) first. The resolver orders
	// node → Codex CLI and dedups shared tools.
	return []agent.Step{{
		Name: "Codex CLI", Check: "codex --version",
		Install: "npm i -g @openai/codex", Requires: []string{"node"},
	}}
}

func (adapter) VersionCommand() string { return "codex --version" }

// configBase is Codex's state directory: CODEX_HOME if set, else ~/.codex. Single source of truth for
// ConfigPath and History.
func configBase() string { return agent.ConfigDir("CODEX_HOME", ".codex") }

// ConfigPath is the agent's user-editable config file (absolute), surfaced so the app can open it in
// its code editor. Empty if the home directory can't be resolved.
func (adapter) ConfigPath() string {
	base := configBase()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "config.toml")
}

// Doctor runs Codex-specific health checks the daemon's doctor appends to its generic ones: is the
// CLI installed (and which version), and is Node present (needed to install/update it).
func (adapter) Doctor(ctx context.Context) []agent.Check {
	return agent.CLIDoctor(ctx, "Codex CLI", "codex --version", "@openai/codex")
}

func (adapter) Auth(store agent.CredStore) agent.AuthModule { return newAuth(store) }

// approvalPolicy is the effective Codex approval policy (default: never = autonomous).
// Other policies let a turn pause for human approval over the same live connection.
func approvalPolicy(in agent.TurnInput) string {
	if v := strings.TrimSpace(in.Config[keyApproval]); v != "" {
		return v
	}
	return "never"
}

// sandbox is the effective sandbox policy for a turn (default: workspace-write, so a turn can edit
// the workspace without an approval round-trip — the autonomous posture).
func sandbox(in agent.TurnInput) string {
	if v := strings.TrimSpace(in.Config[keySandbox]); v != "" {
		return v
	}
	return "workspace-write"
}

// materialized holds per-turn temp-file paths and resolved attachment references the adapter creates
// in RunStream. Passing resolved paths keeps buildExecCommand pure/side-effect-free and unit-testable.
type materialized struct {
	outputSchemaPath string   // --output-schema <path> (from Options.OutputSchema)
	imagePaths       []string // -i <path> per image attachment
	configProfile    string   // -p <profile>: per-run config overlay carrying systemPrompt/mcpServers
	systemPrompt     string
	mcpServers       map[string]codexMCPServer
}

// buildExecCommand assembles the `codex exec` invocation for a turn from the message, applied
// settings (in.Config), resume state, and materialized files — returned as a shell string (run via
// bash -lc). Pure + side-effect-free so it's unit-tested. Codex splits the same concept across
// different flags on a fresh vs resumed session (resume rejects -s/-C/--add-dir), so those go through
// `-c` config overrides on resume; buildExecCommand encodes that fresh-vs-resume branch.
func buildExecCommand(in agent.TurnInput, files materialized) string {
	// Resume when we have any session anchor: an explicit per-turn id, the chat's stored id, or a
	// "continue latest" request. Fork-on-resume has no Codex equivalent (ids are auto-generated), so
	// it's a documented no-op here.
	resuming := in.Options.SessionID != "" || in.SessionID != "" || in.Options.ContinueLatest

	cli := "codex exec"
	// Per-run config overlay (systemPrompt/mcpServers). `-p` must precede the `resume` subcommand —
	// codex rejects it after `resume` — so it is emitted first, before the fresh-vs-resume branch.
	if files.configProfile != "" {
		cli += " -p " + agent.ShellQuote(files.configProfile)
	}
	if resuming {
		cli += " resume"
		// Precedence: an explicit per-turn id > "continue latest" > the chat's stored resume id.
		switch {
		case in.Options.SessionID != "":
			cli += " " + agent.ShellQuote(in.Options.SessionID)
		case in.Options.ContinueLatest:
			cli += " --last"
		case in.SessionID != "":
			cli += " " + agent.ShellQuote(in.SessionID)
		}
	}
	// --json for the structured stream; --skip-git-repo-check because the sandbox cwd may not be a repo.
	cli += " --json --skip-git-repo-check"

	// Keep every -c override in the same command scope. Codex replaces pre-resume -c values
	// when resume has its own -c flags; losing this one switches the endpoint and drops its auth.
	if provider := strings.TrimSpace(in.Env[azureProviderMarker]); provider != "" {
		cli += " -c " + agent.ShellQuote("model_provider="+provider)
	}

	// Model (fresh + resume both accept -m).
	if v := agent.FirstNonEmpty(in.Config[keyModel], in.Env[azureModelMarker]); strings.TrimSpace(v) != "" {
		cli += " -m " + agent.ShellQuote(v)
	}
	// Reasoning effort — config-only (no CLI flag), fresh + resume.
	if v := strings.TrimSpace(in.Config[keyEffort]); v != "" {
		cli += " -c " + agent.ShellQuote("model_reasoning_effort="+v)
	}
	// Auto-compact threshold — config-only, fresh + resume. Codex expects an integer token limit, so a
	// non-numeric unified value (e.g. Claude's "auto") is skipped rather than passed through and rejected.
	if v := strings.TrimSpace(in.Config[keyAutoCompact]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cli += " -c " + agent.ShellQuote("model_auto_compact_token_limit="+strconv.Itoa(n))
		}
	}
	// Approval policy — always emitted (default never = the autonomous exec posture). exec has no -a
	// flag, so it's a config override on both fresh and resume.
	cli += " -c " + agent.ShellQuote("approval_policy="+approvalPolicy(in))

	// Sandbox posture — always emitted (default workspace-write). Fresh takes -s; resume rejects it, so
	// it goes through a config override.
	sb := sandbox(in)
	if resuming {
		cli += " -c " + agent.ShellQuote("sandbox_mode="+sb)
	} else {
		cli += " -s " + agent.ShellQuote(sb)
	}

	// Working directory — fresh only (-C); a resumed session keeps its recorded working root.
	if v := strings.TrimSpace(in.Config[keyWorkdir]); v != "" && !resuming {
		cli += " -C " + agent.ShellQuote(v)
	}

	// Extra writable directory. Fresh: --add-dir; resume rejects it, so a writable-roots config override.
	if v := strings.TrimSpace(in.Config[keyAddDir]); v != "" {
		if resuming {
			cli += " -c " + agent.ShellQuote(`sandbox_workspace_write.writable_roots=["`+v+`"]`)
		} else {
			cli += " --add-dir " + agent.ShellQuote(v)
		}
	}

	// Structured final output (fresh + resume).
	if files.outputSchemaPath != "" {
		cli += " --output-schema " + agent.ShellQuote(files.outputSchemaPath)
	}
	// Image attachments (fresh + resume).
	for _, img := range files.imagePaths {
		cli += " -i " + agent.ShellQuote(img)
	}

	// Prompt argument last, if non-empty (a resume with no new prompt is valid).
	if strings.TrimSpace(in.Message) != "" {
		cli += " " + agent.ShellQuote(in.Message)
	}
	return cli
}

// materialize writes a turn's structured options (JSON output schema) to a temp file and resolves
// attachment references: image attachments become -i paths, other files are referenced by path in an
// appended message block (so the CLI can open them). All IO lives here so buildExecCommand stays pure.
// It takes the whole TurnInput (not just Options) so the sticky `system-prompt` config key can back the
// per-turn override — the per-turn Options.SystemPrompt still wins. cleanup is always non-nil (safe to
// defer even on error).
func materialize(in agent.TurnInput) (files materialized, msgAppend string, cleanup func(), err error) {
	opts := in.Options
	var tmp agent.TempFiles
	var overlayPath string // profile overlay in the real CODEX_HOME; removed by cleanup
	cleanup = func() {
		tmp.Cleanup()
		if overlayPath != "" {
			_ = os.Remove(overlayPath)
		}
	}

	if len(opts.OutputSchema) > 0 && in.Inbound == nil {
		p, e := tmp.Write("mindwire-codex-schema-*.json", opts.OutputSchema)
		if e != nil {
			return files, "", cleanup, fmt.Errorf("write output schema: %w", e)
		}
		files.outputSchemaPath = p
	}

	// Resolve prompt/MCP options once: app-server sends private RPC values, exec materializes a
	// temporary profile. The per-turn system prompt takes precedence over the sticky setting.
	if sp := agent.FirstNonEmpty(opts.SystemPrompt, in.Config[keySystemPrompt]); strings.TrimSpace(sp) != "" || len(opts.MCPServers) > 0 {
		sp = strings.TrimSpace(sp)
		servers, e := decodeMCPServers(opts.MCPServers)
		if e != nil {
			return files, "", cleanup, fmt.Errorf("parse mcpServers: %w", e)
		}
		files.systemPrompt, files.mcpServers = sp, servers
		// Live turns send these values directly over the private app-server pipe.
		if in.Inbound == nil {
			base := configBase()
			if base == "" {
				return files, "", cleanup, fmt.Errorf("cannot honor systemPrompt/mcpServers: CODEX_HOME is not resolvable")
			}
			var sysPromptPath string
			if sp != "" {
				p, e := tmp.Write("mindwire-codex-sysprompt-*.md", []byte(sp))
				if e != nil {
					return files, "", cleanup, fmt.Errorf("write system prompt: %w", e)
				}
				sysPromptPath = p
			}
			profile, path, e := writeConfigOverlay(base, sysPromptPath, servers)
			if e != nil {
				return files, "", cleanup, fmt.Errorf("write codex config overlay: %w", e)
			}
			files.configProfile, overlayPath = profile, path
		}
	}

	var refs []string
	for i, at := range opts.Attachments {
		path := at.Path
		if path == "" && len(at.Data) > 0 {
			p, e := tmp.Write("mindwire-codex-attachment-*", at.Data)
			if e != nil {
				return files, "", cleanup, fmt.Errorf("write attachment %d: %w", i, e)
			}
			path = p
		}
		if path == "" {
			continue // nothing to reference (no path, no data)
		}
		if isImage(at) {
			files.imagePaths = append(files.imagePaths, path) // -i understands images natively
		} else if name := strings.TrimSpace(at.Name); name != "" {
			refs = append(refs, fmt.Sprintf("- %s (%s)", name, path))
		} else {
			refs = append(refs, "- "+path)
		}
	}
	if len(refs) > 0 {
		msgAppend = "\n\nAttached files:\n" + strings.Join(refs, "\n")
	}
	return files, msgAppend, cleanup, nil
}

// isImage reports whether an attachment should be passed as a Codex `-i` image (by mime or extension).
func isImage(at agent.Attachment) bool {
	if strings.HasPrefix(strings.ToLower(at.Mime), "image/") {
		return true
	}
	for _, name := range []string{at.Path, at.Name} {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
			return true
		}
	}
	return false
}

// RunStream uses app-server for every chat turn, including autonomous approvalPolicy=never turns.
// exec --json is retained for callers without an inbound channel; it may emit only completed blocks.
func (adapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	files, msgAppend, cleanup, err := materialize(in)
	defer cleanup()
	if err != nil {
		emit(agent.Event{Type: agent.EventError, Error: err.Error()})
		return agent.TurnResult{Text: err.Error(), IsError: true}, nil
	}
	in.Message += msgAppend // path-reference non-image attachments so the CLI can open them

	if in.Inbound != nil {
		return newAppServer(in, files).Run(ctx, in, emit)
	}

	env := map[string]string{}
	for k, v := range in.Env {
		env[k] = v
	}
	// These are daemon routing hints, not credentials or variables the Codex child needs to inherit.
	delete(env, azureProviderMarker)
	delete(env, azureModelMarker)

	full := buildExecCommand(in, files)
	if in.CWD != "" {
		full = "cd " + agent.ShellQuote(in.CWD) + " && " + full
	}
	return driver.CLI{Command: full, Env: env, Parse: parseStream}.Run(ctx, in, emit)
}
