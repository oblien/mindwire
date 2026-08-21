package claude

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// streamEnvelope is the union of fields across Claude's stream-json event types.
// json.RawMessage fields keep unrelated shapes from failing the whole-line decode.
type streamEnvelope struct {
	Type         string          `json:"type"`
	Subtype      string          `json:"subtype"`
	SessionID    string          `json:"session_id"`
	Model        string          `json:"model"`
	Tools        json.RawMessage `json:"tools"`
	Attempt      int             `json:"attempt"`
	Event        json.RawMessage `json:"event"`   // stream_event partial delta
	Message      json.RawMessage `json:"message"` // assistant / user full message
	Result       string          `json:"result"`
	IsError      bool            `json:"is_error"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	NumTurns     int             `json:"num_turns"`
	DurationMS   int             `json:"duration_ms"`
	// Control protocol (persistent transport): a CLI→client control_request carries a top-level
	// request_id (the correlator the client must echo in its control_response) and the request body.
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
	// EstimatedTokens is the CUMULATIVE thinking-token estimate on a `system/thinking_tokens` event
	// (Claude Code's own "thinking preview" counter). Present even on subscription auth where the
	// thinking TEXT is withheld, so it's the reliable live progress signal for the thinking block.
	EstimatedTokens int `json:"estimated_tokens"`
	// Content + CompactMetadata ride on a `system/compact_boundary` event: content is the human line
	// ("Conversation compacted") and compactMetadata carries the trigger (auto|manual) and token counts.
	Content         string          `json:"content"`
	CompactMetadata json.RawMessage `json:"compactMetadata"`
	// Usage is the top-level per-turn token accounting on the terminal `result` event. Claude reports
	// input/output plus cache creation+read; there is no reasoning breakdown or grand total.
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// parseStream scans Claude's NDJSON, emits unified events, and returns the final
// TurnResult (got=false if no result line was seen).
func parseStream(r io.Reader, emit agent.Emit) (agent.TurnResult, bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20) // assistant messages can be large

	var sessionID string
	var result agent.TurnResult
	got := false
	// Claude's tool_result event carries no tool name (only id+output+isError), so stash each tool_use
	// block by id to reclassify the result into the same ToolAction with its output folded in.
	use := map[string]block{}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var env streamEnvelope
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		if env.SessionID != "" {
			sessionID = env.SessionID
		}

		switch env.Type {
		case "system":
			switch env.Subtype {
			case "init":
				meta := map[string]any{}
				if env.Model != "" {
					meta["model"] = env.Model
				}
				if len(env.Tools) > 0 {
					meta["tools"] = env.Tools
					// Learn the agent's real built-in tool set (drives the tools picker).
					rememberTools(toolNames(env.Tools))
				}
				emit(agent.Event{Type: agent.EventSession, SessionID: sessionID, Meta: meta})
			case "api_retry":
				emit(agent.Event{Type: agent.EventStatus, SessionID: sessionID,
					Meta: map[string]any{"retry": true, "attempt": env.Attempt}})
			case "thinking_tokens":
				// Claude Code's thinking-preview counter — surface the running estimate so the UI can
				// show "Thinking · N tokens" live (and keep it on the finished block). Text stays empty.
				emit(agent.Event{Type: agent.EventThinking, Delta: true, Tokens: env.EstimatedTokens, SessionID: sessionID})
			case "compact_boundary":
				// The conversation was compacted mid-turn (auto at the window limit, or a manual
				// /compact). Surface it as a unified compaction event so the client SEES the boundary;
				// the runner persists it as a "compaction" part and the transcript reader reproduces it.
				emit(agent.Event{Type: agent.EventCompaction, SessionID: sessionID,
					Compaction: compactionFromMeta(env.Content, env.CompactMetadata)})
			}

		case "stream_event":
			if t := textDelta(env.Event); t != "" {
				emit(agent.Event{Type: agent.EventText, Text: t, Delta: true, SessionID: sessionID})
			}
			// Thinking START: the thinking content block OPENS. On subscription/OAuth auth the
			// thinking text is withheld ("omitted" mode — only content_block_start → signature_delta →
			// content_block_stop, no thinking_delta), so this open is the ONLY reliable "is thinking"
			// signal. Emit an empty EventThinking to open the thinking run; the client/runner times it
			// (start here → first text) so "Thinking… / Thought for Ns" is driven by the agent, not UI.
			if thinkingBlockStart(env.Event) {
				emit(agent.Event{Type: agent.EventThinking, Delta: true, SessionID: sessionID})
			}
			if th := thinkingDelta(env.Event); th != "" {
				emit(agent.Event{Type: agent.EventThinking, Text: th, Delta: true, SessionID: sessionID})
			}

		case "assistant":
			for _, b := range contentBlocks(env.Message) {
				if b.Type != "tool_use" {
					continue
				}
				// Some tools are really structured prompts to the user (todos, a proposed
				// plan, a multiple-choice question) — surface them as generic interactions
				// the client renders, not raw tool blobs.
				if it := interactionFor(b); it != nil {
					emit(agent.Event{Type: agent.EventInteraction, SessionID: sessionID, Interaction: it})
					continue
				}
				use[b.ID] = b
				emit(agent.Event{Type: agent.EventToolUse, SessionID: sessionID,
					Tool: &agent.ToolEvent{ID: b.ID, Name: b.Name, Input: b.Input,
						Action: claudeToolAction(b.Name, b.Input, "", false)}})
			}

		case "user":
			for _, b := range contentBlocks(env.Message) {
				if b.Type == "tool_result" {
					out := rawText(b.Content)
					// Reclassify from the originating tool_use (which carries the name+input); if it's
					// missing (out-of-order/id collision) leave Action nil so the runner keeps the
					// input-derived action from the tool_use rather than a misclassified one.
					var action *agent.ToolAction
					if u, ok := use[b.ToolUseID]; ok {
						action = claudeToolAction(u.Name, u.Input, out, b.IsError)
					}
					emit(agent.Event{Type: agent.EventToolResult, SessionID: sessionID,
						Tool: &agent.ToolEvent{ID: b.ToolUseID, Output: out, IsError: b.IsError, Action: action}})
				}
			}

		case "result":
			result = agent.TurnResult{Text: env.Result, SessionID: sessionID, IsError: env.IsError, Subtype: env.Subtype}
			got = true
			ri := &agent.ResultInfo{
				Text: env.Result, IsError: env.IsError, SessionID: sessionID,
				CostUSD: env.TotalCostUSD, NumTurns: env.NumTurns, DurationMS: env.DurationMS,
				Subtype: env.Subtype, Incomplete: agent.Continuable(env.Subtype),
			}
			// Best-effort per-turn tokens from the result's top-level usage object. Claude has no
			// reasoning breakdown (leave 0) and no grand total, so Total is the component sum. Attach a
			// non-nil Usage only when at least one component is reported (keeps the field omitempty).
			if u := env.Usage; u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadInputTokens > 0 || u.CacheCreationInputTokens > 0 {
				ri.Usage = &agent.Usage{
					InputTokens:      u.InputTokens,
					OutputTokens:     u.OutputTokens,
					CacheReadTokens:  u.CacheReadInputTokens,
					CacheWriteTokens: u.CacheCreationInputTokens,
					TotalTokens:      u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens,
				}
			}
			emit(agent.Event{Type: agent.EventResult, SessionID: sessionID, Result: ri})

		case "control_request":
			// The CLI asks the client to authorize a tool (persistent transport, non-bypass mode).
			// Surface it as a unified approval interaction the user answers via POST /runs/{id}/respond.
			if it := approvalInteraction(env.RequestID, env.Request); it != nil {
				emit(agent.Event{Type: agent.EventInteraction, SessionID: sessionID, Interaction: it})
			}
		}
	}

	if !got {
		text := "no result from agent"
		if err := sc.Err(); err != nil {
			// A read error (e.g. a line over the 16 MiB cap) ended scanning early; report it
			// instead of the generic "no result" so the failure isn't silently misattributed.
			text = "stream read error: " + err.Error()
		}
		result = agent.TurnResult{SessionID: sessionID, IsError: true, Text: text}
	}
	return result, got
}

// compactionFromMeta builds a unified CompactionInfo from a compact_boundary event's content line and
// its compactMetadata object ({trigger, preTokens, postTokens, …}). Best-effort: unknown/absent fields
// stay zero-valued. Shared by the live parser and the transcript reader (both see the same shape).
func compactionFromMeta(content string, meta json.RawMessage) *agent.CompactionInfo {
	ci := &agent.CompactionInfo{}
	if len(meta) > 0 {
		var m struct {
			Trigger    string `json:"trigger"`
			PreTokens  int    `json:"preTokens"`
			PostTokens int    `json:"postTokens"`
		}
		if json.Unmarshal(meta, &m) == nil {
			ci.Trigger, ci.PreTokens, ci.PostTokens = m.Trigger, m.PreTokens, m.PostTokens
		}
	}
	return ci
}

// block is one Anthropic message content block (text / tool_use / tool_result).
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"` // thinking block body (native transcript)
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"` // tool_result content (string or blocks)
}

// contentBlocks extracts the content blocks from an Anthropic message (content may
// be a JSON string or an array of blocks).
func contentBlocks(raw json.RawMessage) []block {
	if len(raw) == 0 {
		return nil
	}
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil || len(m.Content) == 0 {
		return nil
	}
	var blocks []block
	if json.Unmarshal(m.Content, &blocks) == nil {
		return blocks
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	return nil
}

// messageText joins the text blocks of an Anthropic message (used for history).
func messageText(raw json.RawMessage) string {
	var sb strings.Builder
	for _, b := range contentBlocks(raw) {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// messageParts folds an Anthropic message's content blocks into ordered rich parts (text / thinking
// / tool) for the reload transcript, using the SAME paired 'tool' shape the live runner produces.
// The native transcript has no per-block timing, so DurationMs stays 0 (the app shows "Thought"
// without seconds for reloaded turns). tool_result blocks (Claude records them as role "user")
// become tool parts here and are folded onto the preceding assistant message in history.go.
func messageParts(raw json.RawMessage) []agent.Part {
	var parts []agent.Part
	for _, b := range contentBlocks(raw) {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, agent.Part{Type: "text", Text: b.Text})
			}
		case "thinking":
			if b.Thinking != "" {
				parts = append(parts, agent.Part{Type: "thinking", Text: b.Thinking})
			}
		case "tool_use":
			// Interaction tools (TodoWrite / ExitPlanMode / AskUserQuestion) reload as interaction
			// parts so the transcript matches the live stream; ordinary tools stay tool parts.
			if it := interactionFor(b); it != nil {
				parts = append(parts, agent.Part{Type: "interaction", Interaction: it})
			} else {
				// Input-only action now; mergeToolResults (history.go) recomputes it with the folded
				// output once the following "user" record's tool_result is paired in.
				parts = append(parts, agent.Part{Type: "tool",
					Tool: &agent.ToolPart{ID: b.ID, Name: b.Name, Input: b.Input,
						Action: claudeToolAction(b.Name, b.Input, "", false)}})
			}
		case "tool_result":
			parts = append(parts, agent.Part{Type: "tool",
				Tool: &agent.ToolPart{ID: b.ToolUseID, Output: rawText(b.Content), IsError: b.IsError}})
		}
	}
	return parts
}

// rawText extracts text from a value that may be a JSON string or array of blocks.
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	return ""
}

// toolNames pulls tool names from the init event's "tools" field, which is an array of
// strings (["Bash","Edit",…]); tolerant of an array-of-objects shape just in case.
func toolNames(raw json.RawMessage) []string {
	var names []string
	if json.Unmarshal(raw, &names) == nil {
		return names
	}
	var objs []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &objs) == nil {
		for _, o := range objs {
			if o.Name != "" {
				names = append(names, o.Name)
			}
		}
	}
	return names
}

// interactionFor maps Claude tools that are really user-facing prompts onto unified
// interactions. Returns nil for ordinary tools (which stay tool_use events).
func interactionFor(b block) *agent.Interaction {
	switch b.Name {
	case "TodoWrite":
		return todosInteraction(b.ID, b.Input)
	case "ExitPlanMode":
		return planInteraction(b.ID, b.Input)
	case "AskUserQuestion":
		return questionInteraction(b.ID, b.Input)
	}
	return nil
}

// planInteraction maps an ExitPlanMode tool input ({"plan":"…markdown…"}) to a plan
// interaction the user can approve. Returns nil if there's no plan text.
func planInteraction(id string, input json.RawMessage) *agent.Interaction {
	var in struct {
		Plan string `json:"plan"`
	}
	if json.Unmarshal(input, &in) != nil || strings.TrimSpace(in.Plan) == "" {
		return nil
	}
	return &agent.Interaction{
		ID: id, Kind: "plan", Title: "Proposed plan", Detail: in.Plan, NeedsResponse: true,
		Options: []agent.Action{{ID: "approve", Label: "Approve & continue"}, {ID: "reject", Label: "Reject"}},
		// ExitPlanMode is a normal tool: the user's answer is injected as a tool_result keyed by this id.
		Meta: map[string]any{"respondVia": respondViaToolResult, "toolUseId": id},
	}
}

// approvalInteraction maps a `can_use_tool` control_request (persistent transport, non-bypass
// permission mode) to a unified approval interaction. The correlators live in Meta and are echoed
// back on the Inbound so the adapter's Encode can build the control_response without server state:
// ID is the control-request id the client must echo, and toolUseId ties it to the emitted tool_use.
// Returns nil for any other control subtype (e.g. the initialize ack, which the parser ignores).
func approvalInteraction(requestID string, raw json.RawMessage) *agent.Interaction {
	var req struct {
		Subtype     string `json:"subtype"`
		ToolName    string `json:"tool_name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		ToolUseID   string `json:"tool_use_id"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &req) != nil || req.Subtype != "can_use_tool" {
		return nil
	}
	name := req.DisplayName
	if name == "" {
		name = req.ToolName
	}
	return &agent.Interaction{
		ID: requestID, Kind: "approval", Title: "Allow " + name + "?", Detail: req.Description,
		Options:       []agent.Action{{ID: "allow", Label: "Allow"}, {ID: "deny", Label: "Deny"}},
		NeedsResponse: true,
		// can_use_tool is answered over the control channel (control_response), NOT as a tool_result.
		Meta: map[string]any{"respondVia": respondViaControl, "toolUseId": req.ToolUseID, "toolName": req.ToolName},
	}
}

// questionInteraction maps an AskUserQuestion tool input to a choice/select interaction.
// Claude's shape: {"questions":[{question,header,multiSelect,options:[{label,description}]}]}.
func questionInteraction(id string, input json.RawMessage) *agent.Interaction {
	var in struct {
		Questions []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(input, &in) != nil || len(in.Questions) == 0 {
		return nil
	}
	q := in.Questions[0]
	kind := "choice"
	if q.MultiSelect {
		kind = "select"
	}
	opts := make([]agent.Action, 0, len(q.Options))
	for _, o := range q.Options {
		if o.Label != "" {
			opts = append(opts, agent.Action{ID: o.Label, Label: o.Label})
		}
	}
	return &agent.Interaction{
		ID: id, Kind: kind, Title: q.Question, Detail: q.Header, Options: opts, NeedsResponse: true,
		// AskUserQuestion is a normal tool: the answer is injected as a tool_result keyed by this id.
		Meta: map[string]any{"respondVia": respondViaToolResult, "toolUseId": id},
	}
}

// todosInteraction maps a TodoWrite tool input ({"todos":[{content,status,…}]}) to a
// unified todos interaction. Returns nil if the input has no todos.
func todosInteraction(id string, input json.RawMessage) *agent.Interaction {
	var in struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal(input, &in) != nil || len(in.Todos) == 0 {
		return nil
	}
	items := make([]agent.TodoItem, 0, len(in.Todos))
	for _, t := range in.Todos {
		items = append(items, agent.TodoItem{Content: t.Content, Status: t.Status})
	}
	return &agent.Interaction{ID: id, Kind: "todos", Title: "To-dos", Items: items}
}

// textDelta pulls the text out of a streaming content_block_delta partial event.
func textDelta(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var ev struct {
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return ""
	}
	if ev.Delta.Type == "text_delta" {
		return ev.Delta.Text
	}
	return ""
}

// thinkingDelta pulls extended-thinking text out of a streaming content_block_delta.
// thinkingBlockStart reports whether a stream event is a content_block_start opening a THINKING
// block. This fires even when the thinking text is omitted (OAuth), so it's the reliable "thinking
// began" marker — unlike thinkingDelta, which is empty in that mode.
func thinkingBlockStart(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var ev struct {
		Type         string `json:"type"`
		ContentBlock struct {
			Type string `json:"type"`
		} `json:"content_block"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return false
	}
	return ev.Type == "content_block_start" && ev.ContentBlock.Type == "thinking"
}

func thinkingDelta(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var ev struct {
		Delta struct {
			Type     string `json:"type"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return ""
	}
	if ev.Delta.Type == "thinking_delta" {
		return ev.Delta.Thinking
	}
	return ""
}
