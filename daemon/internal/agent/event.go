package agent

// EventType is one kind of unified streaming event. Every adapter normalizes its
// agent's native output into these, so the client renders one stream regardless of agent.
type EventType string

const (
	EventSession      EventType = "session"     // session started; Meta has model/tools
	EventText         EventType = "text"        // assistant text (Delta=true for live tokens)
	EventThinking     EventType = "thinking"    // extended-thinking text
	EventToolUse      EventType = "tool_use"    // the agent invoked a tool
	EventToolResult   EventType = "tool_result" // a tool returned
	EventResult       EventType = "result"      // turn finished; Result has the final summary
	EventError        EventType = "error"
	EventStatus       EventType = "status"       // generic status (retry, queued, …) in Meta
	EventInteraction  EventType = "interaction"  // structured agent-defined prompt/feedback (todos, approval, …)
	EventCompaction   EventType = "compaction"   // the conversation was compacted; Compaction has trigger + token counts
	EventContinuation EventType = "continuation" // resolve-mode iteration boundary; Continuation has the iteration + reason
)

type ToolEvent struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Input   any    `json:"input,omitempty"`
	Output  string `json:"output,omitempty"`
	IsError bool   `json:"isError,omitempty"`
	// Action is the deep-normalized view of this tool call (canonical kind + structured payload),
	// carried alongside the raw Input/Output. Best-effort and optional: nil when the adapter can't
	// classify the tool. See toolaction.go.
	Action *ToolAction `json:"action,omitempty"`
}

// Usage is best-effort per-turn token accounting. All fields omitempty (additive wire field).
type Usage struct {
	InputTokens      int `json:"inputTokens,omitempty"`
	OutputTokens     int `json:"outputTokens,omitempty"`
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens  int `json:"reasoningTokens,omitempty"`
	TotalTokens      int `json:"totalTokens,omitempty"`
}

type ResultInfo struct {
	Text      string  `json:"text,omitempty"`
	IsError   bool    `json:"isError,omitempty"`
	SessionID string  `json:"sessionId,omitempty"`
	CostUSD   float64 `json:"costUsd,omitempty"`
	// Usage is best-effort per-turn token accounting, sitting alongside CostUSD. Nil when the agent
	// reported no token counts for the turn (additive wire field).
	Usage      *Usage `json:"usage,omitempty"`
	NumTurns   int    `json:"numTurns,omitempty"`
	DurationMS int    `json:"durationMs,omitempty"`
	// Subtype is the agent's own terminal-result classifier when it distinguishes one (Claude's
	// result subtype: "success", "error_max_turns", "error_max_budget_usd", …). Empty for agents
	// whose terminal event is always fully settled (Codex). Continuable(Subtype) reports whether it
	// means "stopped short but resumable" — the signal the resolve loop drives on.
	Subtype string `json:"subtype,omitempty"`
	// Incomplete is a derived convenience: true when the turn stopped short on a continuable subtype
	// (i.e. Continuable(Subtype)). A resolve iteration reads it to decide whether to auto-resume.
	Incomplete bool `json:"incomplete,omitempty"`
}

// CompactionInfo describes a conversation compaction — auto (the agent hit its window and summarized
// on its own) or manual (an on-demand compact). It's the agent-agnostic payload for an
// EventCompaction event AND for a "compaction" transcript Part, so the SSE stream and reloaded
// history show a compaction the same way. Every field is best-effort: an agent that doesn't report
// token counts or a trigger leaves them zero/empty.
type CompactionInfo struct {
	Trigger    string `json:"trigger,omitempty"`    // "auto" | "manual" (empty when the agent doesn't say)
	PreTokens  int    `json:"preTokens,omitempty"`  // context size before compaction
	PostTokens int    `json:"postTokens,omitempty"` // context size after compaction
	Summary    string `json:"summary,omitempty"`    // the continuation summary the agent wrote, when present
}

// ContinuationInfo delimits one iteration of a resolve-mode run on the merged parent stream. The
// supervisor emits an EventContinuation before each child turn (and once at the caps boundary), so a
// client reading the parent topic can tell one sub-turn from the next and see WHY the loop advanced.
type ContinuationInfo struct {
	Iteration  int    `json:"iteration"`            // 0-based iteration index this boundary opens
	Reason     string `json:"reason,omitempty"`     // "start" | "continue" | "probe" | "max_turns" | "max_budget"
	ChildRunID string `json:"childRunId,omitempty"` // the child Run this iteration streams under (its own record in the tree)
	StopReason string `json:"stopReason,omitempty"` // set on the final boundary: "done" | "capped" | "error"
}

// Event is the unified stream item. Optional fields are populated per Type.
type Event struct {
	Type         EventType         `json:"type"`
	SessionID    string            `json:"sessionId,omitempty"`
	Text         string            `json:"text,omitempty"`
	Delta        bool              `json:"delta,omitempty"`
	Tokens       int               `json:"tokens,omitempty"` // cumulative token count for the block (e.g. thinking preview)
	Tool         *ToolEvent        `json:"tool,omitempty"`
	Result       *ResultInfo       `json:"result,omitempty"`
	Interaction  *Interaction      `json:"interaction,omitempty"`
	Compaction   *CompactionInfo   `json:"compaction,omitempty"`   // populated on EventCompaction
	Continuation *ContinuationInfo `json:"continuation,omitempty"` // populated on EventContinuation (resolve mode)
	Error        string            `json:"error,omitempty"`
	Meta         map[string]any    `json:"meta,omitempty"`
	At           string            `json:"at,omitempty"`
}

// Continuable reports whether a terminal result subtype means the turn "stopped short but can be
// resumed" — the agent hit a self-imposed budget (max turns / max cost) rather than genuinely
// finishing or erroring. It is the single source of truth the resolve loop uses to decide whether to
// auto-continue. Agents with no subtype concept (Codex) always return false here (their terminal
// event is fully settled), so resolve completes them in one iteration.
func Continuable(subtype string) bool {
	switch subtype {
	case "error_max_turns", "error_max_budget_usd":
		return true
	default:
		return false
	}
}

// Emit is how an adapter reports unified events during a turn.
type Emit func(Event)
