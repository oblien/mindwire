// Package opencode is the SST opencode adapter — mindwire's third agent, and the first driven over a
// native HTTP + SSE API rather than a one-shot CLI. opencode is server-first: its own TUI is a client
// of `opencode serve`, so mindwire embeds it the intended way — spawn a per-turn server, subscribe to
// its `GET /event` SSE bus, create/resume a session, POST the prompt, and normalize the streamed
// message parts into unified events. Interactive tool approvals and interrupt ride the same bus. It
// registers itself on import (blank-imported by cmd/daemon), so adding it is one new package with zero
// core or client changes — the same promise codex proved.
package opencode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func init() { agent.Register(adapter{}) }

type adapter struct{}

var _ agent.Adapter = adapter{}

func (adapter) ID() string { return "opencode" }

func (adapter) Meta() agent.CatalogEntry {
	return agent.CatalogEntry{ID: "opencode", Name: "opencode", Tagline: "SST's open-source coding agent"}
}

func (adapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Protocol: agent.ProtocolHTTP,         // driven over `opencode serve` + the GET /event SSE bus (server.go)
		Output:   agent.OutputStructuredJSON, // message parts arrive as structured JSON over SSE
		// opencode's own store (opencode.db, SQLite) is undocumented and off-limits, so History() returns
		// nil and the core serves mindwire's recorded stream instead.
		History:    agent.SupportEmulated,
		Sessions:   agent.SupportNative, // POST /session; resume by reusing the session id
		Resume:     true,
		ToolEvents: true,
		Cancel:     true,
		// The server is spawned per turn and torn down at turn end — no live process is held ACROSS turns,
		// so from the daemon's view this is one-shot per turn (the same posture as codex's app-server).
		Persistent: false,
		Models:     true, // `opencode models` enumerates provider/model ids (models.go)
		// Image attachments ride opencode's native `file` prompt part as a data: URL, which the server
		// forwards to the model as TRUE vision input (not a path the model must open) — verified accepted
		// by the live server (prompt_async with a file part → HTTP 204). RunStream threads Attachments via
		// attach.go; non-image files are path-referenced in the message (they can't use the vision channel).
		ImageInput: true,
		// User-in-loop over the SSE bus: answer a permission ask (respond) and abort a running turn
		// (interrupt). opencode exposes no mid-turn message injection, live model switch, or live
		// permission-mode switch, so those three stay off and their routes 400.
		Respond:           true,
		Input:             false,
		Interrupt:         true,
		SetModel:          false,
		SetPermissionMode: false,
		// opencode's prompt_async body carries a per-turn `system` override (verified from the generated
		// @opencode-ai/sdk types), so a full system-prompt REPLACE is honored — declared true (api.turn
		// accepts it, and RunStream threads it into the prompt body). appendSystemPrompt has no opencode
		// mechanism; per-turn mcpServers / subagents / a Claude settings bundle are Claude-shaped
		// passthroughs opencode cannot take — each stays false so api.turn returns an honest 400 rather
		// than silently dropping the option.
		SystemPrompt:       true,
		AppendSystemPrompt: false,
		MCPServers:         false,
		Subagents:          false,
		ClaudeSettings:     false,
		// Persistent prompt/memory/subagent surface: AGENTS.md memory at project + user scope (memory.go),
		// saved commands as prompt templates (memory.go), and agent definitions (subagents.go) — all native
		// opencode conventions. The authoritative gate is the module type assertion in the API; these flags
		// are the matching UI hints.
		Memory:          true,
		PromptTemplates: true,
		SubagentDefs:    true,
		// Persistent MCP-server config lives in opencode.json's `mcp` object; mcp.go implements
		// agent.MCPServerModule over it (surgical subtree IO, user scope). The type assertion is the
		// authoritative gate; this flag is the matching UI hint.
		MCPConfig: true,
		// Custom-provider registration writes opencode.json `provider.<id>` (providers.go), the native
		// OpenAI-compatible custom-endpoint mechanism; the module type assertion is the authoritative gate.
		CustomProviders: true,
		// On-demand compaction: POST /session/{id}/summarize {providerID,modelID}. compact.go implements
		// agent.CompactModule over it (the type assertion is the authoritative gate); the boundary surfaces
		// as the session.compacted SSE frame → EventCompaction (trigger "manual"). Route verified present on
		// the live server (summarize → HTTP 500 ProviderModelNotFoundError for a bogus model, NOT a 404).
		CompactNow: true,
		// opencode's session.idle terminal is fully settled (no continuable subtype) and it resumes by
		// reusing the session id, so a resolve run completes in a single iteration — advertise the mode.
		Resolve: true,
	}
}

// Field keys — the single source of truth shared by Settings and RunStream so a user's choice and the
// prompt a turn emits can't drift.
const (
	keyModel        = "model"           // canon model → prompt_async model{providerID,modelID}
	keyAgent        = "agent"           // opencode named agent (build/plan/…) → prompt_async agent
	keyPermission   = "permission-mode" // canon permissionMode: auto (autonomous) | ask (pump approvals)
	keyWorkdir      = "working-dir"     // server cwd (binds the session's project)
	keySystemPrompt = "system-prompt"   // canon systemPrompt → prompt_async system (full override)
)

// Settings builds opencode's settings schema. The model select is discovered from `opencode models`
// and degrades to free text when the CLI enumerated nothing (never hardcode a model list); the rest
// map onto their cross-agent canon or are declared custom.
func (adapter) Settings() agent.SettingsSchema {
	model := withChoices(agent.Field{
		Key: keyModel, Label: "Model", Type: agent.FieldText, Scope: agent.ScopeUnified, Canon: agent.CanonModel,
		Placeholder: "anthropic/claude-sonnet-4",
		Help:        "Model the agent should use, as opencode's provider/model id (e.g. anthropic/claude-sonnet-4). Discovered from `opencode models`; leave blank for the opencode config default.",
	}, modelChoices(nil), "Default (opencode config)")

	agentField := agent.Field{
		Key: keyAgent, Label: "Agent", Type: agent.FieldText, Scope: agent.ScopeCustom, Canon: keyAgent,
		Placeholder: "build",
		Help:        "opencode's named agent for the turn (e.g. build, plan, or a custom one from .opencode/agent). Leave blank for opencode's default agent.",
	}

	perm := agent.Field{
		Key: keyPermission, Label: "Permission mode", Type: agent.FieldSelect, Scope: agent.ScopeUnified, Canon: agent.CanonPermissionMode,
		Default: "auto",
		Options: []agent.Option{
			{Value: "auto", Label: "Auto (autonomous)"},
			{Value: "ask", Label: "Ask (approve tools)"},
		},
		Help: "Auto runs autonomously and the driver approves tool requests for you; Ask pauses on each permission request and surfaces it for you to approve or reject.",
	}

	workdir := agent.Field{
		Key: keyWorkdir, Label: "Working directory", Type: agent.FieldText, Scope: agent.ScopeCustom, Canon: keyWorkdir,
		Placeholder: "/path/to/repo",
		Help:        "Directory the opencode server binds as the session's project root. Defaults to the turn's working directory.",
	}

	sysPrompt := agent.Field{
		Key: keySystemPrompt, Label: "System prompt (override)", Type: agent.FieldText, Scope: agent.ScopeUnified, Canon: agent.CanonSystemPrompt,
		Placeholder: "You are a terse senior engineer.",
		Help:        "Replaces opencode's default instructions for the turn (sent as the prompt's system field). A per-turn systemPrompt option overrides this sticky value.",
	}

	return agent.SettingsSchema{Sections: []agent.Section{
		{Title: "Model & agent", Fields: []agent.Field{model, agentField}},
		{Title: "Permissions", Fields: []agent.Field{perm}},
		{Title: "Workspace", Fields: []agent.Field{workdir}},
		{Title: "Prompt & context", Fields: []agent.Field{sysPrompt}},
	}}
}

// withChoices turns a field into a select over discovered values, prepending an "unset" option; it
// degrades to free text when the CLI enumerated no values (don't guess). Mirrors codex's helper.
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
	// opencode ships its own installer (not npm-first), so there is no node dependency: a single
	// curl-piped install with no Requires.
	return []agent.Step{{
		Name: "opencode", Check: "opencode --version",
		Install: "curl -fsSL https://opencode.ai/install | bash",
	}}
}

func (adapter) VersionCommand() string { return "opencode --version" }

// configDir is opencode's user config home: $XDG_CONFIG_HOME/opencode, else ~/.config/opencode. Single
// source of truth for ConfigPath and the memory/subagent layouts. Empty when the home can't be resolved
// (so callers can omit the user scope rather than target a bogus relative path).
func configDir() string {
	if base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); base != "" {
		return filepath.Join(base, "opencode")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "opencode")
}

// ConfigPath is opencode's user-editable config file (absolute), surfaced so the app can open it.
func (adapter) ConfigPath() string {
	base := configDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "opencode.json")
}

// Doctor is bespoke (NOT agent.CLIDoctor, which appends a Node check): opencode installs via its own
// curl script with no node dependency, so the only check is whether the binary is present.
func (adapter) Doctor(ctx context.Context) []agent.Check {
	if out, err := exec.CommandContext(ctx, "bash", "-lc", "opencode --version").CombinedOutput(); err != nil {
		return []agent.Check{{Name: "opencode", Status: agent.CheckFail,
			Detail: "not installed — run setup (curl -fsSL https://opencode.ai/install | bash)"}}
	} else {
		return []agent.Check{{Name: "opencode", Status: agent.CheckOK, Detail: strings.TrimSpace(string(out))}}
	}
}

func (adapter) Auth(store agent.CredStore) agent.AuthModule { return newAuth(store) }

// permMode is the effective permission mode for a turn (default auto = autonomous, the analogue of
// codex's approvalPolicy=never). "ask" pumps each permission request to the user.
func permMode(in agent.TurnInput) string {
	if v := strings.TrimSpace(in.Config[keyPermission]); v != "" {
		return v
	}
	return "auto"
}

// splitModel splits a "provider/model" setting on the FIRST slash. A value with no slash (or an empty
// side) yields ("",""), so RunStream omits the model from the prompt body and opencode falls back to
// its configured default rather than being handed a malformed selector.
func splitModel(s string) (provider, model string) {
	s = strings.TrimSpace(s)
	i := strings.Index(s, "/")
	if i <= 0 || i == len(s)-1 {
		return "", ""
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
}

// RunStream runs one turn over the opencode server transport. It pre-resolves the turn parameters
// (model split, agent, system prompt, cwd, resume id, and whether to pump approvals) into the server
// struct, which owns the process/HTTP/SSE plumbing (server.go). Auth env comes from in.Env; settings
// from in.Config. Interactive approvals engage only when permission mode is "ask" AND there is an
// inbound channel to answer with — otherwise the driver auto-approves (the autonomous default).
func (adapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	provider, model := splitModel(in.Config[keyModel])
	system := strings.TrimSpace(agent.FirstNonEmpty(in.Options.SystemPrompt, in.Config[keySystemPrompt]))

	workdir := strings.TrimSpace(in.Config[keyWorkdir])
	if workdir == "" {
		workdir = in.CWD
	}

	env := map[string]string{}
	for k, v := range in.Env {
		env[k] = v
	}

	// Attachments: images become native `file` vision parts, non-images are path-referenced in the
	// message (attach.go). cleanup removes any temp files written for inline non-image Data; it must
	// outlive the prompt, so defer it here — Run is synchronous, so the files survive the whole turn.
	extraParts, msgAppend, cleanup, err := resolveAttachments(in.Options.Attachments)
	defer cleanup()
	if err != nil {
		emit(agent.Event{Type: agent.EventError, Error: err.Error()})
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}

	return server{
		env:         env,
		message:     in.Message + msgAppend,
		extraParts:  extraParts,
		provider:    provider,
		model:       model,
		agentName:   strings.TrimSpace(in.Config[keyAgent]),
		system:      system,
		cwd:         workdir,
		resumeID:    agent.FirstNonEmpty(in.Options.SessionID, in.SessionID),
		interactive: permMode(in) == "ask" && in.Inbound != nil,
	}.Run(ctx, in, emit)
}
