package agent

import "context"

// CompactModule is an OPTIONAL adapter capability (type-asserted like MemoryModule, NOT bolted onto
// the mandatory Adapter interface): compact a chat's conversation ON DEMAND, folding prior context
// into a summary the agent carries forward. An adapter that implements it sets
// Capabilities.CompactNow=true; the API type-asserts THIS interface as the authoritative gate before
// serving POST /chats/{id}/compact.
//
// It has the SAME shape as Adapter.RunStream so the supervisor drives it through the identical runner
// path — a compaction runs as a first-class RUN (its own run id + SSE stream) and the boundary is
// accumulated into the transcript with zero extra plumbing. The adapter emits the same EventCompaction
// the auto-compaction path does, so an on-demand compaction surfaces in the live stream and the
// reloaded history exactly like one the agent triggered itself.
//
// in.Message carries any optional focus instructions (may be empty); the adapter prepends its own
// native trigger (Claude's /compact slash command, Codex's thread/compact/start RPC). in.SessionID is
// the conversation to compact — the API rejects a chat with no session yet, so an implementation may
// assume there is something to compact.
type CompactModule interface {
	Compact(ctx context.Context, in TurnInput, emit Emit) (TurnResult, error)
}
