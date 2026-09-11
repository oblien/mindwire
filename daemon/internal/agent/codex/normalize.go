package codex

import (
	"encoding/json"
	"fmt"
	"sort"
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
	kindPlan
	kindCollab
	kindImage
	kindImageGeneration
	kindReview
	kindOutput
	kindSubAgentActivity
	kindSleep
	kindCompaction
	kindError
	kindIgnored
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
	case "plan":
		return kindPlan
	case "collab_tool_call", "collab_agent_tool_call":
		return kindCollab
	case "image_view":
		return kindImage
	case "image_generation":
		return kindImageGeneration
	case "sub_agent_activity":
		return kindSubAgentActivity
	case "sleep":
		return kindSleep
	case "context_compaction":
		return kindCompaction
	case "error":
		return kindError
	case "user_message", "hook_prompt":
		return kindIgnored
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
	case "plan":
		return kindPlan
	case "collabToolCall", "collabAgentToolCall":
		return kindCollab
	case "imageView":
		return kindImage
	case "imageGeneration":
		return kindImageGeneration
	case "enteredReviewMode", "exitedReviewMode":
		return kindReview
	case "functionCallOutput":
		return kindOutput
	case "subAgentActivity":
		return kindSubAgentActivity
	case "sleep":
		return kindSleep
	case "contextCompaction":
		return kindCompaction
	case "error":
		return kindError
	case "userMessage", "hookPrompt":
		return kindIgnored
	default:
		return kindOther
	}
}

// Exec uses a string kind; app-server uses {type, move_path}. Decode those separately so an
// app-server file edit cannot make its entire item (or enclosing turn) disappear.
type normChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	// App-server patches and native rollouts carry per-file diffs; older exec items may omit them.
	Diff     string `json:"diff"`
	MovePath string `json:"movePath,omitempty"`
}

func (c *normChange) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Path     string          `json:"path"`
		Kind     json.RawMessage `json:"kind"`
		Diff     string          `json:"diff"`
		MovePath string          `json:"movePath"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	c.Path, c.Diff = wire.Path, wire.Diff
	c.MovePath = wire.MovePath
	if json.Unmarshal(wire.Kind, &c.Kind) == nil {
		return nil
	}
	var kind struct {
		Type     string `json:"type"`
		MovePath string `json:"move_path"`
	}
	if err := json.Unmarshal(wire.Kind, &kind); err != nil {
		return err
	}
	c.Kind, c.MovePath = kind.Type, kind.MovePath
	return nil
}

// normItem is a thread item normalized across transports. Only the fields relevant to a given Kind are
// populated. rawType preserves the original (transport-specific) type string for the toolName default.
// Text is the already-resolved agent-message OR reasoning text (each decoder applies its own
// join/trim rules, so emitNorm can treat Text uniformly).
type normItem struct {
	ID               string
	Kind             itemKind
	rawType          string
	Text             string
	Command          string // command_execution command line
	Cwd              string // command working directory
	Query            string // web_search query
	Output           string // aggregated command output
	OutputKnown      bool   // an explicit final empty string overrides any streamed preview
	ExitCode         *int
	Status           string // completed | failed | declined | inProgress | ...
	Server           string // mcp tool
	Tool             string // mcp / dynamic tool
	Changes          []normChange
	Result           json.RawMessage // mcp tool result
	Error            json.RawMessage
	Arguments        json.RawMessage
	Success          *bool
	MessagePhase     string
	Summary, Content []string
	Path             string
	Review           string
	Prompt           string
	WebAction        *webAction
	Agents           map[string]collabState
	AgentPath        string
	AgentThreadID    string
	ActivityKind     string
	DurationMS       float64
	Questions        []asyncQuestion
}

type asyncQuestion struct {
	Title   string   `json:"title"`
	Options []string `json:"options"`
}

type webAction struct {
	Type    string   `json:"type"`
	Query   string   `json:"query"`
	Queries []string `json:"queries"`
	URL     string   `json:"url"`
	Pattern string   `json:"pattern"`
}

type collabState struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// fromExecItem normalizes an exec-stream item. Reasoning text prefers Text, falling back to Summary;
// both are emitted verbatim (untrimmed) to match the exec path's historical behavior.
func fromExecItem(it item) normItem {
	n := normItem{
		ID:            it.ID,
		Kind:          execKind(it.Type),
		rawType:       it.Type,
		Command:       it.Command,
		Cwd:           it.Cwd,
		Query:         it.Query,
		OutputKnown:   it.AggregatedOutput != nil,
		ExitCode:      it.ExitCode,
		Status:        it.Status,
		Server:        it.Server,
		Tool:          it.Tool,
		Changes:       it.Changes,
		Result:        it.Result,
		Error:         it.Error,
		Arguments:     it.Arguments,
		Success:       it.Success,
		MessagePhase:  it.Phase,
		Path:          it.Path,
		WebAction:     it.Action,
		Prompt:        it.Prompt,
		Agents:        it.Agents,
		AgentPath:     it.AgentPath,
		AgentThreadID: it.AgentThreadID,
		ActivityKind:  it.ActivityKind,
		DurationMS:    it.DurationMS,
	}
	if it.AggregatedOutput != nil {
		n.Output = *it.AggregatedOutput
	}
	switch n.Kind {
	case kindAgentMessage:
		n.Text = it.Text
	case kindReasoning:
		n.Text = it.Text
		if strings.TrimSpace(n.Text) == "" {
			n.Text = strings.Join(textBlocks(it.Summary), "\n\n")
		}
	case kindPlan:
		n.Text = it.Text
	case kindImageGeneration:
		n.Path, n.Error = it.SavedPath, imageFailure(it.Failure, it.Error)
	case kindCompaction:
		n.Text = agent.FirstNonEmpty(it.Text, strings.Join(textBlocks(it.Summary), "\n\n"))
	case kindError:
		n.Text = errorText(it.Error, agent.FirstNonEmpty(it.Message, it.Text, "Codex item failed."))
	}
	return n
}

// fromAsItem normalizes an app-server item. Reasoning content/summary arrive as string slices; they
// are joined and trimmed (the app-server path's historical behavior).
func fromAsItem(it asItem) normItem {
	n := normItem{
		ID:            it.ID,
		Kind:          asKind(it.Type),
		rawType:       it.Type,
		Command:       it.Command,
		Cwd:           it.Cwd,
		Query:         it.Query,
		OutputKnown:   it.Aggregated != nil,
		ExitCode:      it.ExitCode,
		Status:        it.Status,
		Server:        it.Server,
		Tool:          it.Tool,
		Changes:       it.Changes,
		Result:        it.Result,
		Error:         it.Error,
		Arguments:     it.Arguments,
		Success:       it.Success,
		MessagePhase:  it.Phase,
		Path:          it.Path,
		Review:        it.Review,
		WebAction:     it.Action,
		Prompt:        it.Prompt,
		Agents:        it.Agents,
		AgentPath:     it.AgentPath,
		AgentThreadID: it.AgentThreadID,
		ActivityKind:  it.ActivityKind,
		DurationMS:    it.DurationMS,
		Questions:     it.Questions,
	}
	if it.Aggregated != nil {
		n.Output = *it.Aggregated
	}
	switch n.Kind {
	case kindAgentMessage, kindPlan:
		n.Text = it.Text
	case kindReasoning:
		n.Content, n.Summary = textBlocks(it.Content), textBlocks(it.Summary)
		n.Text = reasoningText(n)
	case kindDynamicTool:
		n.Result = it.ContentItems
	case kindOutput:
		n.Tool, n.Result = it.Name, it.Output
	case kindWebSearch:
		n.Result = it.Results
	case kindImageGeneration:
		n.Path, n.Error = it.SavedPath, imageFailure(it.Failure, it.Error)
	case kindCompaction:
		n.Text = agent.FirstNonEmpty(it.Text, strings.Join(textBlocks(it.Summary), "\n\n"))
	case kindError:
		n.Text = errorText(it.Error, agent.FirstNonEmpty(it.Message, it.Text, "Codex item failed."))
	}
	return n
}

func reasoningText(n normItem) string {
	if text := strings.Join(n.Summary, "\n\n"); strings.TrimSpace(text) != "" {
		return text
	}
	return strings.Join(n.Content, "\n\n")
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
	case kindCollab:
		return agent.FirstNonEmpty(n.Tool, "Agent activity")
	case kindImage:
		return "view_image"
	case kindImageGeneration:
		return "image_generation"
	case kindOutput:
		return agent.FirstNonEmpty(n.Tool, "Tool output")
	default:
		return n.rawType
	}
}

// toolResult derives a completed tool's output text and error flag from its item shape. A "declined"
// status only occurs on the app-server transport; checking it here is harmless for the exec path.
func toolResult(n normItem) (string, bool) {
	isErr := n.Status == "failed" || n.Status == "declined" || (n.ExitCode != nil && *n.ExitCode != 0) || (n.Success != nil && !*n.Success)
	if message := errorText(n.Error, ""); message != "" {
		return message, true
	}
	switch n.Kind {
	case kindCommand:
		return n.Output, isErr
	case kindFileChange:
		return agent.FirstNonEmpty(summarizeChanges(n.Changes), n.Output), isErr
	case kindCollab:
		ids := make([]string, 0, len(n.Agents))
		for id := range n.Agents {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var lines []string
		for _, id := range ids {
			a := n.Agents[id]
			lines = append(lines, strings.TrimSpace(id+": "+a.Status+"\n"+a.Message))
			isErr = isErr || a.Status == "errored"
		}
		return strings.Join(lines, "\n\n"), isErr
	case kindImage:
		return n.Path, isErr
	case kindImageGeneration:
		if isErr {
			return "Image generation failed.", true
		}
		// The result is base64 image data. Display its saved path or a concise completion
		// label rather than sending binary content into the tool's text pane.
		return agent.FirstNonEmpty(n.Path, "Image generated."), false
	default:
		text, failed := renderOutput(n.Result)
		return text, isErr || failed
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

// codexToolAction normalizes a tool item into the unified agent.ToolAction without shared state.
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
			if c.MovePath != "" {
				files[len(files)-1].Path = c.MovePath
			}
		}
		title := ""
		if len(files) == 1 {
			title = files[0].Path
		}
		return &agent.ToolAction{Kind: agent.KindFileEdit, Title: title, Files: files}
	case kindWebSearch:
		if n.WebAction != nil {
			a := n.WebAction
			if a.URL != "" {
				return &agent.ToolAction{Kind: agent.KindWebFetch, Title: a.URL, Web: &agent.WebSearch{URL: a.URL, Query: a.Pattern}}
			}
			if n.Query == "" {
				n.Query = agent.FirstNonEmpty(a.Query, strings.Join(a.Queries, "; "))
			}
		}
		return &agent.ToolAction{Kind: agent.KindWebSearch, Title: n.Query, Web: &agent.WebSearch{Query: n.Query}}
	case kindMCPTool:
		return &agent.ToolAction{Kind: agent.KindMCP, Title: toolName(n), MCP: &agent.MCPCall{Server: n.Server, Tool: n.Tool}}
	case kindDynamicTool:
		if n.Tool != "" { // a dynamic tool is MCP-shaped when it names a tool
			return &agent.ToolAction{Kind: agent.KindMCP, Title: n.Tool, MCP: &agent.MCPCall{Tool: n.Tool}}
		}
		return &agent.ToolAction{Kind: agent.KindOther, Title: toolName(n)}
	case kindImage:
		return &agent.ToolAction{Kind: agent.KindFileRead, Title: n.Path, Files: []agent.FileChange{{Path: n.Path}}}
	case kindCollab:
		return &agent.ToolAction{Kind: agent.KindOther, Title: agent.FirstNonEmpty(n.Prompt, toolName(n))}
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
	seenUse                 map[string]bool
	emitted                 map[string]string
	items                   map[string]normItem
	raw                     map[string]json.RawMessage
	toolSnapshots           map[string]string
	toolResults             map[string]string
	interactionSnapshots    map[string]string
	completed               map[string]bool
	visible                 bool
	finalText               string
	finalAnswer             bool
	finalItemID             string
	decodeError             string
	turnDiff                string
	compactTrigger          string
	legacyCompactionPending bool
	modernCompactionPending bool
	terminalInputs          int
	problem                 string
	problemID               string
}

func newStreamState() *streamState {
	return &streamState{seenUse: map[string]bool{}, emitted: map[string]string{},
		items: map[string]normItem{}, raw: map[string]json.RawMessage{},
		toolSnapshots: map[string]string{}, toolResults: map[string]string{}, interactionSnapshots: map[string]string{}, completed: map[string]bool{}, compactTrigger: "auto"}
}

// Some server versions publish only an aggregate turn diff. Keep it visible when no item
// supplied per-file patches, and avoid duplicating the normal file-change components.
func (st *streamState) emitTurnDiff(turnID string, emit agent.Emit) {
	if st.turnDiff == "" {
		return
	}
	for _, item := range st.items {
		for _, change := range item.Changes {
			if change.Diff != "" {
				return
			}
		}
	}
	result, _ := json.Marshal(st.turnDiff)
	emitNorm(normItem{ID: "turn-diff-" + turnID, Kind: kindOther, rawType: "Changes", Result: result},
		phaseCompleted, nil, emit, st)
}

// emitNorm maps one normalized item at a lifecycle phase to unified events. Text and thinking stream
// incrementally (Delta=true) as the item grows across phases; a tool announces a tool_use once (first
// time its id is seen) and a tool_result at completion. raw is the original item JSON, preserved verbatim
// as the tool Input (forward-compatible). Returns the item's full text when a completed agent message
// (the running final answer), else "".
func emitNorm(n normItem, phase itemPhase, raw json.RawMessage, emit agent.Emit, st *streamState) string {
	if n.ID != "" {
		if n.Kind == kindAgentMessage && n.MessagePhase == "" {
			n.MessagePhase = st.items[n.ID].MessagePhase
		}
		if !n.OutputKnown && (n.Kind == kindCommand || n.Kind == kindFileChange) {
			n.Output = agent.FirstNonEmpty(n.Output, st.items[n.ID].Output)
		}
		st.items[n.ID] = n
		if len(raw) > 0 {
			st.raw[n.ID] = raw
		}
	}
	switch n.Kind {
	case kindAgentMessage:
		st.streamText(n, agent.EventText, phase, emit)
		for i, question := range n.Questions {
			if strings.TrimSpace(question.Title) == "" {
				continue
			}
			st.visible = true
			// Async questions have no server-request ID. Show their content without inventing
			// an approval or a /respond action; a normal chat message can answer them.
			st.emitInteraction(&agent.Interaction{ID: fmt.Sprintf("%s:question:%d", n.ID, i), Kind: "info",
				Title: question.Title, Detail: strings.Join(question.Options, "\n"),
				Meta: map[string]any{"source": "codex", "asyncQuestion": true}}, emit)
		}
		if n.MessagePhase == "final_answer" || !st.finalAnswer || n.ID != "" && n.ID == st.finalItemID {
			st.finalText, st.finalItemID = n.Text, n.ID
			st.finalAnswer = n.MessagePhase == "final_answer"
		}
		if phase == phaseCompleted {
			if n.ID != "" {
				st.completed[n.ID] = true
			}
			return st.finalText
		}
	case kindReasoning:
		st.streamText(n, agent.EventThinking, phase, emit)
	case kindPlan:
		if strings.TrimSpace(n.Text) != "" {
			st.visible = true
			st.emitInteraction(&agent.Interaction{ID: n.ID, Kind: "plan", Title: "Plan", Detail: n.Text}, emit)
		}
	case kindReview:
		if phase == phaseCompleted {
			title := "Review started"
			if n.rawType == "exitedReviewMode" {
				title = "Review complete"
			}
			st.visible = true
			st.emitInteraction(&agent.Interaction{ID: n.ID, Kind: "info", Title: title, Detail: n.Review}, emit)
		}
	case kindSubAgentActivity:
		st.visible = true
		title := map[string]string{"started": "Agent started", "interacted": "Agent updated", "interrupted": "Agent interrupted", "completed": "Agent finished"}[n.ActivityKind]
		st.emitInteraction(&agent.Interaction{ID: n.ID, Kind: "info", Title: agent.FirstNonEmpty(title, "Agent activity"),
			Detail: agent.FirstNonEmpty(n.AgentPath, n.AgentThreadID),
			Meta:   map[string]any{"source": "codex", "agentThreadId": n.AgentThreadID, "status": n.ActivityKind}}, emit)
	case kindSleep:
		st.visible = true
		title := "Waiting"
		if phase == phaseCompleted {
			title = "Wait finished"
		}
		st.emitInteraction(&agent.Interaction{ID: n.ID, Kind: "info", Title: title,
			Detail: fmt.Sprintf("%g seconds", n.DurationMS/1000)}, emit)
	case kindCompaction:
		if phase == phaseCompleted {
			st.emitCompaction(n.ID, n.Text, false, emit)
		}
	case kindError:
		st.emitInteraction(&agent.Interaction{ID: n.ID, Kind: "error", Title: "Codex error", Detail: n.Text,
			Meta: map[string]any{"source": "codex"}}, emit)
	case kindIgnored:
		return ""
	default:
		st.visible = true
		st.emitTool(n, phase, raw, emit)
	}
	if phase == phaseCompleted && n.ID != "" {
		st.completed[n.ID] = true
	}
	return ""
}

// Interactions are snapshots, just like tools. A repeated final turn must not append
// a second plan, warning, or activity card, while a changed snapshot keeps the same ID.
func (st *streamState) emitInteraction(inter *agent.Interaction, emit agent.Emit) {
	if inter.Kind == "error" {
		st.problem, st.problemID = inter.Detail, inter.ID
	} else if inter.ID != "" && inter.ID == st.problemID {
		st.problem, st.problemID = "", ""
	}
	encoded, _ := json.Marshal(inter)
	if inter.ID != "" {
		if st.interactionSnapshots[inter.ID] == string(encoded) {
			return
		}
		st.interactionSnapshots[inter.ID] = string(encoded)
	}
	emit(agent.Event{Type: agent.EventInteraction, Interaction: inter})
}

func (st *streamState) decodeFailure(message string, emit agent.Emit) {
	st.decodeError = message
	st.emitInteraction(&agent.Interaction{ID: "codex:decode-error", Kind: "error", Title: "Codex output error", Detail: message,
		Meta: map[string]any{"source": "codex"}}, emit)
}

func (st *streamState) emitCompaction(id, summary string, legacy bool, emit agent.Emit) {
	if id != "" && st.completed[id] {
		return
	}
	if id != "" {
		st.completed[id] = true
	}
	// Older servers can send both representations for one boundary, in either order.
	if legacy && st.modernCompactionPending {
		st.modernCompactionPending = false
		return
	}
	if !legacy && st.legacyCompactionPending {
		st.legacyCompactionPending = false
		return
	}
	st.legacyCompactionPending, st.modernCompactionPending = legacy, !legacy
	st.visible = true
	emit(agent.Event{Type: agent.EventCompaction, ItemID: id,
		Compaction: &agent.CompactionInfo{Trigger: st.compactTrigger, Summary: summary}})
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
	} else if prev := st.emitted[n.ID]; full != prev {
		// A completed item is authoritative, including edits/shortening. Item identity lets
		// clients replace the correct block instead of appending a corrupt suffix.
		if strings.TrimSpace(full) != "" || prev != "" {
			delta := strings.HasPrefix(full, prev)
			text := full
			if delta {
				text = full[len(prev):]
			}
			emit(agent.Event{Type: evType, ItemID: n.ID, Text: text, Delta: delta})
			st.emitted[n.ID] = full
		}
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
	if len(raw) == 0 {
		raw = st.raw[n.ID]
	}
	action := codexToolAction(n)
	snapshot, _ := json.Marshal(action)
	key := string(snapshot) + n.Output
	if !st.seenUse[n.ID] || (phase != phaseCompleted && st.toolSnapshots[n.ID] != key) {
		st.seenUse[n.ID] = true
		st.toolSnapshots[n.ID] = key
		emit(agent.Event{Type: agent.EventToolUse,
			Tool: &agent.ToolEvent{ID: n.ID, Name: name, Input: json.RawMessage(raw), Output: n.Output, Action: action}})
	}
	if phase == phaseCompleted {
		st.completed[n.ID] = true
		out, isErr := toolResult(n)
		tool := &agent.ToolEvent{ID: n.ID, Name: name, Input: json.RawMessage(raw), Output: out, IsError: isErr, Action: action}
		encoded, _ := json.Marshal(tool)
		if n.ID == "" || st.toolResults[n.ID] != string(encoded) {
			st.toolResults[n.ID] = string(encoded)
			emit(agent.Event{Type: agent.EventToolResult, Tool: tool})
		}
	}
}

// emitDelta folds the documented app-server delta notifications into the same item model
// as started/completed snapshots. Final snapshots then deduplicate or correct the preview.
func (st *streamState) emitDelta(method string, raw json.RawMessage, emit agent.Emit) {
	var p struct {
		ItemID       string       `json:"itemId"`
		Delta        string       `json:"delta"`
		SummaryIndex int          `json:"summaryIndex"`
		ContentIndex int          `json:"contentIndex"`
		Changes      []normChange `json:"changes"`
		Message      string       `json:"message"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.ItemID == "" {
		return
	}
	if st.completed[p.ItemID] {
		return
	}
	n := st.items[p.ItemID]
	n.ID = p.ItemID
	appendBlock := func(blocks []string, index int, delta string) []string {
		if index < 0 || index > 10000 {
			return blocks
		}
		for len(blocks) <= index {
			blocks = append(blocks, "")
		}
		blocks[index] += delta
		return blocks
	}
	switch method {
	case "item/agentMessage/delta":
		n.Kind, n.rawType = kindAgentMessage, "agentMessage"
		n.Text += p.Delta
	case "item/plan/delta":
		n.Kind, n.rawType = kindPlan, "plan"
		n.Text += p.Delta
	case "item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded":
		n.Kind, n.rawType = kindReasoning, "reasoning"
		n.Summary = appendBlock(n.Summary, p.SummaryIndex, p.Delta)
		n.Text = reasoningText(n)
	case "item/reasoning/textDelta":
		n.Kind, n.rawType = kindReasoning, "reasoning"
		n.Content = appendBlock(n.Content, p.ContentIndex, p.Delta)
		n.Text = reasoningText(n)
	case "item/commandExecution/outputDelta":
		n.Kind, n.rawType = kindCommand, "commandExecution"
		n.Output += p.Delta
	case "item/fileChange/patchUpdated":
		n.Kind, n.rawType = kindFileChange, "fileChange"
		n.Changes = p.Changes
	case "item/fileChange/outputDelta":
		n.Kind, n.rawType = kindFileChange, "fileChange"
		n.Output += p.Delta
	case "item/mcpToolCall/progress":
		n.Kind, n.rawType = kindMCPTool, "mcpToolCall"
		n.Output = p.Message
	default:
		return
	}
	emitNorm(n, phaseUpdated, nil, emit, st)
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
		if status == "inProgress" {
			status = "in_progress"
		}
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
