package opencode

import (
	"encoding/json"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// normalize.go maps opencode's SSE bus onto mindwire's unified event stream. opencode streams a
// message as a growing set of "parts" (text, reasoning, tool, step-*); message.part.updated fires
// repeatedly with the part's CUMULATIVE state (part.text holds everything so far, not just the new
// chunk), and tool parts carry a lifecycle status (pending→running→completed|error). The invariant we
// hold, matching codex/normalize.go: every visible byte is emitted exactly once, all text/thinking
// events are Delta=true, and each tool yields exactly one tool_use and at most one tool_result.

// ── wire shapes (opencode uses camelCase with an -ID suffix: sessionID/messageID/callID) ──

// ocPart is the union member from message.part.updated{part}. Only the fields we consume are decoded;
// Type selects which are meaningful.
type ocPart struct {
	ID        string       `json:"id"`
	SessionID string       `json:"sessionID"`
	MessageID string       `json:"messageID"`
	Type      string       `json:"type"` // text | reasoning | tool | step-start | step-finish | …
	Text      string       `json:"text"` // cumulative, for text/reasoning
	CallID    string       `json:"callID"`
	Tool      string       `json:"tool"`
	State     *ocToolState `json:"state"`
}

// ocToolState is a tool part's lifecycle. Input is the tool's arguments (raw JSON); Output/Error carry
// the result once the tool has run.
type ocToolState struct {
	Status string          `json:"status"` // pending | running | completed | error
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output"`
	Title  string          `json:"title"`
	Error  string          `json:"error"`
}

// ocMessage is message.updated{info}. For an assistant message it carries running cost + token usage.
type ocMessage struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Role      string   `json:"role"`
	Cost      float64  `json:"cost"`
	Tokens    ocTokens `json:"tokens"`
}

type ocTokens struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

// ocPermission is the permission.updated payload — opencode sends the Permission object AS properties
// (it is the "ask" itself), so normalize decodes properties directly into this.
type ocPermission struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionID"`
	Title     string          `json:"title"`
	Type      string          `json:"type"`
	CallID    string          `json:"callID"`
	Metadata  json.RawMessage `json:"metadata"`
}

// ── streaming state ──

// streamState tracks per-part progress so cumulative text turns into once-only deltas and each tool
// fires one use + one result. Mirrors codex's streamState.
type streamState struct {
	emitted    map[string]int    // part id → bytes of text/reasoning already emitted
	seenUse    map[string]bool   // tool id → tool_use emitted
	seenResult map[string]bool   // tool id → tool_result emitted
	lastTodo   map[string]string // todowrite id → last emitted snapshot signature (de-dupe)
}

func newStreamState() *streamState {
	return &streamState{
		emitted:    map[string]int{},
		seenUse:    map[string]bool{},
		seenResult: map[string]bool{},
		lastTodo:   map[string]string{},
	}
}

// emitPart normalizes one message part and emits its unified events. For a text part it returns the
// part id and the cumulative full text so the caller can accumulate the final answer (concatenating
// text parts in first-seen order reproduces exactly what the deltas streamed); for everything else it
// returns "","".
func (st *streamState) emitPart(p ocPart, emit agent.Emit) (id, full string) {
	switch p.Type {
	case "text":
		st.streamText(agent.EventText, p.ID, p.Text, emit)
		return p.ID, p.Text
	case "reasoning":
		st.streamText(agent.EventThinking, p.ID, p.Text, emit)
		return "", ""
	case "tool":
		if p.Tool == "todowrite" {
			st.emitTodos(p, emit)
			return "", ""
		}
		st.emitTool(p, emit)
		return "", ""
	default:
		// step-start / step-finish / file / patch / snapshot / … carry no directly-visible content in v1.
		return "", ""
	}
}

// streamText emits the not-yet-seen SUFFIX of a cumulative text/reasoning part. id=="" (shouldn't
// happen for opencode parts, which always have prt_ ids) falls back to emitting the whole block once.
// Whitespace-only growth is swallowed so a trailing "\n" from the model doesn't surface as an event.
func (st *streamState) streamText(evType agent.EventType, id, full string, emit agent.Emit) {
	if id == "" {
		if strings.TrimSpace(full) != "" {
			emit(agent.Event{Type: evType, Text: full, Delta: true})
		}
		return
	}
	prev := st.emitted[id]
	if len(full) <= prev {
		return
	}
	chunk := full[prev:]
	st.emitted[id] = len(full)
	if strings.TrimSpace(chunk) != "" {
		emit(agent.Event{Type: evType, Text: chunk, Delta: true})
	}
}

// emitTool fires a single tool_use (on first sighting of the call id) and a single tool_result (when
// the tool reaches completed/error). The tool id is the callID (falling back to the part id), matching
// how opencode threads a call across its pending→completed updates.
func (st *streamState) emitTool(p ocPart, emit agent.Emit) {
	id := agent.FirstNonEmpty(p.CallID, p.ID)
	if id == "" {
		return
	}
	name := p.Tool
	if name == "" {
		name = "tool"
	}
	var input json.RawMessage
	status := ""
	if p.State != nil {
		input = p.State.Input
		status = p.State.Status
	}
	if !st.seenUse[id] {
		st.seenUse[id] = true
		emit(agent.Event{Type: agent.EventToolUse, Tool: &agent.ToolEvent{
			ID: id, Name: name, Input: toolInput(input), Action: ocToolAction(name, input),
		}})
	}
	if (status == "completed" || status == "error") && !st.seenResult[id] {
		st.seenResult[id] = true
		out, isErr := toolResult(p.State)
		emit(agent.Event{Type: agent.EventToolResult, Tool: &agent.ToolEvent{
			ID: id, Name: name, Output: out, IsError: isErr, Action: ocToolAction(name, input),
		}})
	}
}

// emitTodos turns a todowrite tool call into a unified todos Interaction (a plan snapshot) instead of a
// generic tool_use/tool_result pair, so clients render it as the plan panel. todowrite fires on both
// running and completed with the same input, so identical consecutive snapshots are de-duped by id.
func (st *streamState) emitTodos(p ocPart, emit agent.Emit) {
	if p.State == nil || len(p.State.Input) == 0 {
		return
	}
	var in struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal(p.State.Input, &in) != nil || len(in.Todos) == 0 {
		return
	}
	rows := make([]todoRow, 0, len(in.Todos))
	var sig strings.Builder
	for _, t := range in.Todos {
		rows = append(rows, todoRow{Content: t.Content, Status: t.Status})
		sig.WriteString(t.Status)
		sig.WriteByte(':')
		sig.WriteString(t.Content)
		sig.WriteByte('\n')
	}
	inter := buildTodos(rows)
	if inter == nil {
		return
	}
	id := agent.FirstNonEmpty(p.CallID, p.ID)
	if st.lastTodo[id] == sig.String() {
		return // unchanged snapshot — don't re-emit
	}
	st.lastTodo[id] = sig.String()
	emit(agent.Event{Type: agent.EventInteraction, Interaction: inter})
}

// ── pure mappers ──

// toolInput returns the tool arguments as-is for the ToolEvent envelope (nil when absent).
func toolInput(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// toolResult extracts the result text and error flag from a finished tool state. On error with no
// output, the error string stands in as the output so the result is never blank.
func toolResult(st *ocToolState) (string, bool) {
	if st == nil {
		return "", false
	}
	isErr := st.Status == "error"
	out := st.Output
	if isErr && strings.TrimSpace(out) == "" {
		out = st.Error
	}
	return out, isErr
}

// ocToolAction builds the deep-normalized ToolAction for an opencode tool call from its name + raw
// input. Unknown tools classify as KindOther (never guessed) with the tool name as the title.
func ocToolAction(name string, raw json.RawMessage) *agent.ToolAction {
	switch name {
	case "bash":
		var in struct {
			Command     string `json:"command"`
			Description string `json:"description"`
		}
		_ = json.Unmarshal(raw, &in)
		title := agent.FirstNonEmpty(in.Description, in.Command)
		return &agent.ToolAction{Kind: agent.KindShell, Title: title, Shell: &agent.ShellCommand{Command: in.Command}}

	case "edit":
		var in struct {
			FilePath  string `json:"filePath"`
			OldString string `json:"oldString"`
			NewString string `json:"newString"`
		}
		_ = json.Unmarshal(raw, &in)
		fc := agent.FileChange{Path: in.FilePath, Op: "edit", OldText: in.OldString, NewText: in.NewString}
		if d := agent.BuildUnifiedDiff(in.FilePath, in.OldString, in.NewString); d != "" {
			fc.Diff = d
		}
		return &agent.ToolAction{Kind: agent.KindFileEdit, Title: in.FilePath, Files: []agent.FileChange{fc}}

	case "write":
		var in struct {
			FilePath string `json:"filePath"`
			Content  string `json:"content"`
		}
		_ = json.Unmarshal(raw, &in)
		fc := agent.FileChange{Path: in.FilePath, Op: "create", NewText: in.Content}
		if d := agent.BuildUnifiedDiff(in.FilePath, "", in.Content); d != "" {
			fc.Diff = d
		}
		return &agent.ToolAction{Kind: agent.KindFileEdit, Title: in.FilePath, Files: []agent.FileChange{fc}}

	case "read":
		var in struct {
			FilePath string `json:"filePath"`
		}
		_ = json.Unmarshal(raw, &in)
		return &agent.ToolAction{Kind: agent.KindFileRead, Title: in.FilePath,
			Files: []agent.FileChange{{Path: in.FilePath}}}

	case "grep":
		var in struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
			Include string `json:"include"`
		}
		_ = json.Unmarshal(raw, &in)
		return &agent.ToolAction{Kind: agent.KindSearch, Title: in.Pattern,
			Search: &agent.SearchQuery{Query: in.Pattern, Path: in.Path, Glob: in.Include}}

	case "glob", "list":
		var in struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		_ = json.Unmarshal(raw, &in)
		glob := agent.FirstNonEmpty(in.Pattern, in.Path)
		return &agent.ToolAction{Kind: agent.KindSearch, Title: glob,
			Search: &agent.SearchQuery{Glob: in.Pattern, Path: in.Path}}

	case "webfetch":
		var in struct {
			URL    string `json:"url"`
			Format string `json:"format"`
		}
		_ = json.Unmarshal(raw, &in)
		return &agent.ToolAction{Kind: agent.KindWebFetch, Title: in.URL, Web: &agent.WebSearch{URL: in.URL}}

	default:
		title := name
		if title == "" {
			title = "tool"
		}
		return &agent.ToolAction{Kind: agent.KindOther, Title: title}
	}
}

// usageMeta turns an assistant message's cost + token usage into result Meta. Returns nil when nothing
// was reported (a zero snapshot), so an empty map never rides on the result.
func usageMeta(m ocMessage) map[string]any {
	t := m.Tokens
	if m.Cost == 0 && t.Input == 0 && t.Output == 0 && t.Reasoning == 0 && t.Cache.Read == 0 && t.Cache.Write == 0 {
		return nil
	}
	return map[string]any{
		"costUsd":          m.Cost,
		"inputTokens":      t.Input,
		"outputTokens":     t.Output,
		"reasoningTokens":  t.Reasoning,
		"cacheReadTokens":  t.Cache.Read,
		"cacheWriteTokens": t.Cache.Write,
	}
}

// usageStruct maps an assistant message's token usage to the typed agent.Usage carried on the terminal
// result (alongside — not replacing — the existing cost+token Meta). opencode reports no grand total,
// so TotalTokens is the input+output sum. Returns nil when no tokens were reported, so the wire field
// stays omitempty.
func usageStruct(m ocMessage) *agent.Usage {
	t := m.Tokens
	if t.Input == 0 && t.Output == 0 && t.Reasoning == 0 && t.Cache.Read == 0 && t.Cache.Write == 0 {
		return nil
	}
	return &agent.Usage{
		InputTokens:      t.Input,
		OutputTokens:     t.Output,
		ReasoningTokens:  t.Reasoning,
		CacheReadTokens:  t.Cache.Read,
		CacheWriteTokens: t.Cache.Write,
		TotalTokens:      t.Input + t.Output,
	}
}

// sessionErrorText pulls a human message out of a session.error payload, tolerating opencode's
// name/data.message/message shapes and falling back to a generic label.
func sessionErrorText(raw json.RawMessage) string {
	var p struct {
		Error struct {
			Name    string `json:"name"`
			Message string `json:"message"`
			Data    struct {
				Message string `json:"message"`
			} `json:"data"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &p)
	msg := agent.FirstNonEmpty(p.Error.Data.Message, p.Error.Message, p.Error.Name)
	if msg == "" {
		return "opencode session error"
	}
	return msg
}

// permissionInteraction turns a permission.updated ask into an approval Interaction. The interaction id
// IS the opencode permission id, so the inbound response maps straight back to the decision route; the
// session id rides in Meta as a secondary reference.
func permissionInteraction(perm ocPermission) *agent.Interaction {
	title := perm.Title
	if title == "" {
		title = "opencode needs your approval"
	}
	return &agent.Interaction{
		ID:            perm.ID,
		Kind:          "approval",
		Title:         title,
		Options:       []agent.Action{{ID: "allow", Label: "Approve"}, {ID: "deny", Label: "Reject"}},
		NeedsResponse: true,
		Meta:          map[string]any{"permissionID": perm.ID, "sessionID": perm.SessionID},
	}
}

// ── todos plan snapshot (opencode todowrite → unified todos Interaction) ──

type todoRow struct {
	Content string
	Status  string
}

// buildTodos builds a todos Interaction from todowrite rows, dropping blank content and defaulting an
// empty status to "pending". Returns nil when nothing usable remains.
func buildTodos(rows []todoRow) *agent.Interaction {
	items := make([]agent.TodoItem, 0, len(rows))
	for _, r := range rows {
		content := strings.TrimSpace(r.Content)
		if content == "" {
			continue
		}
		status := strings.TrimSpace(r.Status)
		if status == "" {
			status = "pending"
		}
		items = append(items, agent.TodoItem{Content: content, Status: status})
	}
	if len(items) == 0 {
		return nil
	}
	return &agent.Interaction{Kind: "todos", Title: "Plan", Items: items}
}
