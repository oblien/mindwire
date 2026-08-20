package codex

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// parse.go maps Codex's `codex exec --json` NDJSON stream (snake_case) to unified events. The stream
// is a flat sequence of envelopes discriminated by a top-level `type`:
//
//	thread.started   {thread_id}                      → EventSession (session id)
//	turn.started                                      → (lifecycle marker, no event)
//	item.started|updated|completed  {item:{type,…}}   → text / thinking / tool / todos
//	turn.completed   {usage:{…tokens…}}               → EventResult (tokens in Meta; Codex has no USD)
//	turn.failed      {error:{message}}                → terminal EventResult (IsError)
//	error            {message}                        → EventError (mid-stream; not terminal by itself)
//
// item.type is a second discriminator (agent_message/reasoning/command_execution/file_change/
// mcp_tool_call/web_search/todo_list). Unknown top-level and item types are ignored — Codex ships
// fast and the stream is not a stable contract, so the parser is deliberately forward-compatible.
//
// Note: text and reasoning are emitted as Delta=true events via the shared streamText tail
// (normalize.go): incrementally when the stream sends cumulative item.updated frames, otherwise as a
// single whole-block delta at item.completed. Either way each byte is emitted exactly once, so the
// runner's accumulated text equals the final answer — matching claude's streaming discipline.

type streamEnvelope struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     json.RawMessage `json:"item"`
	Usage    *usage          `json:"usage"`
	Error    *streamError    `json:"error"`
	Message  string          `json:"message"`
}

type usage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

type streamError struct {
	Message string `json:"message"`
}

// item is one ThreadItem. Fields are the union across item types; only the relevant ones are set for
// a given type. Unrecognized fields are ignored (forward-compatible).
type item struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Text             string          `json:"text"`    // agent_message / reasoning
	Summary          string          `json:"summary"` // reasoning (alt)
	Command          string          `json:"command"` // command_execution
	Status           string          `json:"status"`  // in_progress | completed | failed
	ExitCode         *int            `json:"exit_code"`
	AggregatedOutput string          `json:"aggregated_output"`
	Cwd              string          `json:"cwd"`
	Query            string          `json:"query"`  // web_search
	Server           string          `json:"server"` // mcp_tool_call
	Tool             string          `json:"tool"`   // mcp_tool_call
	Changes          []normChange    `json:"changes"`
	Items            json.RawMessage `json:"items"`  // todo_list
	Result           json.RawMessage `json:"result"` // mcp_tool_call
}

// parseStream consumes the NDJSON stream, emits unified events, and returns the terminal result plus
// whether one was seen (got=false ⇒ the driver surfaces stderr/exit as the error).
func parseStream(r io.Reader, emit agent.Emit) (agent.TurnResult, bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20) // rollout/tool output lines can be large

	var result agent.TurnResult
	var sessionID, finalText string
	got := false
	st := newStreamState() // tool-use dedup + per-item emitted-length for streaming text/thinking deltas

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var env streamEnvelope
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		switch env.Type {
		case "thread.started":
			if env.ThreadID != "" {
				sessionID = env.ThreadID
				result.SessionID = sessionID
				emit(agent.Event{Type: agent.EventSession, SessionID: sessionID})
			}
		case "turn.started":
			// lifecycle marker; nothing to surface

		case "item.started", "item.updated", "item.completed":
			if len(env.Item) == 0 {
				continue
			}
			var it item
			if json.Unmarshal(env.Item, &it) != nil {
				continue
			}
			finalText = handleItem(execPhase(env.Type), env.Item, it, emit, st, finalText)

		case "turn.completed":
			got = true
			result.Text, result.SessionID = finalText, sessionID
			ev := agent.Event{Type: agent.EventResult, SessionID: sessionID,
				Result: &agent.ResultInfo{Text: finalText, SessionID: sessionID}}
			if env.Usage != nil {
				u := tokenUsage{
					InputTokens:           env.Usage.InputTokens,
					CachedInputTokens:     env.Usage.CachedInputTokens,
					OutputTokens:          env.Usage.OutputTokens,
					ReasoningOutputTokens: env.Usage.ReasoningOutputTokens,
				}
				// Codex reports tokens, not USD — carry them in Meta (unchanged) AND, additively, as the
				// typed per-turn Usage on the result.
				ev.Meta = usageMeta(u)
				ev.Result.Usage = usageStruct(u)
			}
			emit(ev)

		case "turn.failed":
			got = true
			msg := "codex turn failed"
			if env.Error != nil && strings.TrimSpace(env.Error.Message) != "" {
				msg = env.Error.Message
			}
			result = agent.TurnResult{Text: msg, SessionID: sessionID, IsError: true}
			emit(agent.Event{Type: agent.EventResult, SessionID: sessionID,
				Result: &agent.ResultInfo{Text: msg, IsError: true, SessionID: sessionID}})

		case "error":
			// Mid-stream error (e.g. an auth/refresh failure). Surface it as an error event; a
			// turn.failed usually follows and is the terminal result. If none does, got stays false and
			// the driver surfaces this as the turn's error.
			msg := strings.TrimSpace(env.Message)
			if msg == "" {
				msg = "codex error"
			}
			emit(agent.Event{Type: agent.EventError, SessionID: sessionID, Error: msg})
		}
	}

	if err := sc.Err(); err != nil {
		return agent.TurnResult{Text: "stream read error: " + err.Error()}, false
	}
	if !got {
		return agent.TurnResult{Text: "no result from agent"}, false
	}
	return result, true
}

// handleItem maps one item envelope (at a given lifecycle phase) to events, returning the updated
// running final-answer text (the last agent_message wins). Text/thinking/tool items normalize into
// normItem and share the streaming emit tail with the app-server transport (see normalize.go); todo_list
// is exec-only, so its decode stays here (feeding the shared buildTodos).
func handleItem(phase itemPhase, raw json.RawMessage, it item, emit agent.Emit, st *streamState, finalText string) string {
	if it.Type == "todo_list" {
		if phase == phaseCompleted {
			if inter := todosInteraction(it); inter != nil {
				emit(agent.Event{Type: agent.EventInteraction, Interaction: inter})
			}
		}
		return finalText
	}
	if txt := emitNorm(fromExecItem(it), phase, raw, emit, st); txt != "" {
		return txt
	}
	return finalText
}

// todosInteraction maps a todo_list item to a unified todos interaction. Item shape varies across
// versions, so field names are matched defensively (content/text/step; status/completed) before the
// shared buildTodos tail.
func todosInteraction(it item) *agent.Interaction {
	if len(it.Items) == 0 {
		return nil
	}
	var raw []struct {
		Text      string `json:"text"`
		Content   string `json:"content"`
		Step      string `json:"step"`
		Status    string `json:"status"`
		Completed *bool  `json:"completed"`
	}
	if json.Unmarshal(it.Items, &raw) != nil {
		return nil
	}
	rows := make([]todoRow, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, todoRow{
			Content:   agent.FirstNonEmpty(r.Content, r.Text, r.Step),
			Status:    r.Status,
			Completed: r.Completed,
		})
	}
	return buildTodos(rows)
}
