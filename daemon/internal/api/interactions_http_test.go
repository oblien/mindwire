package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type interactionHTTPAdapter struct {
	resolveFakeAdapter
	request  agent.Interaction
	received chan agent.Inbound
	finish   chan struct{}
}

func (f *interactionHTTPAdapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{Persistent: true, Respond: true, Cancel: true, SetPermissionMode: true}
}
func (f *interactionHTTPAdapter) Settings() agent.SettingsSchema {
	return agent.SettingsSchema{Sections: []agent.Section{{Fields: []agent.Field{{
		Key: "native-permission", Canon: agent.CanonPermissionMode, Type: agent.FieldSelect,
		Options: []agent.Option{{Value: "manual"}, {Value: "acceptEdits"}, {Value: "plan"}},
	}}}}}
}
func (f *interactionHTTPAdapter) RunStream(ctx context.Context, input agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	emit(agent.Event{Type: agent.EventText, Text: "Work before the question."})
	emit(agent.Event{Type: agent.EventInteraction, Interaction: &f.request})
	for {
		select {
		case <-ctx.Done():
			return agent.TurnResult{}, ctx.Err()
		case in := <-input.Inbound:
			if in.Kind == "set_permission_mode" {
				if in.Text == "plan" {
					in.Ack <- errors.New("native policy rejected this mode")
				} else {
					in.Ack <- nil
				}
				continue
			}
			f.received <- in
			resolved := f.request
			resolved.NeedsResponse = false
			emit(agent.Event{Type: agent.EventInteraction, Interaction: &resolved})
			select {
			case <-f.finish:
			case <-ctx.Done():
				return agent.TurnResult{}, ctx.Err()
			}
			cancelled := in.Decision == "cancel"
			emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Cancelled: cancelled}})
			return agent.TurnResult{Cancelled: cancelled}, nil
		}
	}
}

func interactionHTTPFixture(t *testing.T, it agent.Interaction) (*http.ServeMux, *session.Store, *stream.Hub, session.Run, *interactionHTTPAdapter) {
	t.Helper()
	fake := &interactionHTTPAdapter{resolveFakeAdapter: resolveFakeAdapter{id: t.Name()}, request: it,
		received: make(chan agent.Inbound, 1), finish: make(chan struct{})}
	agent.Register(fake)
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Fanout(nil), t.TempDir(), fake.id)
	mux := http.NewServeMux()
	New(store, hub, sup).Register(mux)
	response := serve(t, mux, "POST", "/turns?agent="+fake.id, `{"chatId":"interaction-chat","message":"Protocol test"}`)
	var run session.Run
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &run) != nil {
		t.Fatalf("start: %s", response.Body.String())
	}
	t.Cleanup(func() { sup.Cancel(run.ID); waitRunDone(t, store, run.ID) })
	// Read the same snapshot a reconnecting app consumes. The interaction must
	// already accept replies when it becomes visible, without a second watcher.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r := serve(t, mux, "GET", "/runs/"+run.ID+"/snapshot", "")
		var snap struct {
			Parts []agent.Part `json:"parts"`
		}
		if json.Unmarshal(r.Body.Bytes(), &snap) == nil {
			for _, p := range snap.Parts {
				if p.Interaction != nil && p.Interaction.NeedsResponse {
					return mux, store, hub, run, fake
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pending interaction never appeared")
	return nil, nil, nil, session.Run{}, nil
}

func TestRespondFormHTTPIsAtomicAndReopenable(t *testing.T) {
	it := agent.Interaction{ID: "native-request", Kind: "form", NeedsResponse: true,
		Questions: []agent.Question{
			{ID: "theme", Title: "Theme?", Options: []agent.Action{{ID: "dark", Label: "Dark"}}},
			{ID: "features", Title: "Features?", MultiSelect: true, Options: []agent.Action{{ID: "search"}, {ID: "export"}}},
		}, Meta: map[string]any{"requestId": "private-correlator"}}
	mux, store, _, run, fake := interactionHTTPFixture(t, it)
	path := "/runs/" + run.ID + "/respond"
	for _, body := range []string{
		`{"interactionId":"native-request","answers":{"theme":{"options":["dark"]}}}`,
		`{"interactionId":"native-request","answers":{"theme":{"options":["unknown"]},"features":{"options":["search"]}}}`,
	} {
		if r := serve(t, mux, "POST", path, body); r.Code != 400 {
			t.Fatalf("invalid form: %d %s", r.Code, r.Body.String())
		}
	}
	if r := serve(t, mux, "POST", path, `{"interactionId":"stale"}`); r.Code != 409 {
		t.Fatalf("stale request: %d", r.Code)
	}
	for mode, status := range map[string]int{"invalid": 400, "plan": 400, "acceptEdits": 202} {
		r := serve(t, mux, "POST", "/runs/"+run.ID+"/set-permission-mode", `{"mode":"`+mode+`"}`)
		if r.Code != status {
			t.Fatalf("permission %s: %d %s", mode, r.Code, r.Body.String())
		}
	}
	body := `{"interactionId":"native-request","answers":{"theme":{"options":["dark"],"text":"Less contrast"},"features":{"options":["search","export"]}},"meta":{"requestId":"forged"}}`
	statuses := make(chan int, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRecorder()
			mux.ServeHTTP(r, httptest.NewRequest("POST", path, strings.NewReader(body)))
			statuses <- r.Code
		}()
	}
	wg.Wait()
	close(statuses)
	accepted := 0
	for status := range statuses {
		if status == 202 {
			accepted++
		} else if status != 409 {
			t.Fatalf("reply status: %d", status)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d replies for one request", accepted)
	}
	select {
	case received := <-fake.received:
		if len(received.Answers) != 2 || received.Answers["theme"].Text != "Less contrast" || len(received.Answers["features"].Options) != 2 || received.Meta["requestId"] != "private-correlator" {
			t.Fatalf("lost answers or trusted client correlator: %+v", received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("answer not delivered")
	}
	close(fake.finish)
	waitRunDone(t, store, run.ID)
	completed, _ := store.GetRun(run.ID)
	if completed.Status != "done" {
		t.Fatalf("run: %+v", completed)
	}
	assertSavedInteractionsResolved(t, store, run.ChatID)
}

func TestNativeRejectAndStopKeepsHistoryWithoutError(t *testing.T) {
	it := agent.Interaction{ID: "approval", Kind: "approval", NeedsResponse: true,
		Options: []agent.Action{{ID: "allow"}, {ID: "cancel"}}}
	mux, store, _, run, fake := interactionHTTPFixture(t, it)
	path := "/runs/" + run.ID + "/respond"
	for _, decision := range []string{"", "allow_all", "deny"} {
		if r := serve(t, mux, "POST", path, `{"interactionId":"approval","decision":"`+decision+`"}`); r.Code != 400 {
			t.Fatalf("unoffered action accepted: %s", decision)
		}
	}
	if r := serve(t, mux, "POST", path, `{"interactionId":"approval","decision":"cancel"}`); r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	close(fake.finish)
	waitRunDone(t, store, run.ID)
	completed, _ := store.GetRun(run.ID)
	if completed.Status != "cancelled" || completed.Error != "" {
		t.Fatalf("rejection became failure: %+v", completed)
	}
	assertSavedInteractionsResolved(t, store, run.ChatID)
}

func assertSavedInteractionsResolved(t *testing.T, store *session.Store, chatID string) {
	t.Helper()
	messages := store.Messages(chatID)
	if len(messages) != 2 || messages[1].Text != "Work before the question." {
		t.Fatalf("history lost partial answer: %+v", messages)
	}
	found := false
	for _, part := range messages[1].Parts {
		if part.Interaction != nil {
			found = true
			if part.Interaction.NeedsResponse {
				t.Fatal("history still asks for approval")
			}
		}
	}
	if !found {
		t.Fatal("history lost interaction")
	}
}
