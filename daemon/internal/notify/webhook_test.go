package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestWebhookNotifierPosts verifies the generic contract: POST the agent.Notification as JSON,
// with an optional Bearer token and an optional X-Mindwire-Channel routing header.
func TestWebhookNotifierPosts(t *testing.T) {
	var gotMethod, gotAuth, gotType, gotChannel string
	var gotBody agent.Notification
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotType = r.Header.Get("Content-Type")
		gotChannel = r.Header.Get("X-Mindwire-Channel")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhook(func() (string, string, string) { return srv.URL, "chan_123", "tok_abc" })
	err := n.Notify(context.Background(), agent.Notification{
		Condition: agent.Finished, Title: "Claude finished", Body: "done", Agent: "claude-code",
		ChatID: "c1", RunID: "r1",
		Actions: []agent.Action{{ID: "approve", Label: "Approve"}},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotType != "application/json" {
		t.Errorf("content-type = %q", gotType)
	}
	if gotAuth != "Bearer tok_abc" {
		t.Errorf("auth = %q, want Bearer tok_abc", gotAuth)
	}
	if gotChannel != "chan_123" {
		t.Errorf("channel header = %q, want chan_123", gotChannel)
	}
	// The full unified notification is the body.
	if gotBody.Condition != agent.Finished || gotBody.Title != "Claude finished" {
		t.Errorf("body condition/title = %q / %q", gotBody.Condition, gotBody.Title)
	}
	if gotBody.ChatID != "c1" || gotBody.RunID != "r1" || gotBody.Agent != "claude-code" {
		t.Errorf("body routing = %+v", gotBody)
	}
	if len(gotBody.Actions) != 1 || gotBody.Actions[0].ID != "approve" {
		t.Errorf("body actions = %+v", gotBody.Actions)
	}
}

// TestWebhookNotifierNoURLIsNoop verifies that with no webhook configured, Notify is a silent
// no-op (nil error, no request) — notifications are optional and off by default.
func TestWebhookNotifierNoURLIsNoop(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhook(func() (string, string, string) { return "", "", "" })
	if err := n.Notify(context.Background(), agent.Notification{Condition: agent.Finished}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if called {
		t.Errorf("expected no request when webhook unconfigured")
	}
}

// TestWebhookNotifierNon2xxErrors verifies a non-2xx response is surfaced as an error rather than
// silently swallowed.
func TestWebhookNotifierNon2xxErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewWebhook(func() (string, string, string) { return srv.URL, "", "" })
	err := n.Notify(context.Background(), agent.Notification{Condition: agent.Finished, Title: "x"})
	if err == nil {
		t.Fatal("expected an error on a non-2xx webhook response, got nil")
	}
}
