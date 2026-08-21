package codex

import (
	"encoding/json"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// normalize.go is the single item-mapping surface for Codex's two transports. The exec `--json` stream
// (snake_case, parse.go) and the app-server JSON-RPC stream (camelCase, appserver.go) describe the SAME
// thread items in different shapes. Each transport decodes its own wire struct, then normalizes into
// normItem here; from that ONE model, the tool naming, tool-result derivation, change summary, todo
// building, token-usage Meta, and the text/thinking/tool emit tail are shared. Previously these were
// byte-for-byte twin functions that could silently drift apart.

// itemKind is the transport-independent classification of a thread item.
type itemKind int

const (
	kindOther itemKind = iota
	kindAgentMessage
	kindReasoning
	kindCommand
	kindFileChange
	kindMCPTool
	kindDynamicTool
	kindWebSearch
)

// execKind maps an exec (snake_case) item type to its normalized kind. todo_list is deliberately
// kindOther: todos are handled by the caller (todosInteraction) before an item reaches emitNorm.
func execKind(t string) itemKind {
	switch t {
	case "agent_message":
		return kindAgentMessage
	case "reasoning":
		return kindReasoning
	case "command_execution":
		return kindCommand
	case "file_change":
		return kindFileChange
	case "mcp_tool_call":
		return kindMCPTool
	case "web_search":
		return kindWebSearch
	default:
		return kindOther
	}
}

// asKind maps an app-server (camelCase) item type to its normalized kind.
func asKind(t string) itemKind {
	switch t {
	case "agentMessage":
		return kindAgentMessage
	case "reasoning":
		return kindReasoning
	case "commandExecution":
		return kindCommand
	case "fileChange":
		return kindFileChange
	case "mcpToolCall":
		return kindMCPTool
	case "dynamicToolCall":
		return kindDynamicTool
	case "webSearch":
		return kindWebSearch
	default:
		return kindOther
	}
}

// normChange is one file edit within a fileChange item. Its json tags match both wire shapes (exec's
// `changes` and app-server's `changes` are identical), so both item structs decode into it directly.
type normChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	// Diff is a best-effort per-file unified diff. Codex's LIVE file_change item reports only
	// path+kind, so this is empty there; it's populated from the apply_patch body on the history path.
	Diff string `json:"diff"`
}

// normItem is a thread item normalized across transports. Only the fields relevant to a given Kind are
// populated. rawType preserves the original (transport-specific) type string for the toolName default.
// Text is the already-resolved agent-message OR reasoning text (each decoder applies its own
// join/trim rules, so emitNorm can treat Text uniformly).
type normItem struct {
	ID       string
	Kind     itemKind
	rawType  string
	Text     string
	Command  string // command_execution command line
	Cwd      string // command_execution working dir (exec transport only)
	Query    string // web_search query
	Output   string // aggregated command output
	ExitCode *int
	Status   string // completed | failed | declined | inProgress | ...
	Server   string // mcp tool
	Tool     string // mcp / dynamic tool
	Changes  []normChange
	Result   json.RawMessage // mcp tool result
}

// fromExecItem normalizes an exec-stream item. Reasoning text prefers Text, falling back to Summary;
// both are emitted verbatim (untrimmed) to match the exec path's historical behavior.
func fromExecItem(it item) normItem {
	n := normItem{
		ID:       it.ID,
		Kind:     execKind(it.Type),
		rawType:  it.Type,
		Command:  it.Command,
		Cwd:      it.Cwd,
		Query:    it.Query,
		Output:   it.AggregatedOutput,
		ExitCode: it.ExitCode,
		Status:   it.Status,
		Server:   it.Server,
		Tool:     it.Tool,
		Changes:  it.Changes,
		Result:   it.Result,
	}
	switch n.Kind {
	case kindAgentMessage:
		n.Text = it.Text
	case kindReasoning:
		n.Text = it.Text
		if strings.TrimSpace(n.Text) == "" {
			n.Text = it.Summary
		}
	}
	return n
}

// fromAsItem normalizes an app-server item. Reasoning content/summary arrive as string slices; they
// are joined and trimmed (the app-server path's historical behavior).
func fromAsItem(it asItem) normItem {
	n := normItem{
		ID:       it.ID,
		Kind:     asKind(it.Type),
		rawType:  it.Type,
		Command:  it.Command,
		Cwd:      it.Cwd,
		Query:    it.Query,
		Output:   it.Aggregated,
		ExitCode: it.ExitCode,
		Status:   it.Status,
		Server:   it.Server,
		Tool:     it.Tool,
		Changes:  it.Changes,
		Result:   it.Result,
	}
	switch n.Kind {
	case kindAgentMessage:
		n.Text = it.Text
	case kindReasoning:
		n.Text = strings.TrimSpace(strings.Join(it.Content, ""))
		if n.Text == "" {
			n.Text = strings.TrimSpace(strings.Join(it.Summary, ""))
		}
	}
	return n
}

// toolName is a friendly card title for a tool item.
func toolName(n normItem) string {
	switch n.Kind {
	case kindCommand:
		return "shell"
	case kindFileChange:
		return "apply_patch"
	case kindWebSearch:
		return "web_search"
	case kindMCPTool:
		switch {
		case n.Server != "" && n.Tool != "":
			return n.Server + "." + n.Tool
		case n.Tool != "":
			return n.Tool
		default:
			return "mcp_tool_call"
		}
	case kindDynamicTool:
		if n.Tool != "" {
			return n.Tool
		}
		return "tool"
	default:
		return n.rawType
	}
}

// toolResult derives a completed tool's output text and error flag from its item shape. A "declined"
// status only occurs on the app-server transport; checking it here is harmless for the exec path.
func toolResult(n normItem) (string, bool) {
	isErr := n.Status == "failed" || n.Status == "declined" || (n.ExitCode != nil && *n.ExitCode != 0)
	switch n.Kind {
	case kindCommand:
		return n.Output, isErr
	case kindFileChange:
		return summarizeChanges(n.Changes), isErr
	case kindMCPTool:
		if len(n.Result) > 0 {
			return strings.TrimSpace(string(n.Result)), isErr
		}
		return "", isErr
	default:
		return "", isErr
	}
}

// summarizeChanges renders a file_change item's edits as "<kind> <path>" lines.
func summarizeChanges(ch []normChange) string {
	if len(ch) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ch))
	for _, c := range ch {
		switch {
		case c.Kind != "" && c.Path != "":
			parts = append(parts, c.Kind+" "+c.Path)
		case c.Path != "":
			parts = append(parts, c.Path)
		}
	}
	return strings.Join(parts, "\n")
}

// codexToolAction deep-normalizes a tool item into the unified agent.ToolAction. It MUST be pure (no
// shared state): the app-server transport calls emitTool from multiple goroutines via serializeEmit.
// Codex, unlike Claude, DOES report a shell exit code, so ShellCommand.ExitCode is populated (nil only
// when the item hasn't completed); stderr is merged into stdout upstream, so Stderr stays nil.
func codexToolAction(n normItem) *agent.ToolAction {
	switch n.Kind {
	case kindCommand:
		return &agent.ToolAction{
			Kind: agent.KindShell, Title: n.Command,
			Shell: &agent.ShellCommand{Command: n.Command, Cwd: n.Cwd, Stdout: n.Output, ExitCode: n.ExitCode},
		}
	case kindFileChange:
		files := make([]agent.FileChange, 0, len(n.Changes))
		for _, c := range n.Changes {
			files = append(files, agent.FileChange{Path: c.Path, Op: agent.MapChangeOp(c.Kind), Diff: c.Diff})
		}
		title := ""
		if len(files) == 1 {
			title = files[0].Path
		}
		return &agent.ToolAction{Kind: agent.KindFileEdit, Title: title, Files: files}
	case kindWebSearch:
		return &agent.ToolAction{Kind: agent.KindWebSearch, Title: n.Query, Web: &agent.WebSearch{Query: n.Query}}
	case kindMCPTool:
		return &agent.ToolAction{Kind: agent.KindMCP, Title: toolName(n), MCP: &agent.MCPCall{Server: n.Server, Tool: n.Tool}}
	case kindDynamicTool:
		if n.Tool != "" { // a dynamic tool is MCP-shaped when it names a tool
			return &agent.ToolAction{Kind: agent.KindMCP, Title: n.Tool, MCP: &agent.MCPCall{Tool: n.Tool}}
		}
		return &agent.ToolAction{Kind: agent.KindOther, Title: toolName(n)}
	default:
		return &agent.ToolAction{Kind: agent.KindOther, Title: toolName(n)}
	}
}

// itemPhase is a thread item's lifecycle position, normalized across transports (exec's
// item.started/updated/completed and the app-server's started/updated/completed). It drives the
// streaming discipline: text/thinking stream as they grow, tool_result fires only at completion.
type itemPhase int

const (
	phaseStarted itemPhase = iota
	phaseUpdated
	phaseCompleted
)

// execPhase maps an exec envelope type to a lifecycle phase.
func execPhase(t string) itemPhase {
	switch t {
	case "item.completed":
		return phaseCompleted
	case "item.updated":
		return phaseUpdated
	default:
		return phaseStarted
	}
}

// asPhase maps an app-server item notification suffix (started|updated|completed) to a lifecycle phase.
func asPhase(t string) itemPhase {
	switch t {
	case "completed":
		return phaseCompleted
	case "updated":
		return phaseUpdated
	default:
		return phaseStarted
	}
}

// streamState is the per-turn mutable state threaded through the item mappers. seenUse dedups tool_use
// announcements by item id; emitted records how many bytes of each text/thinking item have already been
// emitted, so a cumulative item.updated → item.completed sequence streams as suffix deltas and never
// double-counts. A turn creates one and passes it to every emitNorm call.
type streamState struct {
	seenUse map[string]bool
	emitted map[string]int
}

func newStreamState() *streamState {
	return &streamState{seenUse: map[string]bool{}, emitted: map[string]int{}}
}

// emitNorm maps one normalized item at a lifecycle phase to unified events. Text and thinking stream
// incrementally (Delta=true) as the item grows across phases; a tool announces a tool_use once (first
// time its id is seen) and a tool_result at completion. raw is the original item JSON, preserved verbatim
// as the tool Input (forward-compatible). Returns the item's full text when a completed agent message
// (the running final answer), else "".
func emitNorm(n normItem, phase itemPhase, raw json.RawMessage, emit agent.Emit, st *streamState) string {
	switch n.Kind {
	case kindAgentMessage:
		return st.streamText(n, agent.EventText, phase, emit)
	case kindReasoning:
		st.streamText(n, agent.EventThinking, phase, emit)
	case kindCommand, kindFileChange, kindMCPTool, kindDynamicTool, kindWebSearch:
		st.emitTool(n, phase, raw, emit)
	}
	return ""
}

// streamText emits the newly-arrived suffix of a text/thinking item as a Delta=true event, recording how
// much of the item has been sent. A whole-block item (id seen only at completion) emits its full text
// once — the same single event as before, now flagged Delta=true to match claude's streaming discipline;
// a cumulative item.updated → item.completed sequence streams the growth without re-emitting. Either way
// each byte is emitted exactly once, so the runner's accumulated text equals the final text. Returns the
// item's full text at completion (else ""), for the agent-message final-answer running total.
func (st *streamState) streamText(n normItem, evType agent.EventType, phase itemPhase, emit agent.Emit) string {
	full := n.Text
	if n.ID == "" {
		// No id to correlate incremental frames — emit the whole block once, at completion (legacy path).
		if phase == phaseCompleted && strings.TrimSpace(full) != "" {
			emit(agent.Event{Type: evType, Text: full, Delta: true})
		}
	} else if prev := st.emitted[n.ID]; len(full) > prev {
		// Only emit a suffix once real (non-whitespace) text has arrived, so an all-whitespace early
		// frame doesn't surface an empty block; advance the cursor either way so it isn't re-sent.
		if strings.TrimSpace(full) != "" {
			emit(agent.Event{Type: evType, Text: full[prev:], Delta: true})
		}
		st.emitted[n.ID] = len(full)
	}
	if phase == phaseCompleted && strings.TrimSpace(full) != "" {
		return full
	}
	return ""
}

// emitTool announces a tool_use the first time an item id is seen and a tool_result when it completes,
// correlating by item id.
func (st *streamState) emitTool(n normItem, phase itemPhase, raw json.RawMessage, emit agent.Emit) {
	name := toolName(n)
	if n.ID != "" && !st.seenUse[n.ID] {
		st.seenUse[n.ID] = true
		emit(agent.Event{Type: agent.EventToolUse,
			Tool: &agent.ToolEvent{ID: n.ID, Name: name, Input: json.RawMessage(raw), Action: codexToolAction(n)}})
	}
	if phase == phaseCompleted {
		out, isErr := toolResult(n)
		emit(agent.Event{Type: agent.EventToolResult,
			Tool: &agent.ToolEvent{ID: n.ID, Name: name, Output: out, IsError: isErr, Action: codexToolAction(n)}})
	}
}

// --- todos ---------------------------------------------------------------------------------------

// todoRow is one plan/todo entry before normalization: the winning content candidate plus the status
// signals. Both codex todo shapes (exec's todo_list item, the app-server turn/plan notification) decode
// into this, then share buildTodos.
type todoRow struct {
	Content   string
	Status    string
	Completed *bool
}

// buildTodos maps normalized rows to a unified todos interaction, defaulting status (a completed flag,
// else "pending") and dropping blank-content rows. nil if nothing survives.
func buildTodos(rows []todoRow) *agent.Interaction {
	items := make([]agent.TodoItem, 0, len(rows))
	for _, r := range rows {
		if strings.TrimSpace(r.Content) == "" {
			continue
		}
		status := r.Status
		if status == "" && r.Completed != nil {
			if *r.Completed {
				status = "completed"
			} else {
				status = "pending"
			}
		}
		if status == "" {
			status = "pending"
		}
		items = append(items, agent.TodoItem{Content: r.Content, Status: status})
	}
	if len(items) == 0 {
		return nil
	}
	return &agent.Interaction{Kind: "todos", Title: "Plan", Items: items}
}

// --- token usage ---------------------------------------------------------------------------------

// tokenUsage is Codex's per-turn token accounting, normalized across transports. Codex reports tokens,
// not USD. TotalTokens is app-server-only; HasTotal marks whether it is meaningful.
type tokenUsage struct {
	InputTokens           int
	CachedInputTokens     int
	OutputTokens          int
	ReasoningOutputTokens int
	TotalTokens           int
	HasTotal              bool
}

// usageMeta renders token counts into the Meta map carried on result/status events. totalTokens is
// included only when reported (the app-server transport), so the exec Meta stays exactly as before.
func usageMeta(u tokenUsage) map[string]any {
	m := map[string]any{
		"inputTokens":           u.InputTokens,
		"cachedInputTokens":     u.CachedInputTokens,
		"outputTokens":          u.OutputTokens,
		"reasoningOutputTokens": u.ReasoningOutputTokens,
	}
	if u.HasTotal {
		m["totalTokens"] = u.TotalTokens
	}
	return m
}

// usageStruct maps Codex's per-turn token accounting to the typed agent.Usage carried on the terminal
// result (alongside — not replacing — the existing Meta usage). CachedInputTokens is Codex's cache-read
// concept; Codex has no cache-write, so CacheWriteTokens stays 0. TotalTokens is set only when reported
// (HasTotal). Returns nil when nothing was reported, so the wire field stays omitempty.
func usageStruct(u tokenUsage) *agent.Usage {
	total := 0
	if u.HasTotal {
		total = u.TotalTokens
	}
	if u.InputTokens == 0 && u.CachedInputTokens == 0 && u.OutputTokens == 0 && u.ReasoningOutputTokens == 0 && total == 0 {
		return nil
	}
	return &agent.Usage{
		InputTokens:     u.InputTokens,
		OutputTokens:    u.OutputTokens,
		CacheReadTokens: u.CachedInputTokens,
		ReasoningTokens: u.ReasoningOutputTokens,
		TotalTokens:     total,
	}
}
