package stream

import (
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestSnapshotThenCursorKeepsPartialTextAndToolProgress(t *testing.T) {
	hub := New()
	hub.Publish("run", agent.Event{Type: agent.EventText, ItemID: "answer", Text: "Partial ", Delta: true})
	hub.Publish("run", agent.Event{Type: agent.EventToolUse, Tool: &agent.ToolEvent{ID: "file", Name: "apply_patch",
		Action: &agent.ToolAction{Kind: agent.KindFileEdit, Files: []agent.FileChange{{Path: "App.swift", Op: "create"}}}}})
	before, ok := hub.Snapshot("run")
	if !ok || before.Sequence != 2 || len(before.Parts) != 2 || before.Parts[0].Text != "Partial " {
		t.Fatalf("in-flight components missing: %+v", before)
	}
	hub.Publish("run", agent.Event{Type: agent.EventToolResult, Tool: &agent.ToolEvent{ID: "file", Output: "Created App.swift",
		Action: &agent.ToolAction{Kind: agent.KindFileEdit, Files: []agent.FileChange{{Path: "App.swift", Op: "create", Diff: "@@ -0,0 +1 @@\n+let value = 1"}}}}})
	hub.Publish("run", agent.Event{Type: agent.EventText, ItemID: "answer", Text: "answer", Delta: true})
	hub.Close("run")
	after, _ := hub.Snapshot("run")
	if after.Sequence != 4 || after.Parts[0].Text != "Partial answer" || after.Parts[1].Tool.Action.Files[0].Diff == "" {
		t.Fatalf("final output lost: %+v", after)
	}
	if before.Parts[0].Text != "Partial " || before.Parts[1].Tool.Output != "" || before.Parts[1].Tool.Action.Files[0].Diff != "" {
		t.Fatal("applying an event mutated an already returned snapshot")
	}
	replay, _, done, cancel := hub.SubscribeAfter("run", before.Sequence)
	defer cancel()
	if !done || len(replay) != 2 || replay[0].Sequence != 3 || replay[1].Sequence != 4 {
		t.Fatalf("snapshot-to-stream transition lost/replayed output: %+v", replay)
	}
}

func TestTerminalResultMaterializesMissingTextWithoutDuplication(t *testing.T) {
	for _, prefix := range []string{"", "Partial ", "Partial answer"} {
		var transcript Transcript
		if prefix != "" {
			transcript.Apply(agent.Event{Type: agent.EventText, ItemID: "reply", Text: prefix, Delta: true})
		}
		transcript.Apply(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "Partial answer"}})
		transcript.Apply(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "Partial answer"}})
		snapshot := transcript.Snapshot(true)
		if len(snapshot.Parts) != 1 || snapshot.Parts[0].Text != "Partial answer" {
			t.Fatalf("prefix=%q parts=%+v", prefix, snapshot.Parts)
		}
	}
}
