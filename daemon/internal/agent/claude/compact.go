package claude

import (
	"context"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

var _ agent.CompactModule = adapter{}

// Compact runs a manual compaction the same way Claude Code itself does interactively: the /compact
// slash command sent as a user message. In 2.1.226 /compact is supportsNonInteractive:true, so the
// ordinary one-shot resume path (claude -p "/compact" --resume <sid> --output-format stream-json
// --verbose, in the chat's cwd) triggers a real compaction — no new transport, no PTY. Optional focus
// instructions ride after the command (/compact <focus>).
//
// We force in.Inbound=nil so RunStream picks the one-shot transport rather than the persistent
// stream-json one: a compaction turn never pauses for approvals or follow-up input. The turn resumes
// in.SessionID, so the CLI compacts THAT conversation in place; it writes an isCompactSummary record
// and streams a {type:"system",subtype:"compact_boundary"} event that parseStream already maps to
// EventCompaction (trigger "manual"). So an on-demand compaction surfaces in the live SSE stream and
// the reloaded history exactly like an auto-compaction the CLI triggered itself.
//
// DISABLE_COMPACT=1 in the CLI's environment turns /compact into a no-op; mindwire never sets it, so a
// compaction request always reaches the compactor.
func (a adapter) Compact(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	cmd := "/compact"
	if focus := strings.TrimSpace(in.Message); focus != "" {
		cmd += " " + focus
	}
	in.Message = cmd
	in.Inbound = nil
	return a.RunStream(ctx, in, emit)
}
