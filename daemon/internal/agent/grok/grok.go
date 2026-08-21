// Package grok drives xAI's open-source Grok Build CLI in its documented
// headless mode. It deliberately invokes the native `grok` process rather than
// calling the xAI model API: sessions, workspace access, and tool execution stay
// owned by Grok Build.
package grok

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

func (adapter) ID() string { return "grok" }

func (adapter) Meta() agent.CatalogEntry {
	return agent.CatalogEntry{ID: "grok", Name: "Grok Build", Tagline: "xAI's open-source coding agent"}
}

func (adapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Protocol: agent.ProtocolCLI, Output: agent.OutputStructuredJSON,
		// Grok persists native sessions. ACP provides the protocol-native session,
		// structured tool-update, cancellation, and permission surfaces.
		History: agent.SupportEmulated, Sessions: agent.SupportNative, Resume: true,
		Cancel: true, Persistent: false, Models: true, ToolEvents: true,
		// ACP can answer native permission requests and cancel a live prompt. It
		// does not document live prompt injection or runtime setting switches.
		Respond: true, Input: false, Interrupt: true, SetModel: false, SetPermissionMode: false,
		// Context options are valid when session/new is used; resumed sessions are
		// rejected explicitly by validateACPInput rather than silently ignoring them.
		SystemPrompt: true, AppendSystemPrompt: true, MCPServers: true,
		Subagents: false, ClaudeSettings: false,
		MCPConfig: true, Memory: true, PromptTemplates: true,
		// Resolve is daemon-managed and only requires native resume, which ACP
		// supplies through session/load and session/resume.
		Resolve: true,
	}
}

const (
	keyModel        = "model"
	keyRules        = "rules"
	keySystemPrompt = "system-prompt"
	keyPermission   = "permission-mode"
)

func (adapter) Settings() agent.SettingsSchema {
	return agent.SettingsSchema{Sections: []agent.Section{
		{Title: "Model", Fields: []agent.Field{
			{Key: keyModel, Label: "Model", Type: agent.FieldText, Scope: agent.ScopeUnified, Canon: agent.CanonModel, Placeholder: "grok-build", Help: "Grok Build model ID. Leave blank for the CLI default."},
		}},
		{Title: "Prompt & context", Fields: []agent.Field{
			{Key: keySystemPrompt, Label: "System prompt (override)", Type: agent.FieldText, Scope: agent.ScopeUnified, Canon: agent.CanonSystemPrompt, Placeholder: "You are a careful Go reviewer.", Help: "Replaces Grok Build's system prompt for this turn."},
			{Key: keyRules, Label: "Extra rules", Type: agent.FieldText, Scope: agent.ScopeUnified, Canon: agent.CanonAppendSystemPrompt, Placeholder: "Always add tests.", Help: "Appended to Grok Build's instructions for this turn."},
		}},
		{Title: "Execution", Fields: []agent.Field{
			{Key: keyPermission, Label: "Permission mode", Type: agent.FieldSelect, Scope: agent.ScopeUnified, Canon: agent.CanonPermissionMode, Default: "always-approve", Options: []agent.Option{{Value: "always-approve", Label: "Always approve"}, {Value: "ask", Label: "Ask before tools"}}, Help: "Ask routes Grok Build's native ACP permission requests to the MindWire interaction flow."},
		}},
	}}
}

func (adapter) InstallSteps() []agent.Step {
	return []agent.Step{{Name: "Grok Build", Check: "grok version", Install: "npm i -g @xai-official/grok", Requires: []string{"node"}}}
}

func (adapter) VersionCommand() string { return "grok version" }

func (adapter) ConfigPath() string {
	base := configBase()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "config.toml")
}

func configBase() string {
	if base := strings.TrimSpace(os.Getenv("GROK_HOME")); base != "" {
		return base
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".grok")
}

func (adapter) Doctor(ctx context.Context) []agent.Check {
	out, err := exec.CommandContext(ctx, "bash", "-lc", "grok version").CombinedOutput()
	if err != nil {
		return []agent.Check{{Name: "Grok Build", Status: agent.CheckFail, Detail: "not installed — run setup (npm i -g @xai-official/grok)"}}
	}
	return []agent.Check{{Name: "Grok Build", Status: agent.CheckOK, Detail: strings.TrimSpace(string(out))}}
}

func (adapter) Auth(store agent.CredStore) agent.AuthModule { return newAuth(store) }

func (adapter) Notifications() agent.NotificationSpec {
	return agent.NotificationSpec{Conditions: []agent.ConditionUX{
		{Condition: agent.Finished, Title: "Grok Build finished"},
		{Condition: agent.Errored, Title: "Grok Build hit an error"},
	}}
}

func (adapter) History(agent.HistoryQuery) ([]agent.Message, error) { return nil, nil }

func (adapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	return runACP(ctx, in, emit)
}
