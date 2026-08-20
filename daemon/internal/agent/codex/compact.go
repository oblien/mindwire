package codex

import (
	"context"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

var _ agent.CompactModule = adapter{}

// Compact runs an on-demand compaction over the app-server transport: resume the thread, then send
// thread/compact/start {threadId}. The exec hot path (`codex exec --json`) cannot trigger compaction
// (it's observe-only), so a compaction always routes through the app-server sibling — the same
// persistent transport an interactive-approval turn uses (see appserver.go), driven here with
// compact=true so converse sends the compact RPC instead of turn/start.
//
// in.SessionID is the thread to compact; it must be non-empty (the API rejects a chat with no session,
// and Codex has no `--last` equivalent over app-server — a fresh thread has nothing to compact). A
// compaction never executes tools, so we force approvalPolicy "never": there is no ingress channel on a
// compaction run, so an approval prompt would only hang until the turn timeout. The boundary surfaces as
// a contextCompaction item that converse maps to EventCompaction (trigger "manual") and, in compact
// mode, to the terminal result.
func (a adapter) Compact(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	threadID := agent.FirstNonEmpty(in.Options.SessionID, in.SessionID)
	if threadID == "" {
		msg := "codex: no conversation to compact yet (run a turn first)"
		emit(agent.Event{Type: agent.EventError, Error: msg})
		return agent.TurnResult{Text: msg, IsError: true}, nil
	}

	env := map[string]string{}
	for k, v := range in.Env {
		env[k] = v
	}

	cmd := "codex app-server"
	if in.CWD != "" {
		cmd = "cd " + agent.ShellQuote(in.CWD) + " && " + cmd
	}

	return appServer{
		command:  cmd,
		env:      env,
		model:    strings.TrimSpace(in.Config[keyModel]),
		sandbox:  sandbox(in),
		approval: "never", // a compaction runs no tools; never pause for an approval no one can answer
		resumeID: threadID,
		compact:  true,
	}.Run(ctx, in, emit)
}
