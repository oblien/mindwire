package codex

import (
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// parseRollout normalizes a rollout transcript: user/assistant text, reasoning, and tool calls paired
// to their outputs by call_id, with all assistant activity between two user turns folded into one
// assistant message of ordered parts.
func TestParseRolloutFullTurn(t *testing.T) {
	lines := []string{
		`{"timestamp":"t0","type":"session_meta","payload":{"id":"sess-1","cwd":"/repo"}}`,
		`{"timestamp":"t1","type":"event_msg","payload":{"type":"user_message","message":"add a test"}}`,
		`{"timestamp":"t2","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"I will add a test"}]}}`,
		`{"timestamp":"t3","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"ls\"]}","call_id":"call-1"}}`,
		`{"timestamp":"t4","type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":{"content":"file.txt","success":true}}}`,
		`{"timestamp":"t5","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch","call_id":"call-2"}}`,
		`{"timestamp":"t6","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-2","output":"done"}}`,
		`{"timestamp":"t7","type":"event_msg","payload":{"type":"agent_message","message":"Added the test."}}`,
		`{"timestamp":"t8","type":"token_count","payload":{"total":42}}`,
	}
	msgs, err := parseRollout(strings.NewReader(strings.Join(lines, "\n")+"\n"), "chat-1")
	if err != nil {
		t.Fatalf("parseRollout: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2 (one user, one assistant)\n%+v", len(msgs), msgs)
	}

	if msgs[0].Role != "user" || msgs[0].Text != "add a test" || msgs[0].ChatID != "chat-1" {
		t.Errorf("user message = %+v", msgs[0])
	}

	a := msgs[1]
	if a.Role != "assistant" || a.Text != "Added the test." {
		t.Errorf("assistant message text = %+v", a)
	}
	// Ordered parts: thinking, tool(shell), tool(apply_patch), text.
	if got := partTypes(a.Parts); !equalStrings(got, []string{"thinking", "tool", "tool", "text"}) {
		t.Fatalf("assistant part types = %v, want [thinking tool tool text]", got)
	}
	if a.Parts[0].Text != "I will add a test" {
		t.Errorf("thinking part = %q", a.Parts[0].Text)
	}

	shell := findTool(a.Parts, "call-1")
	if shell == nil || shell.Name != "shell" || shell.Output != "file.txt" || shell.IsError {
		t.Errorf("shell tool part = %+v", shell)
	}
	// function_call arguments are valid JSON → preserved verbatim as the tool Input.
	if shell != nil && !strings.Contains(string(shell.Input), `"command"`) {
		t.Errorf("shell input should carry the JSON args, got %s", shell.Input)
	}

	patch := findTool(a.Parts, "call-2")
	if patch == nil || patch.Name != "apply_patch" || patch.Output != "done" {
		t.Errorf("apply_patch tool part = %+v", patch)
	}
	// A non-JSON custom_tool_call input is encoded as a JSON string.
	if patch != nil && !strings.HasPrefix(string(patch.Input), `"`) {
		t.Errorf("non-JSON input should be a JSON string, got %s", patch.Input)
	}
}

// A second user turn flushes the open assistant message so the next assistant activity is a new one.
func TestParseRolloutTwoTurns(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"one"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"first reply"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"two"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"second reply"}}`,
	}
	msgs, err := parseRollout(strings.NewReader(strings.Join(lines, "\n")+"\n"), "c")
	if err != nil {
		t.Fatalf("parseRollout: %v", err)
	}
	roles := []string{}
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	if !equalStrings(roles, []string{"user", "assistant", "user", "assistant"}) {
		t.Fatalf("roles = %v, want [user assistant user assistant]", roles)
	}
	if msgs[1].Text != "first reply" || msgs[3].Text != "second reply" {
		t.Errorf("assistant texts = %q / %q", msgs[1].Text, msgs[3].Text)
	}
}

// A tool output marked unsuccessful sets IsError on the paired tool part.
func TestParseRolloutToolFailure(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"run it"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{}","call_id":"x"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"x","output":{"content":"nope","success":false}}}`,
	}
	msgs, err := parseRollout(strings.NewReader(strings.Join(lines, "\n")+"\n"), "c")
	if err != nil {
		t.Fatalf("parseRollout: %v", err)
	}
	tp := findTool(msgs[1].Parts, "x")
	if tp == nil || !tp.IsError || tp.Output != "nope" {
		t.Errorf("failed tool part = %+v, want IsError with output nope", tp)
	}
}

// Rollout tool calls carry a deep-normalized action: shell arguments ({command:[…], workdir}) become a
// shell action with the joined command line and folded stdout; an apply_patch body becomes a file_edit
// action whose per-file section is the best-effort diff.
func TestParseRolloutToolActions(t *testing.T) {
	lines := []string{
		`{"timestamp":"t0","type":"event_msg","payload":{"type":"user_message","message":"go"}}`,
		`{"timestamp":"t1","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"go\",\"test\"],\"workdir\":\"/repo\"}","call_id":"c1"}}`,
		`{"timestamp":"t2","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":{"content":"ok","success":true}}}`,
		`{"timestamp":"t3","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: foo.go\n@@\n-old\n+new\n*** End Patch","call_id":"c2"}}`,
		`{"timestamp":"t4","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"c2","output":"done"}}`,
	}
	msgs, err := parseRollout(strings.NewReader(strings.Join(lines, "\n")+"\n"), "c")
	if err != nil {
		t.Fatalf("parseRollout: %v", err)
	}

	shell := findTool(msgs[1].Parts, "c1")
	if shell == nil || shell.Action == nil || shell.Action.Shell == nil {
		t.Fatalf("shell action = %+v", shell)
	}
	if shell.Action.Shell.Command != "go test" || shell.Action.Shell.Cwd != "/repo" {
		t.Errorf("shell action = %+v", shell.Action.Shell)
	}
	if shell.Action.Shell.Stdout != "ok" {
		t.Errorf("shell stdout should be folded in, got %q", shell.Action.Shell.Stdout)
	}

	patch := findTool(msgs[1].Parts, "c2")
	if patch == nil || patch.Action == nil || patch.Action.Kind != agent.KindFileEdit || len(patch.Action.Files) != 1 {
		t.Fatalf("apply_patch action = %+v", patch)
	}
	f := patch.Action.Files[0]
	if f.Path != "foo.go" || f.Op != "edit" {
		t.Errorf("patch file = %+v", f)
	}
	if !strings.Contains(f.Diff, "-old") || !strings.Contains(f.Diff, "+new") {
		t.Errorf("patch diff should carry the body:\n%s", f.Diff)
	}
}

// A `compacted` rollout record surfaces a standalone system/compaction message carrying the
// continuation summary, and it breaks assistant grouping so activity after it starts a fresh
// assistant message. The replacement_history it also carries is NOT re-emitted (F4b).
func TestParseRolloutCompaction(t *testing.T) {
	lines := []string{
		`{"timestamp":"t0","type":"event_msg","payload":{"type":"user_message","message":"go"}}`,
		`{"timestamp":"t1","type":"event_msg","payload":{"type":"agent_message","message":"working"}}`,
		`{"timestamp":"t2","type":"compacted","payload":{"message":"summary of the conversation so far","replacement_history":[{"type":"message","role":"user","content":"SHOULD NOT APPEAR"}]}}`,
		`{"timestamp":"t3","type":"event_msg","payload":{"type":"agent_message","message":"after compaction"}}`,
	}
	msgs, err := parseRollout(strings.NewReader(strings.Join(lines, "\n")+"\n"), "c")
	if err != nil {
		t.Fatalf("parseRollout: %v", err)
	}
	var comp *agent.Message
	for i := range msgs {
		if msgs[i].Role == "system" && len(msgs[i].Parts) == 1 && msgs[i].Parts[0].Type == "compaction" {
			comp = &msgs[i]
		}
		if strings.Contains(msgs[i].Text, "SHOULD NOT APPEAR") {
			t.Fatalf("replacement_history was re-emitted: %+v", msgs[i])
		}
	}
	if comp == nil {
		t.Fatalf("no compaction message surfaced; got %+v", msgs)
	}
	if comp.Parts[0].Compaction == nil || !strings.Contains(comp.Parts[0].Compaction.Summary, "summary of the conversation") {
		t.Errorf("compaction summary = %+v", comp.Parts[0].Compaction)
	}
	// The post-compaction assistant message must be distinct from the pre-compaction one (grouping reset).
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || last.Text != "after compaction" {
		t.Errorf("post-compaction assistant message = %+v, want a fresh 'after compaction'", last)
	}
}

func partTypes(parts []agent.Part) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p.Type
	}
	return out
}

func findTool(parts []agent.Part, id string) *agent.ToolPart {
	for i := range parts {
		if parts[i].Tool != nil && parts[i].Tool.ID == id {
			return parts[i].Tool
		}
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
