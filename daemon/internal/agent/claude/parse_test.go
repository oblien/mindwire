package claude

import (
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// feed runs parseStream over NDJSON lines and returns the emitted events.
func feed(t *testing.T, lines ...string) []agent.Event {
	t.Helper()
	var got []agent.Event
	parseStream(strings.NewReader(strings.Join(lines, "\n")+"\n"), func(e agent.Event) { got = append(got, e) })
	return got
}

func firstInteraction(evs []agent.Event) *agent.Interaction {
	for _, e := range evs {
		if e.Type == agent.EventInteraction {
			return e.Interaction
		}
	}
	return nil
}

func TestParsePlanInteraction(t *testing.T) {
	ev := feed(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"ExitPlanMode","input":{"plan":"1. do X\n2. do Y"}}]}}`)
	it := firstInteraction(ev)
	if it == nil || it.Kind != "plan" {
		t.Fatalf("expected a plan interaction, got %+v", it)
	}
	if it.Detail != "1. do X\n2. do Y" || !it.NeedsResponse || len(it.Options) != 2 {
		t.Errorf("plan interaction = %+v", it)
	}
}

func TestParseQuestionInteraction(t *testing.T) {
	ev := feed(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"AskUserQuestion","input":{"questions":[{"question":"Which DB?","header":"DB","multiSelect":false,"options":[{"label":"Postgres"},{"label":"MySQL"}]}]}}]}}`)
	it := firstInteraction(ev)
	if it == nil || it.Kind != "choice" {
		t.Fatalf("expected a choice interaction, got %+v", it)
	}
	if it.Title != "Which DB?" || len(it.Options) != 2 || it.Options[0].Label != "Postgres" {
		t.Errorf("question interaction = %+v", it)
	}
}

// A Bash tool_use → tool_result pair: the use carries the input-derived shell action (no stdout yet),
// and the result — which itself has no tool name on the wire — is reclassified from the stashed use so
// its action folds in the stdout. Claude reports no exit code, so ExitCode stays nil throughout.
func TestParseToolActionUseResultPair(t *testing.T) {
	evs := feed(t,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"file.txt"}]}}`,
	)
	var use, res *agent.ToolEvent
	for i := range evs {
		switch evs[i].Type {
		case agent.EventToolUse:
			use = evs[i].Tool
		case agent.EventToolResult:
			res = evs[i].Tool
		}
	}
	if use == nil || use.Action == nil || use.Action.Shell == nil || use.Action.Shell.Command != "ls" {
		t.Fatalf("tool_use action = %+v", use)
	}
	if use.Action.Shell.Stdout != "" {
		t.Errorf("tool_use action should have no stdout yet, got %q", use.Action.Shell.Stdout)
	}
	if res == nil || res.Action == nil || res.Action.Shell == nil {
		t.Fatalf("tool_result action = %+v (result must be reclassified from the stashed use)", res)
	}
	if res.Action.Shell.Stdout != "file.txt" {
		t.Errorf("tool_result action stdout = %q, want file.txt", res.Action.Shell.Stdout)
	}
	if res.Action.Shell.ExitCode != nil {
		t.Errorf("Claude reports no exit code; ExitCode must be nil")
	}
}

// A tool_result whose originating tool_use was never seen leaves Action nil (rather than a
// misclassified one); the runner then keeps whatever the use emitted.
func TestParseToolResultWithoutUse(t *testing.T) {
	evs := feed(t,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"ghost","content":"x"}]}}`,
	)
	for _, e := range evs {
		if e.Type == agent.EventToolResult && e.Tool != nil && e.Tool.Action != nil {
			t.Errorf("orphan tool_result must have nil Action, got %+v", e.Tool.Action)
		}
	}
}

func TestParseTodosInteractionAndPlainTool(t *testing.T) {
	ev := feed(t,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"TodoWrite","input":{"todos":[{"content":"a","status":"pending"}]}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t4","name":"Bash","input":{"command":"ls"}}]}}`,
	)
	if it := firstInteraction(ev); it == nil || it.Kind != "todos" || len(it.Items) != 1 {
		t.Fatalf("expected a todos interaction, got %+v", it)
	}
	// Bash stays a tool_use event, not an interaction.
	var sawBash bool
	for _, e := range ev {
		if e.Type == agent.EventToolUse && e.Tool != nil && e.Tool.Name == "Bash" {
			sawBash = true
		}
	}
	if !sawBash {
		t.Error("expected Bash to remain a tool_use event")
	}
}

// A system/compact_boundary line becomes a unified compaction event carrying the trigger and the
// pre/post token counts from compactMetadata (F4b).
func TestParseCompactionBoundary(t *testing.T) {
	ev := feed(t, `{"type":"system","subtype":"compact_boundary","content":"Conversation compacted","compactMetadata":{"trigger":"manual","preTokens":150000,"postTokens":42000}}`)
	var ci *agent.CompactionInfo
	for _, e := range ev {
		if e.Type == agent.EventCompaction {
			ci = e.Compaction
		}
	}
	if ci == nil {
		t.Fatalf("expected a compaction event, got %+v", ev)
	}
	if ci.Trigger != "manual" || ci.PreTokens != 150000 || ci.PostTokens != 42000 {
		t.Errorf("compaction info = %+v, want trigger=manual pre=150000 post=42000", ci)
	}
}

// A `result` line's subtype must reach BOTH the returned TurnResult (the signal the resolve loop
// drives on) and the emitted EventResult, with Incomplete derived for a continuable stop.
func TestParseResultSubtypeContinuable(t *testing.T) {
	var got []agent.Event
	res, ok := parseStream(
		strings.NewReader(`{"type":"result","subtype":"error_max_turns","result":"partial answer","is_error":false,"session_id":"s1"}`+"\n"),
		func(e agent.Event) { got = append(got, e) },
	)
	if !ok {
		t.Fatalf("expected a result line to be parsed")
	}
	if res.Subtype != "error_max_turns" {
		t.Errorf("TurnResult.Subtype = %q, want error_max_turns", res.Subtype)
	}
	var ri *agent.ResultInfo
	for _, e := range got {
		if e.Type == agent.EventResult {
			ri = e.Result
		}
	}
	if ri == nil {
		t.Fatalf("expected an EventResult, got %+v", got)
	}
	if ri.Subtype != "error_max_turns" || !ri.Incomplete {
		t.Errorf("ResultInfo = %+v, want subtype=error_max_turns incomplete=true", ri)
	}
}

// A clean `success` result is genuinely finished: subtype carried, but NOT flagged incomplete (the
// resolve loop must not treat a settled turn as a continuable stop).
func TestParseResultSubtypeSuccessNotIncomplete(t *testing.T) {
	var got []agent.Event
	res, _ := parseStream(
		strings.NewReader(`{"type":"result","subtype":"success","result":"done","is_error":false,"session_id":"s1"}`+"\n"),
		func(e agent.Event) { got = append(got, e) },
	)
	if res.Subtype != "success" {
		t.Errorf("TurnResult.Subtype = %q, want success", res.Subtype)
	}
	for _, e := range got {
		if e.Type == agent.EventResult && e.Result.Incomplete {
			t.Errorf("success result must not be Incomplete: %+v", e.Result)
		}
	}
}
