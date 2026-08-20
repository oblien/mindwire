package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// collect runs emit into a slice guarded for the concurrent goroutines converse emits from.
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

// TestAppServerApprovalFlow drives the full protocol against a scripted server over in-memory pipes:
// handshake → turn → an approval server-request surfaces as an interaction → we answer it → the turn
// completes. It asserts both the events the driver emits AND the decision frame the server receives —
// the round-trip that the exec path structurally cannot do.
func TestAppServerApprovalFlow(t *testing.T) {
	clientR, clientW := io.Pipe() // driver writes clientW; server reads clientR
	serverR, serverW := io.Pipe() // server writes serverW; driver reads serverR

	inbound := make(chan agent.Inbound, 4)
	col := &collector{}
	emit := func(ev agent.Event) {
		col.emit(ev)
		// Answer the first approval interaction the moment it appears (as a real client would).
		if ev.Type == agent.EventInteraction && ev.Interaction != nil && ev.Interaction.Kind == "approval" {
			inbound <- agent.Inbound{Kind: "response", InteractionID: ev.Interaction.ID, Decision: "allow"}
		}
	}

	gotDecision := make(chan json.RawMessage, 1)
	go scriptedServer(clientR, serverW, gotDecision)

	a := appServer{message: "hi", sandbox: "workspace-write", approval: "on-request"}
	result, got := a.converse(context.Background(), clientW, serverR, inbound, emit)
	_ = clientW.Close()
	_ = serverW.Close()

	if !got {
		t.Fatalf("converse reported no result; text=%q", result.Text)
	}
	if result.Text != "Done." {
		t.Errorf("result text = %q, want %q", result.Text, "Done.")
	}
	if result.IsError {
		t.Errorf("result unexpectedly flagged as error")
	}
	if result.SessionID != "sess-1" {
		t.Errorf("session id = %q, want sess-1", result.SessionID)
	}

	// The server must have received an experimental-family accept decision.
	select {
	case raw := <-gotDecision:
		var d struct {
			Decision string `json:"decision"`
		}
		if json.Unmarshal(raw, &d) != nil || d.Decision != "accept" {
			t.Errorf("approval decision frame = %s, want {\"decision\":\"accept\"}", raw)
		}
	default:
		t.Errorf("server never received an approval decision")
	}

	evs := col.snapshot()
	if s := firstOfType(evs, agent.EventSession); s == nil || s.SessionID != "sess-1" {
		t.Errorf("missing/incorrect EventSession: %+v", s)
	}
	if inter := firstOfType(evs, agent.EventInteraction); inter == nil || inter.Interaction.Kind != "approval" {
		t.Errorf("missing approval interaction")
	}
	// The final message streamed as Delta suffixes; concatenated they equal the answer exactly once.
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
		t.Errorf("concatenated text deltas = %q, want 'Done.'", streamed)
	}
	// Live token telemetry surfaced as a status event.
	if st := firstOfType(evs, agent.EventStatus); st == nil || st.Meta == nil || st.Meta["totalTokens"] != 19 {
		t.Errorf("missing/incorrect EventStatus token telemetry: %+v", st)
	}
	if res := firstOfType(evs, agent.EventResult); res == nil || res.Result == nil || res.Result.Text != "Done." {
		t.Errorf("missing/incorrect EventResult: %+v", res)
	}
}

// scriptedServer is a minimal, faithful app-server: it echoes each request id and drives the turn to
// completion, requesting one command approval along the way. It reads the client's decision reply and
// forwards it on gotDecision so the test can assert the wire shape.
func scriptedServer(r io.Reader, w io.Writer, gotDecision chan<- json.RawMessage) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	read := func() rpcIn {
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var m rpcIn
			_ = json.Unmarshal([]byte(line), &m)
			return m
		}
		return rpcIn{}
	}
	writeLine := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = w.Write(append(b, '\n'))
	}

	init := read() // initialize
	writeLine(map[string]any{"id": init.ID, "result": map[string]any{"userAgent": "codex-test"}})
	read() // initialized notification (no reply)

	ts := read() // thread/start
	writeLine(map[string]any{"id": ts.ID, "result": map[string]any{
		"thread": map[string]any{"id": "th-1", "sessionId": "sess-1"}}})

	read() // turn/start
	writeLine(map[string]any{"method": "turn/started", "params": map[string]any{
		"threadId": "th-1", "turn": map[string]any{"id": "turn-1"}}})
	// Ask for command approval.
	writeLine(map[string]any{"id": "srv-1", "method": "item/commandExecution/requestApproval",
		"params": map[string]any{"itemId": "i1", "command": "ls -la", "threadId": "th-1", "turnId": "turn-1"}})
	reply := read() // the client's decision
	gotDecision <- reply.Result
	// Stream the final message incrementally (item/started → updated → completed), exercising the
	// W4a item/updated case and suffix-delta discipline end-to-end.
	writeLine(map[string]any{"method": "item/started", "params": map[string]any{
		"item": map[string]any{"id": "a1", "type": "agentMessage", "text": ""}}})
	writeLine(map[string]any{"method": "item/updated", "params": map[string]any{
		"item": map[string]any{"id": "a1", "type": "agentMessage", "text": "Do"}}})
	writeLine(map[string]any{"method": "item/completed", "params": map[string]any{
		"item": map[string]any{"id": "a1", "type": "agentMessage", "text": "Done."}}})
	// Live token telemetry → an EventStatus.
	writeLine(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{
		"tokenUsage": map[string]any{"total": map[string]any{
			"inputTokens": 12, "outputTokens": 7, "totalTokens": 19}}}})
	writeLine(map[string]any{"method": "turn/completed", "params": map[string]any{
		"threadId": "th-1",
		"turn": map[string]any{"id": "turn-1", "status": "completed",
			"items": []any{map[string]any{"type": "agentMessage", "text": "Done."}}}}})
}

// TestAppServerCompactFlow drives an on-demand compaction over in-memory pipes: handshake →
// thread/resume → thread/compact/start (whose {} response is a mere ACK, not terminal) → a streamed
// contextCompaction item terminates the compact turn. It asserts the terminal result, the manual-trigger
// EventCompaction (with its summary), and that the server received a compact/start carrying the resumed
// thread id — none of which the exec hot path can do.
func TestAppServerCompactFlow(t *testing.T) {
	clientR, clientW := io.Pipe() // driver writes clientW; server reads clientR
	serverR, serverW := io.Pipe() // server writes serverW; driver reads serverR

	col := &collector{}
	gotCompact := make(chan json.RawMessage, 1)
	go scriptedCompactServer(clientR, serverW, gotCompact)

	a := appServer{compact: true, resumeID: "th-1", sandbox: "read-only", approval: "never"}
	result, got := a.converse(context.Background(), clientW, serverR, nil, col.emit)
	_ = clientW.Close()
	_ = serverW.Close()

	if !got {
		t.Fatalf("converse reported no result; text=%q", result.Text)
	}
	if result.Text != "Conversation compacted." {
		t.Errorf("result text = %q, want 'Conversation compacted.'", result.Text)
	}
	if result.IsError {
		t.Errorf("compaction unexpectedly flagged as error")
	}
	if result.SessionID != "sess-1" {
		t.Errorf("session id = %q, want sess-1", result.SessionID)
	}

	// The server must have received a compact/start for the resumed thread.
	select {
	case raw := <-gotCompact:
		var p struct {
			ThreadID string `json:"threadId"`
		}
		if json.Unmarshal(raw, &p) != nil || p.ThreadID != "th-1" {
			t.Errorf("compact/start params = %s, want threadId th-1", raw)
		}
	default:
		t.Errorf("server never received thread/compact/start")
	}

	evs := col.snapshot()
	c := firstOfType(evs, agent.EventCompaction)
	if c == nil || c.Compaction == nil {
		t.Fatalf("missing EventCompaction: %+v", c)
	}
	if c.Compaction.Trigger != "manual" {
		t.Errorf("compaction trigger = %q, want manual", c.Compaction.Trigger)
	}
	if c.Compaction.Summary != "Summarized 3 turns." {
		t.Errorf("compaction summary = %q, want 'Summarized 3 turns.'", c.Compaction.Summary)
	}
	if res := firstOfType(evs, agent.EventResult); res == nil || res.Result == nil || res.Result.Text != "Conversation compacted." {
		t.Errorf("missing/incorrect EventResult: %+v", res)
	}
}

// scriptedCompactServer is a minimal app-server servicing a compaction: it resumes the thread, answers
// thread/compact/start with an empty {} ACK (accepted, not done), then streams a contextCompaction item
// as the terminal signal — a compact turn has no turn, so there is NO turn/completed. It forwards the
// compact/start params so the test can assert the thread id.
func scriptedCompactServer(r io.Reader, w io.Writer, gotCompact chan<- json.RawMessage) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	read := func() rpcIn {
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var m rpcIn
			_ = json.Unmarshal([]byte(line), &m)
			return m
		}
		return rpcIn{}
	}
	writeLine := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = w.Write(append(b, '\n'))
	}

	init := read() // initialize
	writeLine(map[string]any{"id": init.ID, "result": map[string]any{"userAgent": "codex-test"}})
	read() // initialized notification (no reply)

	tr := read() // thread/resume
	writeLine(map[string]any{"id": tr.ID, "result": map[string]any{
		"thread": map[string]any{"id": "th-1", "sessionId": "sess-1"}}})

	cs := read() // thread/compact/start
	gotCompact <- cs.Params
	writeLine(map[string]any{"id": cs.ID, "result": map[string]any{}}) // empty ACK: accepted, not done
	// The compaction boundary — the terminal signal for a compact turn (no turn/completed follows).
	writeLine(map[string]any{"method": "item/completed", "params": map[string]any{
		"item": map[string]any{"id": "c1", "type": "contextCompaction", "summary": "Summarized 3 turns."}}})
}

// TestSerializeEmitIsConcurrencySafe proves the app-server transport's emit serialization: the reader,
// the turn-await watcher, and the handshake all call emit concurrently, and the runner's real emit
// mutates unsynchronized transcript state exactly like this unguarded sink does. Under `go test -race`
// this fails (data race + lost writes) if serializeEmit ever stops locking — the regression guard for
// the concurrent-emit bug the CLI path never had.
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

// TestServerRequestInteractionFamilies asserts each server-request method maps to the right interaction
// kind AND the right decision family — the split that a wrong reply-shape would silently corrupt.
func TestServerRequestInteractionFamilies(t *testing.T) {
	cases := []struct {
		method     string
		wantKind   string
		wantFamily string
	}{
		{"execCommandApproval", "approval", famReviewDecision},
		{"applyPatchApproval", "approval", famReviewDecision},
		{"item/commandExecution/requestApproval", "approval", famV2Decision},
		{"item/fileChange/requestApproval", "approval", famV2Decision},
		{"item/tool/requestUserInput", "choice", famAnswers},
		{"mcpServer/elicitation/request", "approval", famV2Decision}, // unknown → safe yes/no fallback
	}
	for _, tc := range cases {
		msg := rpcIn{ID: json.RawMessage(`"req-9"`), Method: tc.method, Params: json.RawMessage(`{}`)}
		inter, ap := serverRequestInteraction(msg)
		if inter == nil {
			t.Errorf("%s: nil interaction", tc.method)
			continue
		}
		if inter.Kind != tc.wantKind {
			t.Errorf("%s: kind = %q, want %q", tc.method, inter.Kind, tc.wantKind)
		}
		if !inter.NeedsResponse {
			t.Errorf("%s: NeedsResponse should be true", tc.method)
		}
		if inter.ID != `"req-9"` {
			t.Errorf("%s: interaction id = %q, want the raw request id", tc.method, inter.ID)
		}
		if ap.family != tc.wantFamily {
			t.Errorf("%s: family = %q, want %q", tc.method, ap.family, tc.wantFamily)
		}
		if string(ap.rawID) != `"req-9"` {
			t.Errorf("%s: rawID = %s, want the echoed request id", tc.method, ap.rawID)
		}
	}
}

// TestDecisionResult asserts each family encodes allow/deny into its native decision shape.
func TestDecisionResult(t *testing.T) {
	mustJSON := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}

	// Legacy review-decision: allow → "approved"; deny → nested {denied:{rejection}}.
	if got := mustJSON(decisionResult(approval{family: famReviewDecision}, agent.Inbound{Decision: "allow"})); got != `{"decision":"approved"}` {
		t.Errorf("reviewDecision allow = %s", got)
	}
	denied := mustJSON(decisionResult(approval{family: famReviewDecision}, agent.Inbound{Decision: "deny", Text: "no thanks"}))
	if !strings.Contains(denied, `"denied"`) || !strings.Contains(denied, "no thanks") {
		t.Errorf("reviewDecision deny = %s, want a denied rejection carrying the reason", denied)
	}

	// Experimental v2: allow → "accept"; deny → "decline".
	if got := mustJSON(decisionResult(approval{family: famV2Decision}, agent.Inbound{Decision: "allow"})); got != `{"decision":"accept"}` {
		t.Errorf("v2 allow = %s", got)
	}
	if got := mustJSON(decisionResult(approval{family: famV2Decision}, agent.Inbound{Decision: "deny"})); got != `{"decision":"decline"}` {
		t.Errorf("v2 deny = %s", got)
	}

	// Answers: the free-text answer is keyed by the question id.
	ans := mustJSON(decisionResult(approval{family: famAnswers, questionID: "q1"}, agent.Inbound{Text: "blue"}))
	if !strings.Contains(ans, `"answers"`) || !strings.Contains(ans, `"q1":"blue"`) {
		t.Errorf("answers = %s, want the answer keyed by q1", ans)
	}
}

// TestTurnOutcome pulls the final text and error flag from a completed turn envelope.
func TestTurnOutcome(t *testing.T) {
	ok := `{"turn":{"id":"t","status":"completed","items":[
		{"type":"agentMessage","text":"first"},
		{"type":"agentMessage","text":"final answer"}]}}`
	if text, isErr := turnOutcome(json.RawMessage(ok)); text != "final answer" || isErr {
		t.Errorf("completed = (%q,%v), want (final answer,false)", text, isErr)
	}

	fail := `{"turn":{"id":"t","status":"failed","error":{"message":"boom"},"items":[]}}`
	if text, isErr := turnOutcome(json.RawMessage(fail)); text != "boom" || !isErr {
		t.Errorf("failed = (%q,%v), want (boom,true)", text, isErr)
	}
}

// TestEmitItem maps app-server items to unified events. Text/thinking are emitted as Delta=true events
// (W4a streaming discipline, matching the exec parser and claude); a whole-block item with no prior
// updates emits its full text once.
func TestEmitItem(t *testing.T) {
	var evs []agent.Event
	emit := func(ev agent.Event) { evs = append(evs, ev) }
	st := newStreamState()

	// Agent message (completed, no prior frames) → one Delta=true EventText, returning the running text.
	if got := emitItem(phaseCompleted, json.RawMessage(`{"type":"agentMessage","id":"m0","text":"hello"}`), emit, st); got != "hello" {
		t.Errorf("agentMessage return = %q, want hello", got)
	}
	if len(evs) != 1 || evs[0].Type != agent.EventText || evs[0].Text != "hello" || !evs[0].Delta {
		t.Errorf("agentMessage event = %+v, want single Delta EventText 'hello'", evs)
	}

	// Reasoning → EventThinking (Delta=true).
	evs = nil
	emitItem(phaseCompleted, json.RawMessage(`{"type":"reasoning","id":"r0","content":["step one"]}`), emit, st)
	if len(evs) != 1 || evs[0].Type != agent.EventThinking || evs[0].Text != "step one" || !evs[0].Delta {
		t.Errorf("reasoning event = %+v", evs)
	}

	// Command execution: started → tool_use once; completed → tool_result with the output + exit-code error.
	evs = nil
	raw := `{"type":"commandExecution","id":"c1","command":"ls","aggregatedOutput":"boom","exitCode":2,"status":"failed"}`
	emitItem(phaseStarted, json.RawMessage(raw), emit, st)
	emitItem(phaseCompleted, json.RawMessage(raw), emit, st)
	var use, resu *agent.Event
	for i := range evs {
		switch evs[i].Type {
		case agent.EventToolUse:
			use = &evs[i]
		case agent.EventToolResult:
			resu = &evs[i]
		}
	}
	if use == nil || use.Tool == nil || use.Tool.Name != "shell" {
		t.Errorf("tool_use = %+v, want shell", use)
	}
	if resu == nil || resu.Tool == nil || resu.Tool.Output != "boom" || !resu.Tool.IsError {
		t.Errorf("tool_result = %+v, want output boom + IsError", resu)
	}
}

// TestEmitItemStreaming verifies the W4a suffix-delta discipline: a cumulative agentMessage growing
// across updated → completed streams only the new bytes each frame (never re-emitting), and the sum of
// the deltas equals the final text (so the runner's accumulation matches the final answer exactly once).
func TestEmitItemStreaming(t *testing.T) {
	var evs []agent.Event
	emit := func(ev agent.Event) { evs = append(evs, ev) }
	st := newStreamState()

	emitItem(phaseStarted, json.RawMessage(`{"type":"agentMessage","id":"m1","text":""}`), emit, st)
	emitItem(phaseUpdated, json.RawMessage(`{"type":"agentMessage","id":"m1","text":"Hel"}`), emit, st)
	emitItem(phaseUpdated, json.RawMessage(`{"type":"agentMessage","id":"m1","text":"Hello"}`), emit, st)
	got := emitItem(phaseCompleted, json.RawMessage(`{"type":"agentMessage","id":"m1","text":"Hello world"}`), emit, st)

	if got != "Hello world" {
		t.Errorf("completed return = %q, want 'Hello world'", got)
	}
	var joined string
	for _, e := range evs {
		if e.Type != agent.EventText {
			t.Fatalf("unexpected event %+v", e)
		}
		if !e.Delta {
			t.Errorf("streamed text event must be Delta=true, got %+v", e)
		}
		joined += e.Text
	}
	if joined != "Hello world" {
		t.Errorf("concatenated deltas = %q, want 'Hello world' (no double-count, no loss)", joined)
	}
	if len(evs) != 3 { // "Hel", "lo", " world" — the empty started frame emits nothing
		t.Errorf("expected 3 delta events, got %d: %+v", len(evs), evs)
	}
}
