package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// history.go serves native History by reading Codex's own rollout transcript (the agent owns its
// data, exactly like the claude adapter reading ~/.claude/projects/**). A session id maps
// deterministically to its file — the id is embedded in the filename (rollout-<ts>-<id>.jsonl) and
// repeated in the header — so no index is needed; we glob the sessions tree (and archived_sessions,
// where v0.136+ moves old sessions) for the matching suffix.
//
// The rollout is NDJSON of {timestamp, type, payload} envelopes. We normalize the transcript into
// unified messages: user/assistant text from `event_msg`, model reasoning and tool activity from
// `response_item`, pairing each tool call to its output by call_id. Best-effort and version-tolerant:
// unknown envelope/payload types are skipped, and any miss returns nil/err so the caller falls back
// to mindwire's own recorded `--json` stream (which an --ephemeral run leaves as the only source).
//
// Codex has no verified per-session title record in the rollout, so this adapter implements no
// Titler — the API keeps its first-user-message-snippet fallback.

// The adapter owns Codex's rollout transcript, so it can both read and delete it.
var _ agent.HistoryDeleter = adapter{}

// DeleteHistory removes Codex's native rollout transcript for a session — the rollout-<ts>-<id>.jsonl
// file findRollout locates under $CODEX_HOME (live or archived). Best-effort and idempotent: a missing
// session id, unresolvable home, or no rollout on disk (e.g. an --ephemeral run) is not an error, so a
// true chat delete never fails just because the agent left nothing behind. The bool reports whether a
// file was ACTUALLY removed, so the caller only claims `nativePurged` for a rollout that truly existed.
//
// This implements agent.HistoryDeleter; the API's DELETE /chats/{id} drives it per session.
func (adapter) DeleteHistory(q agent.HistoryQuery) (bool, error) {
	if q.SessionID == "" {
		return false, nil
	}
	base := configBase()
	if base == "" {
		return false, nil
	}
	path := findRollout(base, q.SessionID)
	if path == "" {
		return false, nil // no rollout on disk — nothing to delete
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil // raced away between find and remove — treat as absent
		}
		return false, err
	}
	return true, nil
}

// History resolves a session id to its rollout file and maps it to unified messages.
func (adapter) History(q agent.HistoryQuery) ([]agent.Message, error) {
	if q.SessionID == "" {
		return nil, nil
	}
	base := configBase()
	if base == "" {
		return nil, nil // can't resolve home; caller falls back to the recorded log
	}
	path := findRollout(base, q.SessionID)
	if path == "" {
		return nil, nil // no rollout on disk (e.g. --ephemeral); caller falls back
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	messages, err := parseRollout(f, q.ChatID)
	if err != nil {
		return nil, err
	}
	return mergeRecordedHistory(messages, q.Recorded), nil
}

// errStopWalk halts filepath.WalkDir once the target rollout is found (returning a sentinel is the
// version-safe way to short-circuit a walk).
var errStopWalk = errors.New("stop")

// findRollout locates the rollout file for a session id under $CODEX_HOME, searching the live tree
// first and the archived tree second. The filename is rollout-<timestamp>-<id>.jsonl, so the id is a
// unique filename suffix. Returns "" if not found.
func findRollout(base, sessionID string) string {
	suffix := "-" + sessionID + ".jsonl"
	for _, root := range []string{filepath.Join(base, "sessions"), filepath.Join(base, "archived_sessions")} {
		found := ""
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable dir/entry — skip, don't abort the whole walk
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, suffix) {
				found = p
				return errStopWalk
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// rollout envelope + payload shapes (only the fields we map; unknown ones are ignored).

type rolloutEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"` // session_meta | event_msg | response_item | turn_context | compacted | token_count
	Payload   json.RawMessage `json:"payload"`
}

type eventMsgPayload struct {
	Type             string          `json:"type"` // user_message | agent_message | …
	Message          string          `json:"message"`
	Item             json.RawMessage `json:"item"`
	Error            json.RawMessage `json:"error"`
	LastAgentMessage string          `json:"last_agent_message"`
	StartedAtMS      int64           `json:"started_at_ms"`
	CompletedAtMS    int64           `json:"completed_at_ms"`
}

type responseItemPayload struct {
	Type      string          `json:"type"`      // message | function_call | function_call_output | custom_tool_call | custom_tool_call_output | reasoning
	Name      string          `json:"name"`      // function_call / custom_tool_call
	Arguments string          `json:"arguments"` // function_call (JSON-encoded args)
	Input     string          `json:"input"`     // custom_tool_call (raw, e.g. an apply_patch body)
	CallID    string          `json:"call_id"`
	Output    json.RawMessage `json:"output"`  // *_output (string or {content,success})
	Summary   []summaryPart   `json:"summary"` // reasoning
	Content   json.RawMessage `json:"content"`
	Role      string          `json:"role"`
	ID        string          `json:"id"`
	Action    *webAction      `json:"action"`
	Status    string          `json:"status"`
}

type summaryPart struct {
	Text string `json:"text"`
}

// toolRef indexes a tool part inside the accumulated messages so a later *_output can fold onto it.
type toolRef struct{ msg, part int }

// parseRollout maps the NDJSON transcript to unified messages. Assistant activity (text, reasoning,
// tool calls) between two user turns accretes into a single assistant message with ordered parts —
// matching how the app renders a turn; a user turn flushes that accumulation.
func parseRollout(r io.Reader, chatID string) ([]agent.Message, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)

	var out []agent.Message
	curAsst := -1                   // index of the open assistant message, or -1
	toolIdx := map[string]toolRef{} // call_id → where its tool part lives (to fold the output)
	modern := false
	nativeState := newStreamState()
	// Older rollouts can carry the same assistant message in both event_msg and response_item.
	eventTexts, responseTexts := map[string]int{}, map[string]int{}
	nextID := func() string { return "codex-" + strconv.Itoa(len(out)) }

	// ensureAsst returns the open assistant message's index, opening one if needed.
	ensureAsst := func(ts string) int {
		if curAsst < 0 {
			out = append(out, agent.Message{ID: nextID(), ChatID: chatID, Role: "assistant", CreatedAt: ts})
			curAsst = len(out) - 1
		}
		return curAsst
	}
	appendCompaction := func(ts, summary string) {
		curAsst = -1
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Role == "system" && len(last.Parts) == 1 && last.Parts[0].Compaction != nil {
				if summary != "" {
					last.Parts[0].Compaction.Summary = summary
				}
				return // A rollout can record both the completed item and its compacted envelope.
			}
		}
		out = append(out, agent.Message{ID: nextID(), ChatID: chatID, Role: "system", CreatedAt: ts,
			Parts: []agent.Part{{Type: "compaction", At: ts, Compaction: &agent.CompactionInfo{Summary: summary}}}})
	}
	appendFeedback := func(ts string, inter *agent.Interaction) {
		i := ensureAsst(ts)
		part := agent.Part{Type: "interaction", At: ts, Interaction: inter}
		for j, existing := range out[i].Parts {
			if sameHistoryPart(existing, part) {
				out[i].Parts[j] = part
				return
			}
		}
		out[i].Parts = append(out[i].Parts, part)
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var env rolloutEnvelope
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		switch env.Type {
		case "compacted":
			// The conversation was compacted. Surface a standalone marker (a boundary, like a user turn)
			// so a reloaded transcript shows it the same way Claude's does and the live stream will. The
			// payload's `message` is the continuation summary (often empty for an auto-compaction); the
			// replacement_history it also carries is prior context re-injected by Codex — intentionally
			// not re-emitted here (it would duplicate messages already shown).
			var p struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			appendCompaction(env.Timestamp, strings.TrimSpace(p.Message))
		case "event_msg":
			var p eventMsgPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			switch p.Type {
			case "item_completed":
				n, err := rolloutItem(p.Item)
				if err != nil {
					return nil, err
				} // let the API use its recorded stream on malformed native data
				if !modern && curAsst >= 0 && n.rawType != "userMessage" && n.Kind != kindIgnored && n.Kind != kindCompaction {
					// Canonical completed items replace duplicate response_item activity for this turn.
					// Keep diagnostic events: they are not restated in the item snapshots.
					var feedback []agent.Part
					for _, part := range out[curAsst].Parts {
						if part.Interaction != nil && part.Interaction.Meta["source"] == "codex" {
							feedback = append(feedback, part)
						}
					}
					if len(feedback) > 0 {
						out[curAsst].Text, out[curAsst].Parts = "", feedback
					} else {
						out = out[:curAsst]
						curAsst = -1
					}
				}
				modern = true
				if n.rawType == "contextCompaction" {
					appendCompaction(env.Timestamp, "")
					continue
				}
				if n.rawType == "userMessage" {
					curAsst = -1
					nativeState = newStreamState()
					if text := strings.TrimSpace(n.Text); text != "" {
						out = append(out, agent.Message{ID: nextID(), ChatID: chatID, Role: "user", Text: text, CreatedAt: env.Timestamp})
					}
					continue
				}
				emitNorm(n, phaseCompleted, p.Item, func(ev agent.Event) {
					i := ensureAsst(env.Timestamp)
					switch ev.Type {
					case agent.EventText:
						appendText(&out[i], ev.Text)
						out[i].Parts[len(out[i].Parts)-1].ID = ev.ItemID
					case agent.EventThinking:
						out[i].Parts = append(out[i].Parts, agent.Part{Type: "thinking", ID: n.ID, Text: ev.Text, At: env.Timestamp,
							DurationMs: int(p.CompletedAtMS - p.StartedAtMS)})
					case agent.EventInteraction:
						appendFeedback(env.Timestamp, ev.Interaction)
					case agent.EventToolUse:
						toolIdx[ev.Tool.ID] = toolRef{msg: i, part: len(out[i].Parts)}
						out[i].Parts = append(out[i].Parts, agent.Part{Type: "tool", At: env.Timestamp,
							Tool: &agent.ToolPart{ID: ev.Tool.ID, Name: ev.Tool.Name, Input: p.Item, Action: ev.Tool.Action}})
					case agent.EventToolResult:
						if ref, ok := toolIdx[ev.Tool.ID]; ok {
							part := &out[ref.msg].Parts[ref.part]
							part.Tool.Output, part.Tool.IsError, part.Tool.Action = ev.Tool.Output, ev.Tool.IsError, ev.Tool.Action
							if p.CompletedAtMS >= p.StartedAtMS {
								part.DurationMs = int(p.CompletedAtMS - p.StartedAtMS)
							}
						}
					}
				}, nativeState)
			case "task_complete":
				if message := errorText(p.Error, ""); message != "" {
					appendFeedback(env.Timestamp, &agent.Interaction{Kind: "error", Title: "Codex error", Detail: message,
						Meta: map[string]any{"source": "codex"}})
				} else if p.LastAgentMessage != "" && (curAsst < 0 || out[curAsst].Text == "") {
					appendText(&out[ensureAsst(env.Timestamp)], p.LastAgentMessage)
				}
			case "error", "warning", "deprecation_notice":
				kind, title := "warning", "Codex warning"
				if p.Type == "error" {
					kind, title = "error", "Codex error"
				}
				if message := errorText(env.Payload, p.Message); message != "" {
					appendFeedback(env.Timestamp, &agent.Interaction{Kind: kind, Title: title, Detail: message,
						Meta: map[string]any{"source": "codex"}})
				}
			case "user_message":
				modern = false
				nativeState = newStreamState()
				eventTexts, responseTexts = map[string]int{}, map[string]int{}
				txt := strings.TrimSpace(p.Message)
				if txt == "" {
					continue
				}
				curAsst = -1 // a user turn closes the current assistant message
				out = append(out, agent.Message{ID: nextID(), ChatID: chatID, Role: "user", Text: txt, CreatedAt: env.Timestamp})
			case "agent_message":
				if modern {
					continue
				}
				if txt := strings.TrimSpace(p.Message); txt != "" {
					eventTexts[txt]++
					if eventTexts[txt] > responseTexts[txt] {
						appendText(&out[ensureAsst(env.Timestamp)], txt)
					}
				}
			}
		case "response_item":
			if modern {
				continue
			}
			var p responseItemPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			switch p.Type {
			case "message":
				if p.Role == "assistant" {
					if text := strings.TrimSpace(strings.Join(textBlocks(p.Content), "\n\n")); text != "" {
						responseTexts[text]++
						if responseTexts[text] > eventTexts[text] {
							appendText(&out[ensureAsst(env.Timestamp)], text)
						}
					}
				}
			case "reasoning":
				if t := summaryText(p.Summary); t != "" {
					i := ensureAsst(env.Timestamp)
					out[i].Parts = append(out[i].Parts, agent.Part{Type: "thinking", Text: t, At: env.Timestamp})
				}
			case "function_call", "custom_tool_call":
				i := ensureAsst(env.Timestamp)
				input := p.Arguments
				if input == "" {
					input = p.Input
				}
				if strings.TrimPrefix(p.Name, "functions.") == "request_user_input_async" {
					var payload struct {
						Questions []asyncQuestion `json:"questions"`
					}
					if json.Unmarshal([]byte(input), &payload) == nil {
						if inter := asyncQuestionInteraction(p.CallID, payload.Questions); inter != nil {
							appendFeedback(env.Timestamp, inter)
							continue
						}
					}
				}
				if strings.TrimPrefix(p.Name, "functions.") == "update_plan" {
					if inter := planInteraction(json.RawMessage(input)); inter != nil {
						inter.ID = p.CallID
						out[i].Parts = append(out[i].Parts, agent.Part{Type: "interaction", Interaction: inter, At: env.Timestamp})
						continue
					}
				}
				out[i].Parts = append(out[i].Parts, agent.Part{Type: "tool", At: env.Timestamp,
					Tool: &agent.ToolPart{ID: p.CallID, Name: p.Name, Input: rawJSON(input),
						Action: codexHistoryAction(p.Name, input, "", false)}})
				if p.CallID != "" {
					toolIdx[p.CallID] = toolRef{msg: i, part: len(out[i].Parts) - 1}
				}
			case "function_call_output", "custom_tool_call_output":
				ref, ok := toolIdx[p.CallID]
				if !ok {
					continue // output without a matching call we recorded — skip
				}
				if tp := out[ref.msg].Parts[ref.part].Tool; tp != nil {
					tp.Output, tp.IsError = outputText(p.Output)
					// Only shell stdout arrives late; fold it into the already-classified action.
					if tp.Action != nil && tp.Action.Shell != nil {
						tp.Action.Shell.Stdout = tp.Output
						if code := shellExitCode(tp.Output); code != nil {
							tp.Action.Shell.ExitCode = code
							tp.IsError = tp.IsError || *code != 0
						}
					}
				}
			case "web_search_call":
				n := normItem{ID: agent.FirstNonEmpty(p.ID, p.CallID), Kind: kindWebSearch, WebAction: p.Action, Status: p.Status}
				i := ensureAsst(env.Timestamp)
				out[i].Parts = append(out[i].Parts, agent.Part{Type: "tool", At: env.Timestamp,
					Tool: &agent.ToolPart{ID: n.ID, Name: "web_search", Input: env.Payload, Action: codexToolAction(n)}})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err // truncated transcript — let the caller fall back rather than serve a partial
	}
	return out, nil
}

// appendText adds a visible-text part and keeps Message.Text as the joined back-compat text.
func appendText(m *agent.Message, txt string) {
	m.Parts = append(m.Parts, agent.Part{Type: "text", Text: txt})
	if m.Text == "" {
		m.Text = txt
	} else {
		m.Text += "\n\n" + txt
	}
}

// summaryText joins a reasoning item's summary blocks (the human-readable thinking; the encrypted
// `content` is opaque and not surfaced).
func summaryText(parts []summaryPart) string {
	var b []string
	for _, p := range parts {
		if t := strings.TrimSpace(p.Text); t != "" {
			b = append(b, t)
		}
	}
	return strings.Join(b, "\n\n")
}

// outputText renders a tool output payload (a JSON string, or a {content, success} object) to display
// text plus an error flag.
func outputText(raw json.RawMessage) (string, bool) {
	return renderOutput(raw)
}

// rawJSON returns valid JSON for a tool's Input: the value verbatim if it already parses as JSON
// (function_call arguments are a JSON object), otherwise the value encoded as a JSON string (a
// custom_tool_call input like an apply_patch body is raw text). nil for empty.
func rawJSON(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

// codexHistoryAction deep-normalizes a rollout tool call (function_call / custom_tool_call) into the
// unified action. Unlike the live stream (which builds from a normItem), the rollout identifies a tool
// by name + a raw arguments/input string, so classification is name-based here. output is "" at the
// call site and folded in later for shell stdout. Best-effort: an unknown name yields a Title-only
// "other"; the raw Input always survives alongside.
func codexHistoryAction(name, input, output string, isError bool) *agent.ToolAction {
	switch strings.TrimPrefix(name, "functions.") {
	case "shell", "local_shell", "shell_command", "exec_command", "write_stdin":
		return codexShellHistoryAction(input, output)
	case "apply_patch":
		return codexPatchAction(input)
	case "web_search", "web_search_preview":
		var in struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal([]byte(input), &in)
		return &agent.ToolAction{Kind: agent.KindWebSearch, Title: in.Query, Web: &agent.WebSearch{Query: in.Query}}
	case "":
		return nil
	default:
		return &agent.ToolAction{Kind: agent.KindOther, Title: name}
	}
}

// codexShellHistoryAction decodes a rollout shell call's arguments ({command:[…]|string, workdir}).
func codexShellHistoryAction(input, output string) *agent.ToolAction {
	var in struct {
		Command any    `json:"command"`
		Cmd     string `json:"cmd"`
		Workdir string `json:"workdir"`
	}
	_ = json.Unmarshal([]byte(input), &in)
	cmd := agent.FirstNonEmpty(joinCommand(in.Command), in.Cmd)
	return &agent.ToolAction{
		Kind: agent.KindShell, Title: cmd,
		Shell: &agent.ShellCommand{Command: cmd, Cwd: in.Workdir, Stdout: output},
	}
}

// Older exec_command rollouts put the process status in the output header. Only inspect
// that header, never arbitrary stdout that happens to contain similar text.
func shellExitCode(output string) *int {
	for _, line := range strings.Split(output, "\n") {
		if line == "Output:" || line == "Final output:" {
			break
		}
		if value, ok := strings.CutPrefix(line, "Process exited with code "); ok {
			if code, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				return &code
			}
		}
	}
	return nil
}

// joinCommand renders a shell command that may be a bare string or an argv array to one command line.
func joinCommand(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, e := range c {
			if s, ok := e.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// codexPatchAction turns an apply_patch body into a file_edit action. The patch body is the only place a
// real Codex diff exists in-repo, so each file's section text is carried verbatim as FileChange.Diff.
func codexPatchAction(body string) *agent.ToolAction {
	files := parseApplyPatch(body)
	if len(files) == 0 {
		files = []agent.FileChange{{Diff: strings.TrimSpace(body)}}
	}
	title := ""
	if len(files) == 1 {
		title = files[0].Path
	}
	return &agent.ToolAction{Kind: agent.KindFileEdit, Title: title, Files: files}
}

// parseApplyPatch splits a Codex apply_patch body into per-file changes. Markers are "*** Add File: p",
// "*** Update File: p", and "*** Delete File: p"; each file's remaining lines become its best-effort diff.
func parseApplyPatch(body string) []agent.FileChange {
	var files []agent.FileChange
	cur := -1
	var buf []string
	flush := func() {
		if cur >= 0 {
			files[cur].Diff = strings.TrimRight(strings.Join(buf, "\n"), "\n")
		}
		buf = nil
	}
	add := func(prefix, op, line string) {
		flush()
		files = append(files, agent.FileChange{Path: strings.TrimSpace(strings.TrimPrefix(line, prefix)), Op: op})
		cur = len(files) - 1
	}
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "*** Update File:"):
			add("*** Update File:", "edit", t)
		case strings.HasPrefix(t, "*** Add File:"):
			add("*** Add File:", "create", t)
		case strings.HasPrefix(t, "*** Delete File:"):
			add("*** Delete File:", "delete", t)
		case strings.HasPrefix(t, "*** Begin Patch"), strings.HasPrefix(t, "*** End Patch"):
			// structural markers — ignore
		default:
			if cur >= 0 {
				buf = append(buf, ln)
			}
		}
	}
	flush()
	return files
}
