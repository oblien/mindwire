package claude

import (
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"strings"
	"testing"
)

func TestBackgroundResultWaitsForTaskNotification(t *testing.T) {
	input := `{"type":"system","subtype":"task_started","task_id":"background-1","task_type":"local_bash","is_backgrounded":true}
{"type":"result","result":"Started the build","subtype":"success"}
{"type":"system","subtype":"background_tasks_changed","tasks":[]}
{"type":"system","subtype":"task_notification","task_id":"background-1","status":"completed","summary":"Build passed"}
{"type":"system","subtype":"task_progress","task_id":"background-1"}
`
	completed, terminal := false, 0
	result, got := parseStream(strings.NewReader(input), func(e agent.Event) {
		if e.Task != nil && e.Task.Status == "completed" {
			completed = true
		}
		if e.Type == agent.EventResult {
			terminal++
			if !completed {
				t.Error("closed stdin before native background completion")
			}
		}
	})
	if !got || result.IsError || terminal != 1 {
		t.Fatalf("result %+v got=%v terminals=%d", result, got, terminal)
	}
	_, got = parseStream(strings.NewReader(strings.Split(input, `{"type":"system","subtype":"background_tasks_changed"`)[0]), func(agent.Event) {})
	if got {
		t.Fatal("early process exit reported completed background work")
	}
}

func TestSubagentResultCannotCloseForeground(t *testing.T) {
	input := `{"type":"result","parent_tool_use_id":"child-1","result":"Child done"}
{"type":"result","result":"Parent done","subtype":"success"}
`
	terminals := 0
	r, got := parseStream(strings.NewReader(input), func(e agent.Event) {
		if e.Type == agent.EventResult {
			terminals++
		}
	})
	if !got || terminals != 1 || r.Text != "Parent done" {
		t.Fatalf("child settled parent: %+v %d", r, terminals)
	}
}
