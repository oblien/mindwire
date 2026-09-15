package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

func TestPushNotificationConfigSurvivesRestartAndKeepsRouting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	notifier := notify.Fanout(notify.All(store)) // Created BEFORE configuration, as in main.
	var got struct {
		Title, Body string
		Data        map[string]string
	}
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer send-secret" || r.Header.Get("X-Mindwire-Channel") != "workspace" {
			t.Error("notification auth or channel missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"delivered":1,"failed":0,"devices":1}`))
	}))
	defer receiver.Close()
	hub := stream.New()
	sup := orchestrator.New(store, hub, notifier, t.TempDir(), "claude-code")
	mux := http.NewServeMux()
	New(store, hub, sup).Register(mux)
	body, _ := json.Marshal(map[string]string{"url": receiver.URL, "channel": "workspace", "token": "send-secret", "format": "push"})
	if r := serve(t, mux, "PUT", "/notify/config", string(body)); r.Code != http.StatusNoContent {
		t.Fatalf("configure: %d %s", r.Code, r.Body.String())
	}
	status := serve(t, mux, "GET", "/notify/config", "")
	if !strings.Contains(status.Body.String(), `"format":"push"`) || !strings.Contains(status.Body.String(), `"hasToken":true`) || strings.Contains(status.Body.String(), "send-secret") {
		t.Fatalf("configuration status: %s", status.Body.String())
	}
	note := agent.Notification{Condition: agent.WaitingApproval, Title: "Approval needed", Body: "Edit this file?",
		Agent: "codex", ChatID: "chat-1", RunID: "run-1", Actions: []agent.Action{{ID: "approve", Label: "Approve"}}}
	for _, restart := range []bool{false, true} {
		if restart {
			reloaded, err := session.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			notifier = notify.Fanout(notify.All(reloaded))
		}
		if err := notifier.Notify(context.Background(), note); err != nil {
			t.Fatal(err)
		}
		if got.Title != note.Title || got.Body != note.Body || got.Data["target_type"] != "chat" || got.Data["target_id"] != "chat-1" || got.Data["chatId"] != "chat-1" || got.Data["runId"] != "run-1" || got.Data["agent"] != "codex" || got.Data["condition"] != "waiting_approval" {
			t.Fatalf("restart=%v: metadata lost: %+v", restart, got)
		}
		var actions []agent.Action
		if json.Unmarshal([]byte(got.Data["actions"]), &actions) != nil || len(actions) != 1 || actions[0].ID != "approve" {
			t.Fatalf("actions must survive string-valued FCM data: %+v", got.Data)
		}
	}
	if r := serve(t, mux, "PUT", "/notify/config", `{"url":"https://example.com","channel":"x","format":"unknown"}`); r.Code != http.StatusBadRequest {
		t.Fatalf("unknown format accepted: %d", r.Code)
	}
}
