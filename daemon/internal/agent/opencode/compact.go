package opencode

import (
	"context"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

var _ agent.CompactModule = adapter{}

// Compact runs opencode's on-demand compaction over the same per-turn server transport a prompt uses
// (server.go), driven with compact=true so converse POSTs /session/{id}/summarize instead of the
// prompt. opencode summarizes the conversation and folds it forward; the boundary surfaces as the
// session.compacted SSE frame, which converse maps to EventCompaction (trigger "manual") and, in
// compact mode, to the terminal result (a session.idle backstop terminates if that frame never lands).
//
// in.SessionID is the conversation to compact — it must be non-empty (opencode's summarize needs an
// existing session; a fresh chat has nothing to fold). The summarize route also requires an explicit
// {providerID,modelID}: unlike a prompt (which can fall back to opencode's configured default), an
// empty model is rejected, so Compact fails fast with a clear message rather than emitting a malformed
// request. A compaction runs no tools, so there is no approval pump — mirroring codex's compact path.
func (a adapter) Compact(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	sid := agent.FirstNonEmpty(in.Options.SessionID, in.SessionID)
	if sid == "" {
		return compactErr(emit, "opencode: no conversation to compact yet (run a turn first)")
	}
	provider, model := splitModel(in.Config[keyModel])
	if provider == "" || model == "" {
		return compactErr(emit, "opencode: set a provider/model on this chat before compacting (summarize requires an explicit model, e.g. anthropic/claude-sonnet-4)")
	}

	workdir := strings.TrimSpace(in.Config[keyWorkdir])
	if workdir == "" {
		workdir = in.CWD
	}
	env := map[string]string{}
	for k, v := range in.Env {
		env[k] = v
	}

	return server{
		env:      env,
		provider: provider,
		model:    model,
		cwd:      workdir,
		resumeID: sid,
		compact:  true,
	}.Run(ctx, in, emit)
}

// compactErr emits the error event and returns the matching error result (never a Go error — the
// supervisor surfaces the IsError result, same as a failed turn).
func compactErr(emit agent.Emit, msg string) (agent.TurnResult, error) {
	emit(agent.Event{Type: agent.EventError, Error: msg})
	return agent.TurnResult{Text: msg, IsError: true}, nil
}
