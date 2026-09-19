package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type waitingNotificationAdapter struct {
	*fakeAdapter
	kind     string
	notified <-chan struct{}
}

func TestLiveRegistryMutesEveryNotificationConditionBeforeFanout(t *testing.T) {
	fake := &fakeAdapter{id: "notify-mute-policy"}
	s := newResolveSup(t, fake)
	reg, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	if err := reg.Import(registry.Import{
		Agents:   []registry.Agent{{Record: registry.Record{ID: "profile"}, Name: "Profile", AgentType: fake.id}},
		Projects: []registry.Project{{Record: registry.Record{ID: "project"}, Name: "Project", Path: t.TempDir()}},
		Chats:    []registry.Chat{{Record: registry.Record{ID: "chat"}, AgentID: "profile", ProjectID: "project", Title: "Chat"}},
	}); err != nil {
		t.Fatal(err)
	}
	s.SetNotificationPreferences(reg)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"success":true,"delivered":1}`))
	}))
	defer receiver.Close()
	if err := s.store.SetNotifyConfig(receiver.URL, "workspace", "secret", agent.ChannelPush); err != nil {
		t.Fatal(err)
	}
	s.notifier = notify.Fanout(notify.All(s.store))
	adapter, _ := s.Resolve(fake.id)
	conditions := []agent.Condition{agent.Finished, agent.Errored, agent.WaitingApproval, agent.WaitingFeedback, agent.WaitingInput}
	for _, muted := range []bool{false, true, false} {
		chat, _, _, err := reg.ChatContext("chat")
		if err != nil {
			t.Fatal(err)
		}
		chat.NotificationsMuted = &muted
		data, _ := json.Marshal(chat)
		if err := reg.Put("chats", "chat", data, &chat.Revision); err != nil {
			t.Fatal(err)
		}
		before := calls.Load()
		_, notesBefore, closeBefore := s.notes.Subscribe()
		closeBefore()
		for _, condition := range conditions {
			s.emit(adapter, session.Run{ID: "running", ChatID: "chat"}, condition, "test")
		}
		_, notesAfter, closeAfter := s.notes.Subscribe()
		closeAfter()
		want := len(conditions)
		if muted {
			want = 0
		}
		if got := int(calls.Load() - before); got != want || len(notesAfter)-len(notesBefore) != want {
			t.Fatalf("muted=%v: HTTP calls=%d, SSE notes=%d; want %d", muted, got, len(notesAfter)-len(notesBefore), want)
		}
	}
}

type notificationPreferencesFailure struct{}

func (notificationPreferencesFailure) NotificationsMuted(string) (bool, error) {
	return false, errors.New("database unavailable")
}

func TestNotificationPreferenceFailureDoesNotLoseRunResultOrSend(t *testing.T) {
	fake := &fakeAdapter{id: "notify-preference-failure", turns: []scriptedTurn{{text: "The result is preserved"}}}
	s := newResolveSup(t, fake)
	s.SetNotificationPreferences(notificationPreferencesFailure{})
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer receiver.Close()
	if err := s.store.SetNotifyConfig(receiver.URL, "workspace", "secret", agent.ChannelPush); err != nil {
		t.Fatal(err)
	}
	s.notifier = notify.Fanout(notify.All(s.store))
	adapter, _ := s.Resolve(fake.id)
	run, ok := s.StartTurn(adapter, StartTurnInput{ChatID: "chat", Message: "test"})
	if !ok {
		t.Fatal("turn did not start")
	}
	events := drainResolve(t, s, run.ID)
	var result, skipped bool
	for _, event := range events {
		result = result || event.Result != nil && event.Result.Text == "The result is preserved"
		skipped = skipped || event.Meta["notify"] == "not sent: notification settings unavailable"
	}
	completed, _ := s.store.GetRun(run.ID)
	if !result || !skipped || completed.Status != "done" || calls.Load() != 0 {
		t.Fatalf("result=%v skip=%v status=%s delivered=%d", result, skipped, completed.Status, calls.Load())
	}
}

func (f *waitingNotificationAdapter) RunStream(ctx context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	emit(agent.Event{Type: agent.EventInteraction, Interaction: &agent.Interaction{
		ID: "decision", Kind: f.kind, Title: "Notification check", NeedsResponse: true,
	}})
	select {
	case <-f.notified:
	case <-ctx.Done():
		return agent.TurnResult{}, ctx.Err()
	case <-time.After(5 * time.Second):
		return agent.TurnResult{}, context.DeadlineExceeded
	}
	emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: "done"}})
	return agent.TurnResult{Text: "done"}, nil
}

func TestWaitingInteractionNotifiesWhileTurnIsStillRunning(t *testing.T) {
	for _, kind := range []string{"approval", "question"} {
		t.Run(kind, func(t *testing.T) {
			notified := make(chan struct{}, 1)
			fake := &waitingNotificationAdapter{fakeAdapter: &fakeAdapter{id: "notify-waiting-" + kind}, kind: kind, notified: notified}
			s := newResolveSup(t, fake.fakeAdapter)
			agent.Register(fake)
			s = New(s.store, stream.New(), s.notifier, t.TempDir(), fake.id)
			condition := "waiting_feedback"
			if kind == "approval" {
				condition = "waiting_approval"
			}
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct{ Data map[string]string }
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload.Data["condition"] == condition {
					run, ok := s.store.GetRun(payload.Data["runId"])
					if !ok || run.Status != "running" {
						t.Error("waiting notification arrived after completion")
					}
					notified <- struct{}{}
				}
				_, _ = w.Write([]byte(`{"success":true,"delivered":1}`))
			}))
			defer receiver.Close()
			if err := s.store.SetNotifyConfig(receiver.URL, "workspace", "secret", agent.ChannelPush); err != nil {
				t.Fatal(err)
			}
			s.notifier = notify.Fanout(notify.All(s.store))
			adapter, _ := s.Resolve(fake.id)
			run, ok := s.StartTurn(adapter, StartTurnInput{ChatID: "chat", Message: "test"})
			if !ok {
				t.Fatal("turn did not start")
			}
			drainResolve(t, s, run.ID)
			completed, _ := s.store.GetRun(run.ID)
			if completed.Status != "done" {
				t.Fatalf("waiting notification did not unblock test adapter: %+v", completed)
			}
		})
	}
}

func TestTerminalTurnsDeliverUnifiedPushNotifications(t *testing.T) {
	for _, isError := range []bool{false, true} {
		name := "finished"
		if isError {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			fake := &fakeAdapter{id: "notify-" + name, turns: []scriptedTurn{{text: "Notification check", isError: isError}}}
			s := newResolveSup(t, fake)
			var received map[string]any
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
				}
				_, _ = w.Write([]byte(`{"success":true,"delivered":1}`))
			}))
			defer receiver.Close()
			if err := s.store.SetNotifyConfig(receiver.URL, "workspace", "secret", agent.ChannelPush); err != nil {
				t.Fatal(err)
			}
			s.notifier = notify.Fanout(notify.All(s.store))
			adapter, _ := s.Resolve(fake.id)
			run, ok := s.StartTurn(adapter, StartTurnInput{ChatID: "chat", Message: "test"})
			if !ok {
				t.Fatal("turn did not start")
			}
			events := drainResolve(t, s, run.ID) // Completion closes after the webhook attempt.
			data, _ := received["data"].(map[string]any)
			if data["condition"] != name || data["target_id"] != "chat" || data["runId"] != run.ID || data["agent"] != fake.id {
				t.Fatalf("turn notification missing: %+v", received)
			}
			found := false
			for _, ev := range events {
				if ev.Type == agent.EventStatus && ev.Meta["notify"] == "sent ✓" {
					found = true
				}
			}
			if !found {
				t.Fatal("delivery outcome absent from run stream")
			}
		})
	}
}
