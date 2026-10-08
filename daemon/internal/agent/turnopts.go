package agent

// UnsupportedTurnOption enforces the turn-option capability gate: it reports the first per-turn option
// the agent doesn't support (ok=false + a client-facing message), or ok=true when the request is clean.
// Each option has two client entry points — the typed TurnOptions field and the canon-addressed
// setting — and both are gated so no path silently drops. Mirrors the per-route capability checks
// (Cancel/SetModel/…): an unsupported input is an honest 400, never a no-op.
//
// This is the single source of truth for the gate, shared by the HTTP surface (api.turn) and the
// in-process Go SDK (mindwire.Client.Turn) so neither can drift from the other.
func UnsupportedTurnOption(caps Capabilities, opts TurnOptions) (string, bool) {
	if opts.Command != nil {
		if !caps.Commands {
			return "this harness does not expose native commands", false
		}
		if !ValidCommandName(opts.Command.Name) || len(opts.Command.Arguments) > 64*1024 {
			return "invalid command or arguments", false
		}
		if opts.Voice != nil {
			return "finish voice input before running a command", false
		}
	}
	if opts.Voice != nil {
		if caps.Voice == nil || caps.Voice.Support != SupportNative || !caps.Voice.Remote || caps.Voice.Mode != "conversation" {
			return "This harness does not expose native remote voice conversations.", false
		}
		if len(opts.Voice.ClientID) < 16 || len(opts.Voice.ClientID) > 80 || len(opts.Voice.Voice) > 80 {
			return "A voice client identity is required.", false
		}
		if opts.ForkOnResume || len(opts.Attachments) > 0 {
			return "Send attachments or finish creating the conversation branch before starting live voice.", false
		}
	}
	if opts.ForkOnResume && !caps.Fork {
		return "agent does not support conversation forks", false
	}
	systemPrompt := opts.SystemPrompt != "" || opts.Settings[CanonSystemPrompt] != ""
	if systemPrompt && !caps.SystemPrompt {
		return "agent does not support systemPrompt", false
	}
	if opts.Settings[CanonAppendSystemPrompt] != "" && !caps.AppendSystemPrompt {
		return "agent does not support appendSystemPrompt", false
	}
	if len(opts.MCPServers) > 0 && !caps.MCPServers {
		return "agent does not support mcpServers", false
	}
	if len(opts.Subagents) > 0 && !caps.Subagents {
		return "agent does not support subagents", false
	}
	if len(opts.ClaudeSettings) > 0 && !caps.ClaudeSettings {
		return "agent does not support claudeSettings", false
	}
	return "", true
}
