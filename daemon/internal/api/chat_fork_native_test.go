package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
)

// Runs against the real native CLI and the disposable provider used by the
// interoperability fixtures. Only transcripts in the fixture home are read.
func checkNativeHistoricalFork(t *testing.T, mux *http.ServeMux, sup *orchestrator.Supervisor, store *session.Store,
	chat registry.Chat, harness, home string, requestBodies func() []string) string {
	t.Helper()
	history := func(id string) []agent.Message {
		t.Helper()
		r := serve(t, mux, "GET", "/chats/"+id+"/messages?agent="+harness, "")
		var rows []agent.Message
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &rows) != nil {
			t.Fatalf("history: %d %s", r.Code, r.Body.String())
		}
		return rows
	}
	original := history(chat.ID)
	var selected agent.Message
	for _, row := range original {
		if row.Role == "user" && row.Text == "Continued from Mindwire" {
			selected = row
		}
	}
	if selected.ID == "" || !selected.CanFork {
		t.Fatalf("historical prompt has no fork action: %+v", selected)
	}
	// Capture the original native transcript, not databases/logs a CLI can update.
	files := map[string][]byte{}
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".jsonl") && strings.Contains(filepath.Base(path), chat.SessionID) {
			files[path], _ = os.ReadFile(path)
		}
		return nil
	})
	if len(files) == 0 {
		t.Fatal("fixture has no source transcript")
	}
	id := "native-fork-" + harness
	body, _ := json.Marshal(map[string]string{"newChatId": id, "beforeMessageId": selected.ID})
	fork := func() {
		t.Helper()
		r := serve(t, mux, "POST", "/chats/"+chat.ID+"/fork?agent="+harness, string(body))
		if r.Code != 200 {
			t.Fatalf("fork: %d %s", r.Code, r.Body.String())
		}
	}
	fork()
	fork() // A lost acknowledgement must not create a second branch.
	prefix := history(id)
	if len(prefix) != 2 || prefix[0].Text != "Started in native CLI" || prefix[1].Text != "Native answer 1" {
		t.Fatalf("pending branch leaked or lost history: %+v", prefix)
	}
	post, _ := json.Marshal(map[string]string{"chatId": id, "message": "Edited historical prompt", "requestId": "edit-request"})
	r := serve(t, mux, "POST", "/turns?agent="+harness, string(post))
	var run session.Run
	if r.Code != 202 || json.Unmarshal(r.Body.Bytes(), &run) != nil {
		t.Fatalf("send fork: %d %s", r.Code, r.Body.String())
	}
	defer sup.Cancel(run.ID)
	deadline := time.Now().Add(45 * time.Second)
	for sup.Busy(id) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	finished, _ := store.GetRun(run.ID)
	if finished.Status != "done" {
		t.Fatalf("fork turn: %+v", finished)
	}
	sid := store.Session(harness, id)
	if sid == "" || sid == chat.SessionID || store.PendingFork(harness, id) != nil {
		t.Fatal("fork did not commit a distinct native identity")
	}
	fork() // Receipt remains idempotent after native initialization.
	r = serve(t, mux, "POST", "/turns?agent="+harness, string(post))
	var replay session.Run
	if r.Code != 202 || json.Unmarshal(r.Body.Bytes(), &replay) != nil || replay.ID != run.ID {
		t.Fatalf("duplicate submission: %d %s", r.Code, r.Body.String())
	}
	rows := history(id)
	if len(rows) != 4 || rows[2].Text != "Edited historical prompt" {
		t.Fatalf("completed fork history: %+v", rows)
	}
	found := false
	for _, request := range requestBodies() {
		if !strings.Contains(request, "Edited historical prompt") {
			continue
		}
		found = true
		if !strings.Contains(request, "Native answer 1") || strings.Contains(request, "Continued from Mindwire") || strings.Contains(request, "Native answer 2") || strings.Contains(request, "Back in native CLI") || strings.Contains(request, "Native answer 3") {
			t.Fatal("fork model context did not match the selected history prefix")
		}
	}
	if !found {
		t.Fatal("fork did not reach the local provider")
	}
	for path, before := range files {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("fork changed the source native transcript")
		}
	}
	if after := history(chat.ID); len(after) != len(original) {
		t.Fatal("fork changed original history")
	}
	// Editing the first prompt must start with no conversation prefix.
	firstID := id + "-first"
	body, _ = json.Marshal(map[string]string{"newChatId": firstID, "beforeMessageId": original[0].ID})
	r = serve(t, mux, "POST", "/chats/"+chat.ID+"/fork?agent="+harness, string(body))
	if r.Code != 200 {
		t.Fatalf("first-message fork: %d %s", r.Code, r.Body.String())
	}
	if got := history(firstID); len(got) != 0 {
		t.Fatalf("first-message fork retained future: %s", fmt.Sprint(got))
	}
	send := func(chatID, prompt string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"chatId": chatID, "message": prompt, "requestId": chatID + "-send"})
		r := serve(t, mux, "POST", "/turns?agent="+harness, string(body))
		var run session.Run
		if r.Code != 202 || json.Unmarshal(r.Body.Bytes(), &run) != nil {
			t.Fatalf("send branch: %d %s", r.Code, r.Body.String())
		}
		defer sup.Cancel(run.ID)
		deadline := time.Now().Add(45 * time.Second)
		for sup.Busy(chatID) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if done, _ := store.GetRun(run.ID); done.Status != "done" {
			t.Fatalf("branch turn: %+v", done)
		}
	}
	send(firstID, "Replacement first prompt")
	if got := history(firstID); len(got) != 2 || got[0].Text != "Replacement first prompt" {
		t.Fatalf("first-message resend inherited the source: %+v", got)
	}
	requests := requestBodies()
	last := requests[len(requests)-1]
	if !strings.Contains(last, "Replacement first prompt") || strings.Contains(last, "Started in native CLI") || strings.Contains(last, "Native answer 1") {
		t.Fatal("first-message resend inherited model context")
	}
	// A branch is a native conversation too; it must be possible to edit it again.
	nestedID := id + "-nested"
	body, _ = json.Marshal(map[string]string{"newChatId": nestedID, "beforeMessageId": rows[2].ID})
	r = serve(t, mux, "POST", "/chats/"+id+"/fork?agent="+harness, string(body))
	if r.Code != 200 {
		t.Fatalf("nested fork: %d %s", r.Code, r.Body.String())
	}
	send(nestedID, "Edited the branch again")
	if got := history(nestedID); len(got) != 4 || got[0].Text != "Started in native CLI" || got[2].Text != "Edited the branch again" {
		t.Fatalf("nested fork lost its history prefix: %+v", got)
	}
	requests = requestBodies()
	last = requests[len(requests)-1]
	if !strings.Contains(last, "Native answer 1") || strings.Contains(last, "Edited historical prompt") || strings.Contains(last, "Continued from Mindwire") {
		t.Fatal("nested fork received discarded context")
	}
	for path, before := range files {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("first-message or nested fork changed the source native transcript")
		}
	}
	return sid
}
