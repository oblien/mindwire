package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

func TestWorkspaceMutesThroughHTTPPreserveRunStreamAndHistory(t *testing.T) {
	fake := resolveFakeAdapter{id: "http-notification-preferences"}
	agent.Register(fake)
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Open(filepath.Join(dir, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Fanout(notify.All(store)), dir, fake.id)
	api := New(store, hub, sup, reg)
	if err := api.InitError(); err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	mux := http.NewServeMux()
	api.Register(mux)
	authed := Auth("daemon-secret", mux)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer daemon-secret")
		authed.ServeHTTP(w, r)
	})
	var delivered atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered.Add(1)
		_, _ = w.Write([]byte(`{"success":true,"delivered":1}`))
	}))
	defer receiver.Close()
	config, _ := json.Marshal(map[string]string{"url": receiver.URL, "channel": "workspace", "token": "send-secret", "format": "push"})
	if got := serve(t, handler, "PUT", "/notify/config", string(config)); got.Code != 204 {
		t.Fatal(got.Body.String())
	}
	batch, _ := json.Marshal(registry.Import{
		Agents:   []registry.Agent{{Record: registry.Record{ID: "profile"}, Name: "Profile", AgentType: fake.id}},
		Projects: []registry.Project{{Record: registry.Record{ID: "project"}, Name: "Project", Path: dir}},
		Chats:    []registry.Chat{{Record: registry.Record{ID: "chat"}, AgentID: "profile", ProjectID: "project", Title: "Chat"}},
	})
	if got := serve(t, handler, "POST", "/workspace/import", string(batch)); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	for _, muted := range []bool{true, false} {
		var snapshot registry.Snapshot
		if err := json.Unmarshal(serve(t, handler, "GET", "/workspace", "").Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		profile := snapshot.Agents[0]
		profile.NotificationsMuted = &muted
		update, _ := json.Marshal(map[string]any{"record": profile, "expectedRevision": profile.Revision})
		got := serve(t, handler, "PUT", "/workspace/agents/profile", string(update))
		if got.Code != 200 {
			t.Fatal(got.Body.String())
		}
		if err := json.Unmarshal(got.Body.Bytes(), &snapshot); err != nil || snapshot.Agents[0].NotificationsMuted == nil || *snapshot.Agents[0].NotificationsMuted != muted {
			t.Fatalf("preference not acknowledged: %s", got.Body.String())
		}
		turn := serve(t, handler, "POST", "/turns", `{"chatId":"chat","message":"test"}`)
		var run session.Run
		if turn.Code != 202 || json.Unmarshal(turn.Body.Bytes(), &run) != nil {
			t.Fatalf("turn: %s", turn.Body.String())
		}
		sup.Wait()
		feed := serve(t, handler, "GET", "/runs/"+run.ID+"/stream", "").Body.String()
		outcome, wantDelivered := "sent ✓", int32(1)
		if muted {
			outcome, wantDelivered = "muted", 0
		}
		if !strings.Contains(feed, `"notify":"`+outcome+`"`) || !strings.Contains(feed, "done") || delivered.Load() != wantDelivered {
			t.Fatalf("muted=%v delivered=%d stream=%s", muted, delivered.Load(), feed)
		}
		history := serve(t, handler, "GET", "/chats/chat/messages", "")
		if history.Code != 200 || !strings.Contains(history.Body.String(), "done") {
			t.Fatalf("muting affected history: %s", history.Body.String())
		}
	}
}

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
