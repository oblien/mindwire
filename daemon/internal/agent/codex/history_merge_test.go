package codex

import (
	"reflect"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func historyMessage(id, role, text, at string, parts ...agent.Part) agent.Message {
	return agent.Message{ID: id, ChatID: "chat", Role: role, Text: text, CreatedAt: at, Parts: parts}
}

func historyNotice(id, kind, detail string) agent.Part {
	return agent.Part{Type: "interaction", Interaction: &agent.Interaction{ID: id, Kind: kind, Title: "Codex notice", Detail: detail}}
}

func TestRecordedNoticesMergeIntoTheCorrectRepeatedQuestion(t *testing.T) {
	native := []agent.Message{
		historyMessage("u1", "user", "Try again", "2026-09-11T10:00:00.100Z"),
		historyMessage("a1", "assistant", "First answer", "2026-09-11T10:00:01Z", agent.Part{Type: "text", ID: "first", Text: "First answer"}),
		historyMessage("u2", "user", "Try again", "2026-09-11T10:01:00.100Z"),
		historyMessage("a2", "assistant", "Second answer", "2026-09-11T10:01:01Z", agent.Part{Type: "text", ID: "second", Text: "Second answer"}),
	}
	recorded := []agent.Message{
		historyMessage("record-user", "user", "Try again", "2026-09-11T10:01:00Z"),
		historyMessage("record-reply", "assistant", "Second answer", "2026-09-11T10:01:02Z",
			historyNotice("warning", "warning", "Configuration warning"), agent.Part{Type: "text", ID: "second", Text: "Second answer"}),
	}
	result := mergeRecordedHistory(native, recorded)
	if len(result) != 4 || len(result[1].Parts) != 1 || len(result[3].Parts) != 2 || result[3].Parts[0].Interaction.Detail != "Configuration warning" {
		t.Fatalf("wrong turn supplemented: %+v", result)
	}
	if result[3].Text != "Second answer" {
		t.Fatalf("duplicated answer: %q", result[3].Text)
	}
	if again := mergeRecordedHistory(result, recorded); !reflect.DeepEqual(again, result) {
		t.Fatal("repeated reload duplicated the recorded components")
	}
}

func TestRecordedPartsRespectNativeSnapshotsAndCompactionBoundaries(t *testing.T) {
	before := agent.Part{Type: "text", ID: "before", Text: "Before compaction"}
	boundary := agent.Part{Type: "compaction", Compaction: &agent.CompactionInfo{Summary: "Summary"}}
	native := []agent.Message{
		historyMessage("user", "user", "Continue", "2026-09-11T10:00:00Z"),
		historyMessage("before", "assistant", before.Text, "2026-09-11T10:00:01Z", before),
		historyMessage("compact", "system", "", "2026-09-11T10:00:02Z", boundary),
		historyMessage("after", "assistant", "Final", "2026-09-11T10:00:03Z",
			agent.Part{Type: "tool", Tool: &agent.ToolPart{ID: "tool", Name: "shell", Output: "Complete output"}},
			agent.Part{Type: "text", ID: "answer", Text: "Final"}),
	}
	recorded := []agent.Message{native[0], historyMessage("recorded", "assistant", "Draft answer", "2026-09-11T10:00:04Z",
		before, boundary, historyNotice("error", "error", "Optional tool unavailable"),
		agent.Part{Type: "tool", Tool: &agent.ToolPart{ID: "tool", Name: "shell", Output: "Partial output"}},
		agent.Part{Type: "text", ID: "answer", Text: "Draft answer"})}
	result := mergeRecordedHistory(native, recorded)
	if len(result) != 4 || result[2].Role != "system" || len(result[1].Parts) != 1 || len(result[3].Parts) != 3 {
		t.Fatalf("compaction split was lost: %+v", result)
	}
	if result[3].Parts[0].Interaction.Detail != "Optional tool unavailable" || result[3].Parts[1].Tool.Output != "Complete output" || result[3].Parts[2].Text != "Final" {
		t.Fatalf("final snapshots overwritten: %+v", result[3].Parts)
	}
}

func TestRecordedFailureWithoutNativeEchoSurvivesBetweenNativeTurns(t *testing.T) {
	native := []agent.Message{
		historyMessage("u1", "user", "First", "2026-09-11T10:00:00Z"), historyMessage("a1", "assistant", "Done", "2026-09-11T10:00:01Z"),
		historyMessage("u3", "user", "Third", "2026-09-11T10:02:00Z"), historyMessage("a3", "assistant", "Done", "2026-09-11T10:02:01Z"),
	}
	recorded := []agent.Message{
		historyMessage("u2", "user", "Second", "2026-09-11T10:01:00Z"),
		historyMessage("a2", "assistant", "", "2026-09-11T10:01:01Z", historyNotice("failure", "error", "Deployment not found (HTTP 404)")),
	}
	result := mergeRecordedHistory(native, recorded)
	if len(result) != 6 || result[2].ID != "u2" || result[3].ID != "a2" || result[4].ID != "u3" {
		t.Fatalf("failed turn lost/reordered: %+v", result)
	}
}

func TestNativeErrorsAndWarningsSurviveCanonicalItemUpgrade(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"Continue"}}`,
		`{"type":"event_msg","payload":{"type":"warning","message":"An optional feature is unavailable."}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done"}]}}`,
		`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"answer","content":[{"type":"output_text","text":"Done"}]}}}`,
		`{"type":"event_msg","payload":{"type":"error","message":"Stream disconnected","codex_error_info":{"response_stream_disconnected":{"http_status_code":503}}}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","error":{"message":"Stream disconnected","codex_error_info":{"response_stream_disconnected":{"http_status_code":503}}}}}`,
	}
	result, err := parseRollout(strings.NewReader(strings.Join(lines, "\n")), "chat")
	if err != nil || len(result) != 2 {
		t.Fatalf("messages=%+v error=%v", result, err)
	}
	parts := result[1].Parts
	if len(parts) != 3 || parts[0].Interaction.Kind != "warning" || parts[1].Text != "Done" || !strings.Contains(parts[2].Interaction.Detail, "HTTP 503") {
		t.Fatalf("warning lost or error duplicated: %+v", parts)
	}
}
