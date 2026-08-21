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
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// collector gathers emitted events under a lock (converse emits from several goroutines).
type collector struct {
	mu     sync.Mutex
	events []agent.Event
}

func (c *collector) emit(ev agent.Event) {
	c.mu.Lock()
	c.events = append(c.events, ev)
	c.mu.Unlock()
}

func (c *collector) snapshot() []agent.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]agent.Event, len(c.events))
	copy(out, c.events)
	return out
}

func firstOfType(evs []agent.Event, t agent.EventType) *agent.Event {
	for i := range evs {
		if evs[i].Type == t {
			return &evs[i]
		}
	}
	return nil
}

// scriptServer is a minimal but faithful opencode server over httptest: it serves the GET /event SSE
// bus and the session REST routes, driving one turn to completion. Its behavior is chosen by mode so a
// single struct backs both the approval and the interrupt round-trip.
type scriptServer struct {
	sessionID  string
	mode       string // "approval" | "interrupt"
	prompted   chan struct{}
	promptOnce sync.Once
	decisions  chan json.RawMessage // permissions POST body → serveEvents (and stashed for the test)
	aborts     chan struct{}        // abort POST → serveEvents

	mu         sync.Mutex
	decision   json.RawMessage
	promptBody json.RawMessage // the prompt_async request body, for part-shape assertions
	sawAbort   bool
}

func newScriptServer(mode string) *scriptServer {
	return &scriptServer{
		sessionID: "ses_test",
		mode:      mode,
		prompted:  make(chan struct{}),
		decisions: make(chan json.RawMessage, 4),
		aborts:    make(chan struct{}, 4),
	}
}

func (s *scriptServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/event":
			s.serveEvents(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": s.sessionID})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/prompt_async"):
			body, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.promptBody = body
			s.mu.Unlock()
			s.promptOnce.Do(func() { close(s.prompted) })
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/permissions/"):
			body, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.decision = body
			s.mu.Unlock()
			select {
			case s.decisions <- body:
			default:
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/abort"):
			s.mu.Lock()
			s.sawAbort = true
			s.mu.Unlock()
			select {
			case s.aborts <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (s *scriptServer) serveEvents(w http.ResponseWriter, r *http.Request) {
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
	case <-s.prompted:
	case <-r.Context().Done():
		return
	}

	textPart := func(text string) map[string]any {
		return frame("message.part.updated", map[string]any{
			"part": map[string]any{"id": "prt_1", "sessionID": s.sessionID, "messageID": "msg_1", "type": "text", "text": text},
		})
	}
	idle := frame("session.idle", map[string]any{"sessionID": s.sessionID})

	switch s.mode {
	case "error":
		// A provider failure: opencode reports it as one session.error frame and never goes idle.
		send(frame("session.error", map[string]any{
			"sessionID": s.sessionID,
			"error":     map[string]any{"name": "ProviderAuthError", "data": map[string]any{"message": "Google Generative AI API key is missing"}},
		}))

	case "foreign-error":
		// A session.error for a DIFFERENT session must not end our turn; ours then completes normally.
		send(frame("session.error", map[string]any{
			"sessionID": "ses_someone_else",
			"error":     map[string]any{"name": "SomeoneElsesProblem"},
		}))
		send(textPart("Done."))
		send(idle)

	case "interrupt":
		send(textPart("working"))
		select {
		case <-s.aborts:
		case <-r.Context().Done():
			return
		case <-time.After(3 * time.Second):
		}
		send(idle)

	default: // approval
		// Ask for a permission; opencode sends the Permission object AS properties.
		send(frame("permission.updated", map[string]any{
			"id": "perm-1", "sessionID": s.sessionID, "title": "Run ls -la", "type": "bash", "callID": "call-1",
		}))
		select {
		case <-s.decisions:
		case <-r.Context().Done():
			return
		case <-time.After(3 * time.Second):
		}
		send(textPart("Done."))
		send(frame("message.updated", map[string]any{
			"info": map[string]any{"id": "msg_1", "sessionID": s.sessionID, "role": "assistant",
				"cost": 0.002, "tokens": map[string]any{"input": 12, "output": 7, "reasoning": 0,
					"cache": map[string]any{"read": 3, "write": 1}}},
		}))
		send(idle)
	}
}

// TestConverseAutoApprove drives the autonomous path against a scripted server: opencode raises a
// permission ask and the driver (interactive=false) approves it with "always" WITHOUT surfacing an
// interaction, then the turn completes. Asserts the session/result events, the token+cost Meta, the
// streamed answer, that no interaction leaked, and the decision frame the server received.
func TestConverseAutoApprove(t *testing.T) {
	sc := newScriptServer("approval")
	ts := httptest.NewServer(sc.handler())
	defer ts.Close()

	col := &collector{}
	srv := server{message: "hi", interactive: false}
	result, got := srv.converse(context.Background(), ts.URL, nil, col.emit)

	if !got {
		t.Fatalf("converse reported no result; text=%q", result.Text)
	}
	if result.Text != "Done." || result.IsError {
		t.Errorf("result = %+v, want text 'Done.' not error", result)
	}
	if result.SessionID != "ses_test" {
		t.Errorf("session id = %q, want ses_test", result.SessionID)
	}

	// The autonomous driver must have approved with "always".
	sc.mu.Lock()
	dec := sc.decision
	sc.mu.Unlock()
	var d struct {
		Response string `json:"response"`
	}
	if json.Unmarshal(dec, &d) != nil || d.Response != "always" {
		t.Errorf("auto decision = %s, want {\"response\":\"always\"}", dec)
	}

	evs := col.snapshot()
	if s := firstOfType(evs, agent.EventSession); s == nil || s.SessionID != "ses_test" {
		t.Errorf("missing/incorrect EventSession: %+v", s)
	}
	if inter := firstOfType(evs, agent.EventInteraction); inter != nil {
		t.Errorf("autonomous turn must not surface an interaction, got %+v", inter)
	}
	var streamed string
	for _, e := range evs {
		if e.Type == agent.EventText {
			if !e.Delta {
				t.Errorf("streamed text must be Delta=true, got %+v", e)
			}
			streamed += e.Text
		}
	}
	if streamed != "Done." {
		t.Errorf("concatenated deltas = %q, want 'Done.'", streamed)
	}
	res := firstOfType(evs, agent.EventResult)
	if res == nil || res.Result == nil || res.Result.Text != "Done." {
		t.Fatalf("missing/incorrect EventResult: %+v", res)
	}
	if res.Meta == nil || res.Meta["inputTokens"] != 12 || res.Meta["outputTokens"] != 7 {
		t.Errorf("result meta = %+v, want token telemetry", res.Meta)
	}
}

// TestConverseApprovalFlow drives the interactive path: opencode raises a permission ask, the driver
// surfaces it as an approval interaction, we answer "allow" as a client would, and the decision reaches
// the server as "once". Asserts the interaction surfaced, the decision frame, and the terminal result —
// the user-in-loop round-trip the autonomous path skips.
func TestConverseApprovalFlow(t *testing.T) {
	sc := newScriptServer("approval")
	ts := httptest.NewServer(sc.handler())
	defer ts.Close()

	inbound := make(chan agent.Inbound, 4)
	col := &collector{}
	emit := func(ev agent.Event) {
		col.emit(ev)
		if ev.Type == agent.EventInteraction && ev.Interaction != nil && ev.Interaction.Kind == "approval" {
			inbound <- agent.Inbound{Kind: "response", InteractionID: ev.Interaction.ID, Decision: "allow"}
		}
	}

	srv := server{message: "hi", interactive: true}
	result, got := srv.converse(context.Background(), ts.URL, inbound, emit)

	if !got || result.Text != "Done." {
		t.Fatalf("converse result = %+v got=%v, want 'Done.'", result, got)
	}

	sc.mu.Lock()
	dec := sc.decision
	sc.mu.Unlock()
	var d struct {
		Response string `json:"response"`
	}
	if json.Unmarshal(dec, &d) != nil || d.Response != "once" {
		t.Errorf("interactive decision = %s, want {\"response\":\"once\"}", dec)
	}

	evs := col.snapshot()
	if inter := firstOfType(evs, agent.EventInteraction); inter == nil || inter.Interaction == nil || inter.Interaction.ID != "perm-1" {
		t.Errorf("missing approval interaction (id perm-1): %+v", inter)
	}
	if res := firstOfType(evs, agent.EventResult); res == nil || res.Result == nil || res.Result.Text != "Done." {
		t.Errorf("missing/incorrect EventResult: %+v", res)
	}
}

// TestConverseInterrupt drives the interrupt round-trip: the moment opencode streams output, the client
// sends an interrupt, the pump POSTs /abort, and the turn still reaches a terminal result. Asserts the
// server received the abort and a result event fired — the cancel path with no real binary.
func TestConverseInterrupt(t *testing.T) {
	sc := newScriptServer("interrupt")
	ts := httptest.NewServer(sc.handler())
	defer ts.Close()

	inbound := make(chan agent.Inbound, 4)
	col := &collector{}
	var once sync.Once
	emit := func(ev agent.Event) {
		col.emit(ev)
		if ev.Type == agent.EventText {
			once.Do(func() { inbound <- agent.Inbound{Kind: "interrupt"} })
		}
	}

	srv := server{message: "count to 100", interactive: false}
	_, got := srv.converse(context.Background(), ts.URL, inbound, emit)
	if !got {
		t.Fatal("converse reported no result after interrupt")
	}

	sc.mu.Lock()
	sawAbort := sc.sawAbort
	sc.mu.Unlock()
	if !sawAbort {
		t.Error("server never received an abort — the interrupt round-trip was not exercised")
	}
	if firstOfType(col.snapshot(), agent.EventResult) == nil {
		t.Error("no terminal result after interrupt")
	}
}

// TestSerializeEmitIsConcurrencySafe proves the transport's emit serialization: many goroutines call
// the wrapped emit concurrently, and the underlying (deliberately unguarded) sink mirrors the runner's
// emit closure. Under `go test -race` this fails if serializeEmit ever stops locking.
func TestSerializeEmitIsConcurrencySafe(t *testing.T) {
	var got []agent.Event // deliberately unguarded — mirrors runner.go's emit closure
	safe := serializeEmit(func(ev agent.Event) { got = append(got, ev) })

	const n = 500
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			safe(agent.Event{Type: agent.EventText, Text: "x"})
		}()
	}
	wg.Wait()

	if len(got) != n {
		t.Fatalf("serializeEmit lost writes under concurrency: got %d, want %d", len(got), n)
	}
}

// TestConverseSessionErrorEmitsOnce pins the Event contract for a failed turn: ONE session.error frame
// must produce exactly ONE carrying event. It used to produce two (an EventError *and* the terminal
// EventResult) with the same string, which every consumer that renders both — the console's turn view —
// drew twice. The message must still reach the caller through the returned TurnResult, which is what
// both SDKs read as run.error.
func TestConverseSessionErrorEmitsOnce(t *testing.T) {
	sc := newScriptServer("error")
	ts := httptest.NewServer(sc.handler())
	defer ts.Close()

	col := &collector{}
	srv := server{message: "hi", interactive: false}
	result, got := srv.converse(context.Background(), ts.URL, nil, col.emit)

	const want = "Google Generative AI API key is missing"
	if !got {
		t.Fatalf("converse reported no result; a session.error is a terminal result, text=%q", result.Text)
	}
	if !result.IsError || result.Text != want {
		t.Errorf("result = %+v, want IsError with text %q", result, want)
	}

	evs := col.snapshot()
	var carrying int
	for _, e := range evs {
		if e.Type == agent.EventError || (e.Type == agent.EventResult && e.Result != nil && e.Result.IsError) {
			carrying++
		}
	}
	if carrying != 1 {
		t.Errorf("one session.error produced %d error-carrying events, want exactly 1: %+v", carrying, evs)
	}
	if e := firstOfType(evs, agent.EventError); e != nil {
		t.Errorf("session.error must not emit a bare EventError beside the terminal result, got %+v", e)
	}
	res := firstOfType(evs, agent.EventResult)
	if res == nil || res.Result == nil || !res.Result.IsError || res.Result.Text != want {
		t.Fatalf("terminal EventResult must carry the message: %+v", res)
	}
}

// TestConverseForeignSessionErrorIgnored pins the session-scope filter every other arm of route applies:
// an error belonging to another session must not terminate this turn.
func TestConverseForeignSessionErrorIgnored(t *testing.T) {
	sc := newScriptServer("foreign-error")
	ts := httptest.NewServer(sc.handler())
	defer ts.Close()

	col := &collector{}
	srv := server{message: "hi", interactive: false}
	result, got := srv.converse(context.Background(), ts.URL, nil, col.emit)

	// Our session still completes normally; the foreign error is dropped, not surfaced.
	if !got || result.IsError || result.Text != "Done." {
		t.Fatalf("result = %+v (got=%v), want the normal completion", result, got)
	}
	if e := firstOfType(col.snapshot(), agent.EventError); e != nil {
		t.Errorf("unexpected error event on a clean turn: %+v", e)
	}
}
