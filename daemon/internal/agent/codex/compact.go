package codex

import (
	"context"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

var _ agent.CompactModule = adapter{}

// Compact runs an on-demand compaction over the app-server transport: resume the thread, then send
// thread/compact/start {threadId}. It uses the same provider/auth settings as live chats, with
// compact=true so converse sends the compact RPC instead of turn/start.
//
// in.SessionID is the thread to compact; it must be non-empty (the API rejects a chat with no session,
// since a fresh thread has nothing to compact). A
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

	server := newAppServer(in, materialized{})
	server.resumeID, server.continueLatest = threadID, false
	server.approval, server.compact = "never", true
	return server.Run(ctx, in, emit)
}
