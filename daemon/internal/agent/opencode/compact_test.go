package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// compactServer is a minimal opencode server for the compaction path: it serves the SSE bus and the
// /session/{id}/summarize route, recording the summarize body and — once summarize is called — emitting
// session.compacted (and optionally session.idle) so converse can drive an on-demand compaction to a
// terminal result with no real binary. No /session route: a compaction always resumes an existing id.
type compactServer struct {
	sessionID  string
	summarized chan struct{}
	once       sync.Once
	emitIdle   bool // also emit session.idle after session.compacted (exercises the backstop terminal)

	mu   sync.Mutex
	body json.RawMessage
}

func newCompactServer(emitIdle bool) *compactServer {
	return &compactServer{sessionID: "ses_test", summarized: make(chan struct{}), emitIdle: emitIdle}
}

func (s *compactServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/event":
			s.serveEvents(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/summarize"):
			b, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.body = b
			s.mu.Unlock()
			s.once.Do(func() { close(s.summarized) })
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (s *compactServer) serveEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	frame := func(typ string, props any) map[string]any {
		return map[string]any{"type": typ, "properties": props}
	}

	send(frame("server.connected", map[string]any{}))
	select {
	case <-s.summarized:
	case <-r.Context().Done():
		return
	}
	send(frame("session.compacted", map[string]any{"sessionID": s.sessionID}))
	if s.emitIdle {
		send(frame("session.idle", map[string]any{"sessionID": s.sessionID}))
	}
}

// TestConverseCompact drives the on-demand compaction path: with compact=true, converse resumes the
// session, POSTs /summarize with the explicit {providerID,modelID} the route requires, and the
// session.compacted frame surfaces as EventCompaction{trigger:"manual"} and terminates the run. Proves
// the CompactModule wire path end-to-end without a real opencode binary.
func TestConverseCompact(t *testing.T) {
	sc := newCompactServer(false) // no session.idle: session.compacted alone must terminate a compact run
	ts := httptest.NewServer(sc.handler())
	defer ts.Close()

	col := &collector{}
	srv := server{provider: "anthropic", model: "claude-sonnet-4", resumeID: "ses_test", compact: true}
	result, got := srv.converse(context.Background(), ts.URL, nil, col.emit)
	if !got {
		t.Fatalf("compact converse reported no result; text=%q", result.Text)
	}
	if result.IsError {
		t.Errorf("compact result unexpectedly an error: %+v", result)
	}
	if result.SessionID != "ses_test" {
		t.Errorf("session id = %q, want ses_test", result.SessionID)
	}

	// The summarize body carried the explicit provider/model the route demands (an empty model 500s).
	sc.mu.Lock()
	body := sc.body
	sc.mu.Unlock()
	var sb struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	}
	if json.Unmarshal(body, &sb) != nil || sb.ProviderID != "anthropic" || sb.ModelID != "claude-sonnet-4" {
		t.Errorf("summarize body = %s, want {providerID:anthropic, modelID:claude-sonnet-4}", body)
	}

	// The compaction boundary surfaced as EventCompaction{trigger:manual} plus a terminal result.
	evs := col.snapshot()
	c := firstOfType(evs, agent.EventCompaction)
	if c == nil || c.Compaction == nil || c.Compaction.Trigger != "manual" {
		t.Fatalf("missing/incorrect EventCompaction: %+v", c)
	}
	if firstOfType(evs, agent.EventResult) == nil {
		t.Error("no terminal result after compaction")
	}
}

// TestConverseCompactIdleBackstop proves that when opencode emits session.idle alongside
// session.compacted, the run still terminates exactly once (first-wins) with a single compaction event —
// the belt-and-suspenders terminal for a manual compaction.
func TestConverseCompactIdleBackstop(t *testing.T) {
	sc := newCompactServer(true)
	ts := httptest.NewServer(sc.handler())
	defer ts.Close()

	col := &collector{}
	srv := server{provider: "anthropic", model: "claude-sonnet-4", resumeID: "ses_test", compact: true}
	_, got := srv.converse(context.Background(), ts.URL, nil, col.emit)
	if !got {
		t.Fatal("compact converse (idle backstop) reported no result")
	}

	evs := col.snapshot()
	var results, compactions int
	for _, e := range evs {
		switch e.Type {
		case agent.EventResult:
			results++
		case agent.EventCompaction:
			compactions++
		}
	}
	if results != 1 {
		t.Errorf("EventResult count = %d, want exactly 1 (first-wins terminal)", results)
	}
	if compactions != 1 {
		t.Errorf("EventCompaction count = %d, want exactly 1", compactions)
	}
}

// TestCompactRequiresModel asserts Compact fails fast with a clear message when no provider/model is
// resolvable — opencode's summarize route rejects an empty model, so the adapter must not emit a
// malformed request. Likewise with no session id there is nothing to compact.
func TestCompactRequiresModel(t *testing.T) {
	col := &collector{}
	res, _ := adapter{}.Compact(context.Background(), agent.TurnInput{
		SessionID: "ses_test", // has a session, but no model configured
		Config:    map[string]string{},
	}, col.emit)
	if !res.IsError || !strings.Contains(res.Text, "model") {
		t.Errorf("Compact without a model = %+v, want an IsError result mentioning the model", res)
	}

	col2 := &collector{}
	res2, _ := adapter{}.Compact(context.Background(), agent.TurnInput{
		Config: map[string]string{keyModel: "anthropic/claude-sonnet-4"}, // model, but no session
	}, col2.emit)
	if !res2.IsError || !strings.Contains(res2.Text, "compact") {
		t.Errorf("Compact without a session = %+v, want an IsError result about nothing to compact", res2)
	}
}
