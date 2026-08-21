package codex

import (
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// feed runs parseStream over NDJSON lines and returns the emitted events plus the terminal result.
func feed(t *testing.T, lines ...string) ([]agent.Event, agent.TurnResult, bool) {
	t.Helper()
	var got []agent.Event
	res, ok := parseStream(strings.NewReader(strings.Join(lines, "\n")+"\n"), func(e agent.Event) { got = append(got, e) })
	return got, res, ok
}

func countType(evs []agent.Event, ty agent.EventType) int {
	n := 0
	for _, e := range evs {
		if e.Type == ty {
			n++
		}
	}
	return n
}

func firstOf(evs []agent.Event, ty agent.EventType) *agent.Event {
	for i := range evs {
		if evs[i].Type == ty {
			return &evs[i]
		}
	}
	return nil
}

// A full, well-formed exec turn: session id, reasoning, a shell tool (started→completed), a todo
// list, the final message, and the terminal result carrying token usage.
func TestParseFullTurn(t *testing.T) {
	evs, res, ok := feed(t,
		`{"type":"thread.started","thread_id":"th-123"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"r1","type":"reasoning","text":"thinking hard"}}`,
		`{"type":"item.started","item":{"id":"c1","type":"command_execution","command":"ls","status":"in_progress"}}`,
		`{"type":"item.updated","item":{"id":"c1","type":"command_execution","command":"ls","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"c1","type":"command_execution","command":"ls","status":"completed","exit_code":0,"aggregated_output":"file.txt"}}`,
		`{"type":"item.completed","item":{"id":"td","type":"todo_list","items":[{"text":"step one","completed":false}]}}`,
		`{"type":"item.completed","item":{"id":"m1","type":"agent_message","text":"all done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":2,"output_tokens":5,"reasoning_output_tokens":3}}`,
	)
	if !ok {
		t.Fatal("expected a terminal result (got=true)")
	}

	if s := firstOf(evs, agent.EventSession); s == nil || s.SessionID != "th-123" {
		t.Fatalf("expected session event th-123, got %+v", s)
	}
	if res.SessionID != "th-123" || res.IsError {
		t.Errorf("result = %+v, want SessionID th-123 not-error", res)
	}
	if th := firstOf(evs, agent.EventThinking); th == nil || th.Text != "thinking hard" {
		t.Errorf("thinking event = %+v", th)
	}

	// The tool item appears at started/updated/completed but must announce a single tool_use.
	if n := countType(evs, agent.EventToolUse); n != 1 {
		t.Errorf("expected exactly 1 tool_use (deduped), got %d", n)
	}
	if tu := firstOf(evs, agent.EventToolUse); tu == nil || tu.Tool == nil || tu.Tool.Name != "shell" {
		t.Errorf("tool_use = %+v, want name shell", tu)
	}
	if tr := firstOf(evs, agent.EventToolResult); tr == nil || tr.Tool == nil || tr.Tool.Output != "file.txt" || tr.Tool.IsError {
		t.Errorf("tool_result = %+v, want output file.txt not-error", tr)
	}

	if it := firstOf(evs, agent.EventInteraction); it == nil || it.Interaction == nil ||
		it.Interaction.Kind != "todos" || len(it.Interaction.Items) != 1 || it.Interaction.Items[0].Content != "step one" {
		t.Errorf("todos interaction = %+v", it)
	}

	if tx := firstOf(evs, agent.EventText); tx == nil || tx.Text != "all done" {
		t.Errorf("text event = %+v, want 'all done'", tx)
	}

	rev := firstOf(evs, agent.EventResult)
	if rev == nil || rev.Result == nil || rev.Result.Text != "all done" {
		t.Fatalf("result event = %+v", rev)
	}
	if rev.Meta == nil || rev.Meta["inputTokens"] != 10 || rev.Meta["outputTokens"] != 5 {
		t.Errorf("result meta tokens = %+v", rev.Meta)
	}
}

// W4a: a cumulative agent_message that grows across item.updated → item.completed streams as Delta=true
// suffixes (never re-emitting the whole block), and the concatenated deltas equal the final text — which
// also becomes the terminal result. The exec parser thus mirrors claude's streaming discipline.
func TestParseStreamingText(t *testing.T) {
	evs, res, ok := feed(t,
		`{"type":"thread.started","thread_id":"th-s"}`,
		`{"type":"item.started","item":{"id":"m1","type":"agent_message","text":""}}`,
		`{"type":"item.updated","item":{"id":"m1","type":"agent_message","text":"Hel"}}`,
		`{"type":"item.updated","item":{"id":"m1","type":"agent_message","text":"Hello"}}`,
		`{"type":"item.completed","item":{"id":"m1","type":"agent_message","text":"Hello world"}}`,
		`{"type":"turn.completed","usage":{}}`,
	)
	if !ok || res.Text != "Hello world" {
		t.Fatalf("result = %+v ok=%v, want text 'Hello world'", res, ok)
	}
	var joined string
	nText := 0
	for _, e := range evs {
		if e.Type != agent.EventText {
			continue
		}
		nText++
		if !e.Delta {
			t.Errorf("streamed text must be Delta=true, got %+v", e)
		}
		joined += e.Text
	}
	if joined != "Hello world" {
		t.Errorf("concatenated deltas = %q, want 'Hello world' (no double-count)", joined)
	}
	if nText != 3 { // "Hel", "lo", " world"
		t.Errorf("expected 3 text deltas, got %d", nText)
	}
}

// A failed turn produces a terminal error result carrying the failure message.
func TestParseTurnFailed(t *testing.T) {
	evs, res, ok := feed(t,
		`{"type":"thread.started","thread_id":"th-9"}`,
		`{"type":"turn.failed","error":{"message":"model overloaded"}}`,
	)
	if !ok || !res.IsError || res.Text != "model overloaded" {
		t.Fatalf("result = %+v ok=%v, want error 'model overloaded'", res, ok)
	}
	if rev := firstOf(evs, agent.EventResult); rev == nil || rev.Result == nil || !rev.Result.IsError {
		t.Errorf("expected an error result event, got %+v", rev)
	}
}

// A mid-stream error with no terminal turn event surfaces as an error event and got=false, so the
// driver falls back to stderr/exit for the turn's error.
func TestParseMidStreamErrorNoTerminal(t *testing.T) {
	evs, _, ok := feed(t,
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"error","message":"stream blew up"}`,
	)
	if ok {
		t.Error("a lone error must not count as a terminal result (got should be false)")
	}
	if e := firstOf(evs, agent.EventError); e == nil || e.Error != "stream blew up" {
		t.Errorf("error event = %+v", e)
	}
}

// A failed command tool (non-zero exit) sets IsError on its result.
func TestParseCommandFailure(t *testing.T) {
	evs, _, _ := feed(t,
		`{"type":"thread.started","thread_id":"th-2"}`,
		`{"type":"item.completed","item":{"id":"c9","type":"command_execution","command":"false","status":"failed","exit_code":1,"aggregated_output":"boom"}}`,
		`{"type":"turn.completed","usage":{}}`,
	)
	if tr := firstOf(evs, agent.EventToolResult); tr == nil || tr.Tool == nil || !tr.Tool.IsError || tr.Tool.Output != "boom" {
		t.Errorf("failed tool_result = %+v, want IsError with output boom", tr)
	}
}

// The live exec stream attaches a deep-normalized action to command_execution, file_change, and
// web_search items. Codex — unlike Claude — DOES report a shell exit code, so the completed shell
// action carries a non-nil ExitCode (the key cross-agent contrast).
func TestParseToolActions(t *testing.T) {
	evs, _, _ := feed(t,
		`{"type":"thread.started","thread_id":"th-a"}`,
		`{"type":"item.started","item":{"id":"c1","type":"command_execution","command":"go test","cwd":"/repo","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"c1","type":"command_execution","command":"go test","cwd":"/repo","status":"completed","exit_code":0,"aggregated_output":"ok"}}`,
		`{"type":"item.completed","item":{"id":"f1","type":"file_change","status":"completed","changes":[{"path":"a.go","kind":"modify"}]}}`,
		`{"type":"item.completed","item":{"id":"w1","type":"web_search","query":"golang generics"}}`,
		`{"type":"turn.completed","usage":{}}`,
	)

	// Shell: the tool_use (from item.started) has the command but no exit code yet; the tool_result
	// (item.completed) carries stdout + a non-nil exit code.
	var shellUse, shellRes *agent.ToolEvent
	for i := range evs {
		if evs[i].Tool == nil || evs[i].Tool.ID != "c1" {
			continue
		}
		if evs[i].Type == agent.EventToolUse {
			shellUse = evs[i].Tool
		} else if evs[i].Type == agent.EventToolResult {
			shellRes = evs[i].Tool
		}
	}
	if shellUse == nil || shellUse.Action == nil || shellUse.Action.Shell == nil ||
		shellUse.Action.Shell.Command != "go test" || shellUse.Action.Shell.Cwd != "/repo" {
		t.Fatalf("shell tool_use action = %+v", shellUse)
	}
	if shellUse.Action.Shell.ExitCode != nil {
		t.Errorf("in-progress shell must have nil ExitCode")
	}
	if shellRes == nil || shellRes.Action == nil || shellRes.Action.Shell == nil {
		t.Fatalf("shell tool_result action = %+v", shellRes)
	}
	if shellRes.Action.Shell.ExitCode == nil || *shellRes.Action.Shell.ExitCode != 0 {
		t.Errorf("completed Codex shell must report exit code 0 (non-nil), got %v", shellRes.Action.Shell.ExitCode)
	}
	if shellRes.Action.Shell.Stdout != "ok" {
		t.Errorf("shell stdout = %q, want ok", shellRes.Action.Shell.Stdout)
	}

	// File change → file_edit; "modify" maps to op "edit".
	var fc *agent.ToolEvent
	for i := range evs {
		if evs[i].Type == agent.EventToolUse && evs[i].Tool != nil && evs[i].Tool.ID == "f1" {
			fc = evs[i].Tool
		}
	}
	if fc == nil || fc.Action == nil || fc.Action.Kind != agent.KindFileEdit || len(fc.Action.Files) != 1 ||
		fc.Action.Files[0].Path != "a.go" || fc.Action.Files[0].Op != "edit" {
		t.Errorf("file_change action = %+v", fc)
	}

	// Web search → web_search with the query.
	var ws *agent.ToolEvent
	for i := range evs {
		if evs[i].Type == agent.EventToolUse && evs[i].Tool != nil && evs[i].Tool.ID == "w1" {
			ws = evs[i].Tool
		}
	}
	if ws == nil || ws.Action == nil || ws.Action.Kind != agent.KindWebSearch || ws.Action.Web == nil ||
		ws.Action.Web.Query != "golang generics" {
		t.Errorf("web_search action = %+v", ws)
	}
}

// A non-zero exit is reported (non-nil pointer to the code), and IsError is set.
func TestParseToolActionFailedExit(t *testing.T) {
	evs, _, _ := feed(t,
		`{"type":"thread.started","thread_id":"th-b"}`,
		`{"type":"item.completed","item":{"id":"c2","type":"command_execution","command":"false","status":"failed","exit_code":2,"aggregated_output":"boom"}}`,
		`{"type":"turn.completed","usage":{}}`,
	)
	tr := firstOf(evs, agent.EventToolResult)
	if tr == nil || tr.Tool == nil || tr.Tool.Action == nil || tr.Tool.Action.Shell == nil {
		t.Fatalf("failed shell action = %+v", tr)
	}
	if tr.Tool.Action.Shell.ExitCode == nil || *tr.Tool.Action.Shell.ExitCode != 2 {
		t.Errorf("expected exit code 2, got %v", tr.Tool.Action.Shell.ExitCode)
	}
}

// Garbage and non-JSON lines are skipped without derailing the stream.
func TestParseSkipsJunk(t *testing.T) {
	_, res, ok := feed(t,
		`not json at all`,
		``,
		`{"type":"thread.started","thread_id":"th-3"}`,
		`{"type":"item.completed","item":{"id":"m","type":"agent_message","text":"ok"}}`,
		`{"type":"turn.completed","usage":{}}`,
	)
	if !ok || res.Text != "ok" {
		t.Errorf("result = %+v ok=%v, want text ok", res, ok)
	}
}
