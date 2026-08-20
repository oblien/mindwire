package api

import (
	"encoding/json"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	// Registered for their init() so the test gates against the REAL declared capabilities,
	// not a hand-copied literal that could drift from the adapters.
	_ "github.com/oblien/mindwire/daemon/internal/agent/claude"
	_ "github.com/oblien/mindwire/daemon/internal/agent/codex"
	_ "github.com/oblien/mindwire/daemon/internal/agent/opencode"
)

// capsByID returns a registered adapter's declared capabilities (fatal if it isn't registered).
func capsByID(t *testing.T, id string) agent.Capabilities {
	t.Helper()
	for _, a := range agent.All() {
		if a.ID() == id {
			return a.Capabilities()
		}
	}
	t.Fatalf("adapter %q not registered", id)
	return agent.Capabilities{}
}

// TestTurnOptionGate is W1's honest-signaling regression, updated for W4b. api.turn must reject a turn
// carrying an option the target adapter does not support (a 400, surfaced here as agent.UnsupportedTurnOption
// ok=false) rather than silently dropping it. Post-W4b codex supports systemPrompt (model_instructions_file)
// and mcpServers (a `[mcp_servers.NAME]` profile overlay) but NOT appendSystemPrompt (no clean codex
// mechanism), so only the append case 400s for codex. claude declares all three, so every case passes.
// Both client entry points (the typed TurnOptions field and the canon-addressed setting) are exercised.
func TestTurnOptionGate(t *testing.T) {
	claude := capsByID(t, "claude-code")
	codex := capsByID(t, "codex")
	opencode := capsByID(t, "opencode")

	// Guard the declarations themselves so a future edit to any adapter can't quietly desync the gate.
	if !(claude.SystemPrompt && claude.AppendSystemPrompt && claude.MCPServers && claude.Subagents && claude.ClaudeSettings) {
		t.Fatalf("claude must support all five turn options, got %+v", claude)
	}
	if !codex.SystemPrompt || codex.AppendSystemPrompt || !codex.MCPServers || codex.Subagents || codex.ClaudeSettings {
		t.Fatalf("codex must support systemPrompt+mcpServers only (not appendSystemPrompt/subagents/claudeSettings), got %+v", codex)
	}
	// opencode's prompt body carries a per-turn system override, so systemPrompt is accepted; the four
	// Claude-shaped passthroughs have no opencode wire path and must each 400.
	if !opencode.SystemPrompt || opencode.AppendSystemPrompt || opencode.MCPServers || opencode.Subagents || opencode.ClaudeSettings {
		t.Fatalf("opencode must support systemPrompt only (not append/mcp/subagents/claudeSettings), got %+v", opencode)
	}

	tests := []struct {
		name        string
		opts        agent.TurnOptions
		codexMsg    string // codex's expected 400 message ("" ⇒ codex accepts)
		opencodeMsg string // opencode's expected 400 message ("" ⇒ opencode accepts)
	}{
		{"empty", agent.TurnOptions{}, "", ""},
		{"systemPrompt typed", agent.TurnOptions{SystemPrompt: "You are a bot."}, "", ""},
		{"systemPrompt canon", agent.TurnOptions{Settings: map[string]string{agent.CanonSystemPrompt: "You are a bot."}}, "", ""},
		{"appendSystemPrompt canon", agent.TurnOptions{Settings: map[string]string{agent.CanonAppendSystemPrompt: "Be terse."}}, "agent does not support appendSystemPrompt", "agent does not support appendSystemPrompt"},
		{"mcpServers", agent.TurnOptions{MCPServers: json.RawMessage(`{"srv":{"command":"x"}}`)}, "", "agent does not support mcpServers"},
		{"subagents", agent.TurnOptions{Subagents: json.RawMessage(`{"reviewer":{"description":"r","prompt":"p"}}`)}, "agent does not support subagents", "agent does not support subagents"},
		{"claudeSettings", agent.TurnOptions{ClaudeSettings: json.RawMessage(`{"hooks":{}}`)}, "agent does not support claudeSettings", "agent does not support claudeSettings"},
		// An unrelated setting is never gated by these option checks.
		{"other setting", agent.TurnOptions{Settings: map[string]string{agent.CanonModel: "opus"}}, "", ""},
	}

	for _, tc := range tests {
		t.Run("codex/"+tc.name, func(t *testing.T) {
			msg, ok := agent.UnsupportedTurnOption(codex, tc.opts)
			if tc.codexMsg == "" {
				if !ok {
					t.Fatalf("codex should accept %s, got 400 %q", tc.name, msg)
				}
				return
			}
			if ok || msg != tc.codexMsg {
				t.Fatalf("codex %s: want 400 %q, got ok=%v msg=%q", tc.name, tc.codexMsg, ok, msg)
			}
		})
		t.Run("opencode/"+tc.name, func(t *testing.T) {
			msg, ok := agent.UnsupportedTurnOption(opencode, tc.opts)
			if tc.opencodeMsg == "" {
				if !ok {
					t.Fatalf("opencode should accept %s, got 400 %q", tc.name, msg)
				}
				return
			}
			if ok || msg != tc.opencodeMsg {
				t.Fatalf("opencode %s: want 400 %q, got ok=%v msg=%q", tc.name, tc.opencodeMsg, ok, msg)
			}
		})
		t.Run("claude/"+tc.name, func(t *testing.T) {
			// claude supports all three, so every case (including the option-bearing ones) passes.
			if msg, ok := agent.UnsupportedTurnOption(claude, tc.opts); !ok {
				t.Fatalf("claude should accept %s, got 400 %q", tc.name, msg)
			}
		})
	}
}
