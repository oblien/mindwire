package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/notify"
	"github.com/oblien/mindwire/packages/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/packages/daemon/internal/session"
	"github.com/oblien/mindwire/packages/daemon/internal/stream"
)

func TestRunStreamCursorAndReplayBoundary(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Fanout(nil), t.TempDir(), "codex")
	mux := http.NewServeMux()
	New(store, hub, sup).Register(mux)
	if err := store.SaveRun(session.Run{ID: "run", ChatID: "chat", Agent: "codex", Status: "done"}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second", "third"} {
		hub.Publish("run", agent.Event{Type: agent.EventText, Text: text})
	}
	hub.Close("run")
	response := serve(t, mux, "GET", "/runs/run/stream?after=1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("stream: %d %s", response.Code, response.Body.String())
	}
	var events []agent.Event
	for _, line := range strings.Split(response.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event agent.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if len(events) != 4 || events[0].Meta["stream"] != "open" || events[3].Meta["stream"] != "ready" {
		t.Fatalf("missing replay boundary: %+v", events)
	}
	for i, event := range events[1:3] {
		if event.Sequence != int64(i+2) || !event.Replay {
			t.Fatalf("wrong replay cursor/flag: %+v", event)
		}
	}
	for _, cursor := range []string{"-1", "invalid", "999999999999999999999"} {
		response := serve(t, mux, "GET", "/runs/run/stream?after="+cursor, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("cursor %s: status %d", cursor, response.Code)
		}
	}
}

func TestActiveHistoryKeepsEarlierTurnWithinSameSecond(t *testing.T) {
	user := session.Message{ID: "accepted", ChatID: "chat", Role: "user", Text: "Build", CreatedAt: "2026-09-14T10:00:00.6001Z"}
	recorded := []session.Message{
		{ID: "previous-user", Role: "user", Text: "Build", CreatedAt: "2026-09-14T10:00:00.1Z"},
		{ID: "previous-reply", Role: "assistant", Text: "First build", CreatedAt: "2026-09-14T10:00:00.3Z"},
		user,
		{ID: "saved-before-status", Role: "assistant", Text: "Second build", CreatedAt: "2026-09-14T10:00:00.9Z"},
	}
	native := []agent.Message{
		{ID: "native-user", Role: "user", Text: "Build", CreatedAt: "2026-09-14T10:00:00.100Z"},
		{ID: "native-reply", Role: "assistant", Text: "First build", CreatedAt: "2026-09-14T10:00:00.200Z"},
		{ID: "native-current-user", Role: "user", Text: "Build", CreatedAt: "2026-09-14T10:00:00.700Z"},
		{ID: "native-current-reply", Role: "assistant", Text: "Writing files", CreatedAt: "2026-09-14T10:00:00.800Z"},
	}
	run := session.Run{ID: "run", Status: "running", CreatedAt: "2026-09-14T10:00:00.6Z"}
	for _, source := range [][]agent.Message{native, {agent.Message(recorded[0]), agent.Message(recorded[1]), agent.Message(user), agent.Message(recorded[3])}} {
		got := session.HistoryBeforeRun(source, recorded, run)
		if len(got) != 3 || got[1].Text != "First build" || got[2].ID != user.ID {
			t.Fatalf("same-second turns were confused: %+v", got)
		}
		page := pageWindow(got, 1, got[1].ID, func(message agent.Message) string { return message.ID })
		if len(page) != 1 || page[0].ID != source[0].ID {
			t.Fatalf("native pagination IDs changed: %+v", page)
		}
	}
}

func TestRunSnapshotSurvivesBufferExpiryAndDaemonRestart(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, retained := range []bool{true, false} {
		hub := stream.New()
		sup := orchestrator.New(store, hub, notify.Fanout(nil), t.TempDir(), "codex")
		mux := http.NewServeMux()
		New(store, hub, sup).Register(mux)
		_ = store.SaveRun(session.Run{ID: "run", ChatID: "chat", Agent: "codex", Status: "cancelled", ReplyID: "reply"})
		_ = store.AddMessage(session.Message{ID: "reply", ChatID: "chat", Role: "assistant", Text: "Partial answer", Parts: []agent.Part{{ID: "answer", Type: "text", Text: "Partial answer"}}})
		if retained {
			hub.Publish("run", agent.Event{Type: agent.EventText, ItemID: "answer", Text: "Partial answer", Delta: true})
			hub.Close("run")
		}
		response := serve(t, mux, "GET", "/runs/run/snapshot", "")
		var snapshot struct {
			Run session.Run `json:"run"`
			stream.Snapshot
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &snapshot) != nil || len(snapshot.Parts) != 1 || snapshot.Parts[0].Text != "Partial answer" || snapshot.Run.Status != "cancelled" {
			t.Fatalf("retained=%v snapshot=%s", retained, response.Body.String())
		}
		if retained && snapshot.Sequence != 1 || !retained && snapshot.Sequence != 0 {
			t.Fatalf("wrong cursor: %+v", snapshot)
		}
	}
}

// A delayed integration reply reads its own run, even after the same chat has
// continued elsewhere. Native history is not a substitute for a turn snapshot.
func TestRunSnapshotKeepsOriginalTurnAfterNewerTurnAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []session.Message{
		{ID: "first", ChatID: "chat", Role: "assistant", Text: "First answer"},
		{ID: "input-answer", ChatID: "chat", Role: "user", Text: "Follow-up"},
		{ID: "continued", ChatID: "chat", Role: "assistant", Text: "Original turn finished"},
		{ID: "newer", ChatID: "chat", Role: "assistant", Text: "Newer dashboard answer"},
	} {
		if err := store.AddMessage(message); err != nil {
			t.Fatal(err)
		}
	}
	for _, run := range []session.Run{
		{ID: "original", ChatID: "chat", Agent: "codex", Status: "done", ReplyID: "continued", MessageIDs: []string{"first", "input-answer", "continued"}},
		{ID: "latest", ChatID: "chat", Agent: "codex", Status: "done", ReplyID: "newer"},
	} {
		if err := store.SaveRun(run); err != nil {
			t.Fatal(err)
		}
	}
	store, err = session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Fanout(nil), t.TempDir(), "codex")
	mux := http.NewServeMux()
	New(store, hub, sup).Register(mux)
	response := serve(t, mux, "GET", "/runs/original/snapshot", "")
	var snapshot orchestrator.RunSnapshot
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &snapshot) != nil {
		t.Fatalf("snapshot: %d %s", response.Code, response.Body.String())
	}
	if snapshot.Run.ID != "original" || len(snapshot.Parts) != 3 || snapshot.Parts[0].Text != "First answer" || snapshot.Parts[1].Input == nil || snapshot.Parts[1].Input.Text != "Follow-up" || snapshot.Parts[2].Text != "Original turn finished" {
		t.Fatalf("lost or mixed turns after restart: %+v", snapshot)
	}
}
