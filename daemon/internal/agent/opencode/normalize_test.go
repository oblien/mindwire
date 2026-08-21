package opencode

import (
	"encoding/json"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// A text part arrives with CUMULATIVE text on each update; emitPart must stream only the not-yet-seen
// suffix (all Delta=true), and the concatenation of those deltas must equal the final text exactly once
// — the same discipline codex/claude hold.
func TestEmitPartTextConcatOnce(t *testing.T) {
	st := newStreamState()
	var evs []agent.Event
	emit := func(e agent.Event) { evs = append(evs, e) }

	id, full := st.emitPart(ocPart{ID: "prt_1", Type: "text", Text: "Hel"}, emit)
	if id != "prt_1" || full != "Hel" {
		t.Fatalf("first emitPart = (%q,%q), want (prt_1,Hel)", id, full)
	}
	st.emitPart(ocPart{ID: "prt_1", Type: "text", Text: "Hello"}, emit)
	_, last := st.emitPart(ocPart{ID: "prt_1", Type: "text", Text: "Hello world"}, emit)
	if last != "Hello world" {
		t.Errorf("final full = %q, want 'Hello world'", last)
	}

	var joined string
	for _, e := range evs {
		if e.Type != agent.EventText {
			t.Fatalf("unexpected event type %v", e.Type)
		}
		if !e.Delta {
			t.Errorf("streamed text must be Delta=true, got %+v", e)
		}
		joined += e.Text
	}
	if joined != "Hello world" {
		t.Errorf("concatenated deltas = %q, want 'Hello world' (no double-count, no loss)", joined)
	}
	if len(evs) != 3 { // "Hel", "lo", " world"
		t.Errorf("expected 3 delta events, got %d: %+v", len(evs), evs)
	}
}

// A reasoning part maps to EventThinking (Delta=true), not EventText.
func TestEmitPartReasoningToThinking(t *testing.T) {
	st := newStreamState()
	var evs []agent.Event
	emit := func(e agent.Event) { evs = append(evs, e) }

	st.emitPart(ocPart{ID: "prt_r", Type: "reasoning", Text: "thinking hard"}, emit)
	if len(evs) != 1 || evs[0].Type != agent.EventThinking || evs[0].Text != "thinking hard" || !evs[0].Delta {
		t.Errorf("reasoning event = %+v, want single Delta EventThinking", evs)
	}
}

// Whitespace-only growth is swallowed so a trailing newline from the model never surfaces as an event.
func TestEmitPartWhitespaceSwallowed(t *testing.T) {
	st := newStreamState()
	var n int
	emit := func(agent.Event) { n++ }
	st.emitPart(ocPart{ID: "prt_1", Type: "text", Text: "done"}, emit)
	st.emitPart(ocPart{ID: "prt_1", Type: "text", Text: "done\n"}, emit) // only whitespace grew
	if n != 1 {
		t.Errorf("emitted %d events, want 1 (whitespace-only growth swallowed)", n)
	}
}

// A tool part threads pending→completed by callID: exactly one tool_use (on first sighting) and exactly
// one tool_result (when it completes), and a repeated completed frame does not re-emit.
func TestEmitPartToolUseResultPairByCallID(t *testing.T) {
	st := newStreamState()
	var evs []agent.Event
	emit := func(e agent.Event) { evs = append(evs, e) }

	raw := json.RawMessage(`{"command":"ls -la"}`)
	st.emitPart(ocPart{Type: "tool", CallID: "call-1", ID: "prt_t", Tool: "bash",
		State: &ocToolState{Status: "running", Input: raw}}, emit)
	st.emitPart(ocPart{Type: "tool", CallID: "call-1", ID: "prt_t", Tool: "bash",
		State: &ocToolState{Status: "completed", Input: raw, Output: "total 0"}}, emit)
	// A duplicate completed frame must not produce another result.
	st.emitPart(ocPart{Type: "tool", CallID: "call-1", ID: "prt_t", Tool: "bash",
		State: &ocToolState{Status: "completed", Input: raw, Output: "total 0"}}, emit)

	var use, res *agent.Event
	for i := range evs {
		switch evs[i].Type {
		case agent.EventToolUse:
			if use != nil {
				t.Fatalf("more than one tool_use emitted: %+v", evs)
			}
			use = &evs[i]
		case agent.EventToolResult:
			if res != nil {
				t.Fatalf("more than one tool_result emitted: %+v", evs)
			}
			res = &evs[i]
		}
	}
	if use == nil || use.Tool == nil || use.Tool.ID != "call-1" || use.Tool.Name != "bash" {
		t.Fatalf("tool_use = %+v, want id call-1 name bash", use)
	}
	if use.Tool.Action == nil || use.Tool.Action.Kind != agent.KindShell {
		t.Errorf("tool_use action = %+v, want KindShell", use.Tool.Action)
	}
	if res == nil || res.Tool == nil || res.Tool.Output != "total 0" || res.Tool.IsError {
		t.Errorf("tool_result = %+v, want output 'total 0' IsError=false", res)
	}
}

// The tool id falls back to the part id when there is no callID, so a use/result pair is still keyed.
func TestEmitPartToolFallsBackToPartID(t *testing.T) {
	st := newStreamState()
	var evs []agent.Event
	emit := func(e agent.Event) { evs = append(evs, e) }
	st.emitPart(ocPart{Type: "tool", ID: "prt_only", Tool: "read",
		State: &ocToolState{Status: "completed", Input: json.RawMessage(`{"filePath":"a.go"}`), Output: "x"}}, emit)
	if len(evs) < 1 || evs[0].Tool == nil || evs[0].Tool.ID != "prt_only" {
		t.Errorf("tool id = %+v, want fallback to part id prt_only", evs)
	}
}

// An errored tool with no output surfaces the error string as the output so a result is never blank.
func TestToolResultErrorFallback(t *testing.T) {
	out, isErr := toolResult(&ocToolState{Status: "error", Error: "boom"})
	if out != "boom" || !isErr {
		t.Errorf("toolResult(error) = (%q,%v), want (boom,true)", out, isErr)
	}
	if out, isErr := toolResult(nil); out != "" || isErr {
		t.Errorf("toolResult(nil) = (%q,%v), want ('',false)", out, isErr)
	}
}

// ocToolAction deep-normalizes each known tool and never guesses an unknown one (→ KindOther).
func TestOcToolAction(t *testing.T) {
	if a := ocToolAction("bash", json.RawMessage(`{"command":"go test","description":"run tests"}`)); a.Kind != agent.KindShell || a.Shell == nil || a.Shell.Command != "go test" || a.Title != "run tests" {
		t.Errorf("bash = %+v", a)
	}
	if a := ocToolAction("edit", json.RawMessage(`{"filePath":"main.go","oldString":"a","newString":"b"}`)); a.Kind != agent.KindFileEdit || len(a.Files) != 1 || a.Files[0].Op != "edit" || a.Files[0].Diff == "" {
		t.Errorf("edit = %+v", a)
	}
	if a := ocToolAction("write", json.RawMessage(`{"filePath":"new.go","content":"pkg"}`)); a.Kind != agent.KindFileEdit || len(a.Files) != 1 || a.Files[0].Op != "create" {
		t.Errorf("write = %+v", a)
	}
	if a := ocToolAction("read", json.RawMessage(`{"filePath":"x.go"}`)); a.Kind != agent.KindFileRead || a.Title != "x.go" {
		t.Errorf("read = %+v", a)
	}
	if a := ocToolAction("grep", json.RawMessage(`{"pattern":"TODO","path":".","include":"*.go"}`)); a.Kind != agent.KindSearch || a.Search == nil || a.Search.Query != "TODO" || a.Search.Glob != "*.go" {
		t.Errorf("grep = %+v", a)
	}
	if a := ocToolAction("glob", json.RawMessage(`{"pattern":"**/*.go"}`)); a.Kind != agent.KindSearch || a.Search == nil || a.Search.Glob != "**/*.go" {
		t.Errorf("glob = %+v", a)
	}
	if a := ocToolAction("webfetch", json.RawMessage(`{"url":"https://x.dev"}`)); a.Kind != agent.KindWebFetch || a.Web == nil || a.Web.URL != "https://x.dev" {
		t.Errorf("webfetch = %+v", a)
	}
	if a := ocToolAction("mystery", nil); a.Kind != agent.KindOther || a.Title != "mystery" {
		t.Errorf("unknown = %+v, want KindOther titled 'mystery'", a)
	}
}

// A todowrite tool becomes a todos Interaction (plan snapshot); an identical consecutive snapshot is
// de-duped, and a changed one re-emits.
func TestEmitPartTodos(t *testing.T) {
	st := newStreamState()
	var inter []agent.Event
	emit := func(e agent.Event) {
		if e.Type == agent.EventInteraction {
			inter = append(inter, e)
		}
	}
	in := json.RawMessage(`{"todos":[{"content":"step one","status":"in_progress"},{"content":"","status":"pending"}]}`)
	st.emitPart(ocPart{Type: "tool", CallID: "td-1", Tool: "todowrite", State: &ocToolState{Status: "running", Input: in}}, emit)
	st.emitPart(ocPart{Type: "tool", CallID: "td-1", Tool: "todowrite", State: &ocToolState{Status: "completed", Input: in}}, emit) // identical → deduped
	if len(inter) != 1 {
		t.Fatalf("emitted %d todo interactions, want 1 (identical snapshot deduped)", len(inter))
	}
	it := inter[0].Interaction
	if it == nil || it.Kind != "todos" || len(it.Items) != 1 || it.Items[0].Content != "step one" || it.Items[0].Status != "in_progress" {
		t.Fatalf("todos interaction = %+v, want one item 'step one'/in_progress (blank dropped)", it)
	}

	changed := json.RawMessage(`{"todos":[{"content":"step one","status":"completed"}]}`)
	st.emitPart(ocPart{Type: "tool", CallID: "td-1", Tool: "todowrite", State: &ocToolState{Status: "completed", Input: changed}}, emit)
	if len(inter) != 2 {
		t.Errorf("changed snapshot should re-emit; got %d interactions", len(inter))
	}
}

// usageMeta returns nil for a zero snapshot and a fully-keyed map when anything was reported.
func TestUsageMeta(t *testing.T) {
	if usageMeta(ocMessage{}) != nil {
		t.Error("zero usage should map to nil meta")
	}
	m := ocMessage{Cost: 0.002, Tokens: ocTokens{Input: 12, Output: 7, Reasoning: 3}}
	m.Tokens.Cache.Read = 5
	m.Tokens.Cache.Write = 1
	meta := usageMeta(m)
	if meta == nil {
		t.Fatal("non-zero usage mapped to nil meta")
	}
	for k, want := range map[string]any{
		"costUsd": 0.002, "inputTokens": 12, "outputTokens": 7,
		"reasoningTokens": 3, "cacheReadTokens": 5, "cacheWriteTokens": 1,
	} {
		if meta[k] != want {
			t.Errorf("meta[%q] = %v, want %v", k, meta[k], want)
		}
	}
}

// sessionErrorText tolerates opencode's name/message/data.message shapes and falls back to a label.
func TestSessionErrorText(t *testing.T) {
	cases := map[string]string{
		`{"error":{"data":{"message":"deep"}}}`: "deep",
		`{"error":{"message":"mid"}}`:           "mid",
		`{"error":{"name":"ProviderError"}}`:    "ProviderError",
		`{"error":{}}`:                          "opencode session error",
	}
	for raw, want := range cases {
		if got := sessionErrorText(json.RawMessage(raw)); got != want {
			t.Errorf("sessionErrorText(%s) = %q, want %q", raw, got, want)
		}
	}
}

// A permission ask maps to an approval Interaction whose ID IS the opencode permission id (so the
// inbound response routes straight back), with allow/deny options and the id echoed in Meta.
func TestPermissionInteraction(t *testing.T) {
	inter := permissionInteraction(ocPermission{ID: "perm-9", SessionID: "ses_1", Title: "Run ls"})
	if inter.ID != "perm-9" || inter.Kind != "approval" || !inter.NeedsResponse {
		t.Fatalf("interaction = %+v, want approval id perm-9 NeedsResponse", inter)
	}
	if len(inter.Options) != 2 || inter.Options[0].ID != "allow" || inter.Options[1].ID != "deny" {
		t.Errorf("options = %+v, want allow/deny", inter.Options)
	}
	if inter.Meta["permissionID"] != "perm-9" || inter.Meta["sessionID"] != "ses_1" {
		t.Errorf("meta = %+v, want permissionID+sessionID", inter.Meta)
	}
	// A blank title gets a sensible default rather than an empty label.
	if got := permissionInteraction(ocPermission{ID: "p"}); got.Title == "" {
		t.Error("blank permission title should default to a non-empty prompt")
	}
}

// splitModel splits on the FIRST slash and yields ("","") for any malformed selector so RunStream omits
// the model rather than handing opencode a bad one.
func TestSplitModel(t *testing.T) {
	cases := []struct{ in, p, m string }{
		{"anthropic/claude-opus-4-8", "anthropic", "claude-opus-4-8"},
		{"a/b/c", "a", "b/c"}, // only the first slash splits
		{" openai/gpt-5 ", "openai", "gpt-5"},
		{"noslash", "", ""},
		{"/leading", "", ""},
		{"trailing/", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if p, m := splitModel(c.in); p != c.p || m != c.m {
			t.Errorf("splitModel(%q) = (%q,%q), want (%q,%q)", c.in, p, m, c.p, c.m)
		}
	}
}

// permMode defaults to autonomous "auto" and honors an explicit config value.
func TestPermMode(t *testing.T) {
	if got := permMode(agent.TurnInput{}); got != "auto" {
		t.Errorf("default permMode = %q, want auto", got)
	}
	if got := permMode(agent.TurnInput{Config: map[string]string{keyPermission: "ask"}}); got != "ask" {
		t.Errorf("permMode(ask) = %q, want ask", got)
	}
}
