package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
)

// server.go is opencode's transport — the analogue of codex/appserver.go, adapted from
// JSON-RPC-over-stdio to a native HTTP + SSE server. One turn = spawn `opencode serve` on a free
// loopback port, wait for health, subscribe to the GET /event bus, create (or resume) a session, POST
// the prompt, normalize the streamed parts, and tear the process down. Interactive tool approvals and
// interrupt ride the same event bus and the session's REST routes. converse() is split out of Run() so
// server_test.go can drive it against an httptest.Server with no real binary.

const (
	healthDeadline  = 15 * time.Second // server cold-start budget for /global/health
	connectDeadline = 10 * time.Second // budget for the first SSE frame (server.connected)
	healthPoll      = 100 * time.Millisecond
)

// server holds the resolved parameters for one turn. It is a value receiver — created fresh per turn by
// RunStream, never shared.
type server struct {
	env         map[string]string // auth env (AuthModule.EnvForRun) overlaid on os.Environ
	message     string            // the user text part (may carry an appended "Attached files" block)
	extraParts  []map[string]any  // extra prompt parts beyond the text (e.g. image `file` parts)
	provider    string            // "" ⇒ omit model from the prompt (opencode uses its configured default)
	model       string
	agentName   string
	system      string
	cwd         string
	resumeID    string
	interactive bool // true ⇒ pump permission asks to the user; false ⇒ auto-approve
	compact     bool // true ⇒ this run is an on-demand compaction (summarize) rather than a prompt
}

// Run spawns the per-turn opencode server, drives one turn over it, and tears it down. On any failure
// before a terminal result it surfaces the captured stderr as an error event, mirroring codex's Run.
func (s server) Run(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	port, err := freePort()
	if err != nil {
		emit(agent.Event{Type: agent.EventError, Error: err.Error()})
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}

	// runCtx bounds the child + all HTTP/SSE to this turn; cancel on return tree-kills via proc.Group.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmdline := fmt.Sprintf("opencode serve --hostname 127.0.0.1 --port %d", port)
	cmd := exec.CommandContext(runCtx, "bash", "-lc", cmdline)
	proc.Group(cmd) // whole-tree SIGKILL on cancel
	if s.cwd != "" {
		cmd.Dir = s.cwd
	}
	cmd.Env = os.Environ()
	for k, v := range s.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		emit(agent.Event{Type: agent.EventError, Error: err.Error()})
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	// Expose this turn's process group to resource monitoring (no-op if none is wired on ctx).
	proc.Report(ctx, cmd.Process.Pid)

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	var (
		result agent.TurnResult
		got    bool
	)
	if herr := waitHealthy(runCtx, base); herr != nil {
		result = agent.TurnResult{Text: "opencode serve did not become healthy: " + herr.Error(), IsError: true}
	} else {
		result, got = s.converse(runCtx, base, in.Inbound, emit)
	}

	cancel()
	_ = cmd.Wait()

	if !got {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(result.Text)
		}
		if msg == "" {
			msg = "opencode produced no result"
		}
		emit(agent.Event{Type: agent.EventError, Error: msg})
		return agent.TurnResult{Text: msg, IsError: true}, nil
	}
	return result, nil
}

// converse runs the full protocol over a live opencode server at base. Split out of Run so tests drive
// it against an httptest.Server. Returns the terminal result and whether one was seen (got=false ⇒ Run
// surfaces stderr as the error).
func (s server) converse(ctx context.Context, base string, inbound <-chan agent.Inbound, rawEmit agent.Emit) (agent.TurnResult, bool) {
	// The SSE reader, the inbound pump, and this goroutine's EventSession all emit — serialize them, as
	// the runner's emit assumes serial calls (same reason as codex's app-server path).
	emit := serializeEmit(rawEmit)
	client := &http.Client{} // no timeout: the /event stream is long-lived; ctx bounds every request

	var mu sync.Mutex // guards sessionID, lastMeta, lastUsage, result/got, and the answer accumulator
	sessionID := s.resumeID
	var lastMeta map[string]any
	var lastUsage *agent.Usage // typed mirror of lastMeta, attached additively to the terminal result

	st := newStreamState()
	answer := map[string]string{} // text part id → cumulative text
	var order []string            // first-seen order of text part ids

	done := make(chan struct{})
	var doneOnce sync.Once
	closeDone := func() { doneOnce.Do(func() { close(done) }) }

	var result agent.TurnResult
	var got bool
	// emitTerminal fires the single EventResult (first wins) and closes done.
	emitTerminal := func(res agent.TurnResult, meta map[string]any) {
		mu.Lock()
		first := !got
		if first {
			got, result = true, res
		}
		usage := lastUsage
		mu.Unlock()
		if first {
			ev := agent.Event{Type: agent.EventResult, SessionID: res.SessionID,
				Result: &agent.ResultInfo{Text: res.Text, IsError: res.IsError, SessionID: res.SessionID}}
			if len(meta) > 0 {
				ev.Meta = meta
			}
			if usage != nil {
				ev.Result.Usage = usage // additive typed tokens alongside the Meta usage
			}
			emit(ev)
		}
		closeDone()
	}
	finalText := func() string {
		mu.Lock()
		defer mu.Unlock()
		var b strings.Builder
		for _, id := range order {
			b.WriteString(answer[id])
		}
		return b.String()
	}

	// 1. Subscribe to the event bus BEFORE prompting so no message.part.* is missed.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/event", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return agent.TurnResult{Text: "subscribe /event: " + err.Error(), IsError: true}, false
	}

	connected := make(chan struct{})
	var connectOnce sync.Once
	go func() {
		defer resp.Body.Close()
		_ = parseSSE(resp.Body, func(ev sseEvent) bool {
			select {
			case <-done:
				return false // terminal already reached — stop reading
			default:
			}
			s.route(ev, &mu, &sessionID, &lastMeta, &lastUsage, st, answer, &order,
				connected, &connectOnce, emit, emitTerminal, finalText, client, base)
			return true
		})
		closeDone() // stream ended (server gone / ctx cancelled) — unblock any waiter
	}()

	// Wait for server.connected before creating the session.
	select {
	case <-connected:
	case <-done:
		mu.Lock()
		r, ok := result, got
		mu.Unlock()
		if ok {
			return r, true
		}
		return agent.TurnResult{Text: "opencode event stream closed before ready", IsError: true}, false
	case <-ctx.Done():
		return agent.TurnResult{Text: ctx.Err().Error(), IsError: true}, false
	case <-time.After(connectDeadline):
		return agent.TurnResult{Text: "timed out waiting for the opencode event stream", IsError: true}, false
	}

	// 2. Create or resume the session, then announce it.
	sid, err := s.ensureSession(ctx, client, base)
	if err != nil {
		closeDone()
		return agent.TurnResult{Text: "session: " + err.Error(), IsError: true}, false
	}
	mu.Lock()
	sessionID = sid
	mu.Unlock()
	emit(agent.Event{Type: agent.EventSession, SessionID: sid})

	// 3. Drive the turn: a normal prompt, or (compact mode) an on-demand summarize. Both are
	// fire-and-forget — the turn plays out over the event bus (the terminal session.idle, and for a
	// compaction the session.compacted boundary, arrive as SSE frames).
	send, verb := s.prompt, "prompt"
	if s.compact {
		send, verb = s.summarize, "summarize"
	}
	if err := send(ctx, client, base, sid); err != nil {
		closeDone()
		return agent.TurnResult{Text: verb + ": " + err.Error(), IsError: true}, false
	}

	// 4. Inbound pump: answer approvals and honor interrupt for the life of the turn.
	go s.pump(ctx, client, base, sid, inbound, done)

	// 5. Wait for the terminal event (session.idle/error) or cancellation.
	select {
	case <-done:
	case <-ctx.Done():
		// Turn cancelled — best-effort abort on a detached context (ctx is already done).
		s.abort(context.Background(), client, base, sid)
	}

	mu.Lock()
	r, ok := result, got
	mu.Unlock()
	if !ok {
		return agent.TurnResult{Text: "opencode ended without a result", SessionID: sid}, false
	}
	return r, true
}

// route dispatches one decoded SSE frame. It runs on the single reader goroutine; shared state is
// guarded by mu. Kept as a method (not a closure) only for readability — all state is passed in.
func (s server) route(
	ev sseEvent,
	mu *sync.Mutex, sessionID *string, lastMeta *map[string]any, lastUsage **agent.Usage,
	st *streamState, answer map[string]string, order *[]string,
	connected chan struct{}, connectOnce *sync.Once,
	emit agent.Emit, emitTerminal func(agent.TurnResult, map[string]any), finalText func() string,
	client *http.Client, base string,
) {
	ours := func() string { mu.Lock(); defer mu.Unlock(); return *sessionID }
	// mine reports whether an event's session id is (or is plausibly) this turn's. An empty id on
	// either side is treated as a match — we run exactly one session per server.
	mine := func(sid string) bool {
		o := ours()
		return o == "" || sid == "" || o == sid
	}

	switch ev.Type {
	case "server.connected":
		connectOnce.Do(func() { close(connected) })

	case "message.part.updated":
		var p struct {
			Part ocPart `json:"part"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || !mine(p.Part.SessionID) {
			return
		}
		id, full := st.emitPart(p.Part, emit)
		if id != "" {
			mu.Lock()
			if _, seen := answer[id]; !seen {
				*order = append(*order, id)
			}
			answer[id] = full
			mu.Unlock()
		}

	case "message.updated":
		var p struct {
			Info ocMessage `json:"info"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || p.Info.Role != "assistant" || !mine(p.Info.SessionID) {
			return
		}
		if m := usageMeta(p.Info); m != nil {
			u := usageStruct(p.Info) // typed mirror for the terminal result's Usage
			mu.Lock()
			*lastMeta = m
			*lastUsage = u
			mu.Unlock()
		}

	case "permission.updated":
		var perm ocPermission
		if json.Unmarshal(ev.Properties, &perm) != nil || !mine(perm.SessionID) {
			return
		}
		if s.interactive {
			emit(agent.Event{Type: agent.EventInteraction, Interaction: permissionInteraction(perm)})
		} else {
			// Autonomous default: approve for the rest of the turn without pausing.
			go s.decide(context.Background(), client, base, perm.SessionID, perm.ID, "always")
		}

	case "session.compacted":
		// The conversation was summarized. Emit the agnostic boundary; a manual (compact-mode) run also
		// treats this as its terminal — a session.idle backstop still terminates if the boundary frame
		// never arrives. In a NORMAL turn this is opencode auto-compacting mid-turn: surface it and let
		// the turn continue.
		var p struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || !mine(p.SessionID) {
			return
		}
		trigger := "auto"
		if s.compact {
			trigger = "manual"
		}
		emit(agent.Event{Type: agent.EventCompaction, SessionID: ours(),
			Compaction: &agent.CompactionInfo{Trigger: trigger}})
		if s.compact {
			mu.Lock()
			meta := *lastMeta
			mu.Unlock()
			emitTerminal(agent.TurnResult{Text: finalText(), SessionID: ours()}, meta)
		}

	case "session.error":
		// ONE event, like every other terminal path here: the error rides the EventResult (IsError) and
		// the message rides the returned TurnResult. Emitting a separate EventError alongside it would
		// put the same string on the wire twice, and every consumer that renders both — the SDKs' event
		// stream, the console's turn view — would draw it twice. Both SDKs already read the error event
		// only as a fallback for an empty run.error, which emitTerminal populates.
		var p struct {
			SessionID string `json:"sessionID"`
		}
		// sessionID is optional in this frame, and `mine` treats an empty id on either side as a match;
		// filtering still matters because a foreign session's error must not end our turn.
		if json.Unmarshal(ev.Properties, &p) == nil && !mine(p.SessionID) {
			return
		}
		if msg := sessionErrorText(ev.Properties); msg != "" {
			emitTerminal(agent.TurnResult{Text: msg, SessionID: ours(), IsError: true}, nil)
		}

	case "session.idle":
		var p struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || !mine(p.SessionID) {
			return
		}
		mu.Lock()
		meta := *lastMeta
		mu.Unlock()
		emitTerminal(agent.TurnResult{Text: finalText(), SessionID: ours()}, meta)
	}
}

// pump forwards inbound control messages to opencode for the life of the turn: a response answers a
// permission ask (allow→once, deny→reject), an interrupt aborts the session. It exits when the turn
// ends (done), the context is cancelled, or the inbound channel closes.
func (s server) pump(ctx context.Context, client *http.Client, base, sid string, inbound <-chan agent.Inbound, done <-chan struct{}) {
	if inbound == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case msg, ok := <-inbound:
			if !ok {
				return
			}
			switch msg.Kind {
			case "response":
				pid := agent.FirstNonEmpty(msg.InteractionID, msg.Meta["permissionID"])
				if pid == "" {
					continue
				}
				decision := "once"
				if agent.Denied(msg.Decision) {
					decision = "reject"
				}
				s.decide(ctx, client, base, sid, pid, decision)
			case "interrupt":
				s.abort(ctx, client, base, sid)
			}
		}
	}
}

// ── REST helpers ──

// ensureSession reuses the resume id when present, else creates a session bound to the turn's cwd.
func (s server) ensureSession(ctx context.Context, client *http.Client, base string) (string, error) {
	if s.resumeID != "" {
		return s.resumeID, nil
	}
	u := base + "/session"
	if s.cwd != "" {
		u += "?directory=" + url.QueryEscape(s.cwd)
	}
	body, _ := json.Marshal(map[string]any{"title": "mindwire"})
	var out struct {
		ID string `json:"id"`
	}
	if err := s.doJSON(ctx, client, http.MethodPost, u, body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("session response had no id")
	}
	return out.ID, nil
}

// prompt sends the user turn. The model is included only when both halves resolved; agent/system are
// included only when set. It is fire-and-forget (204) — the turn plays out over the event bus.
func (s server) prompt(ctx context.Context, client *http.Client, base, sid string) error {
	parts := []any{map[string]any{"type": "text", "text": s.message}}
	for _, p := range s.extraParts { // image `file` parts, etc.
		parts = append(parts, p)
	}
	body := map[string]any{"parts": parts}
	if s.provider != "" && s.model != "" {
		body["model"] = map[string]any{"providerID": s.provider, "modelID": s.model}
	}
	if s.agentName != "" {
		body["agent"] = s.agentName
	}
	if s.system != "" {
		body["system"] = s.system
	}
	b, _ := json.Marshal(body)
	return s.doJSON(ctx, client, http.MethodPost, base+"/session/"+url.PathEscape(sid)+"/prompt_async", b, nil)
}

// summarize triggers opencode's on-demand compaction for the session (POST /session/{id}/summarize).
// The route requires an explicit {providerID,modelID}; Compact guarantees both are resolved before we
// reach here. Fire-and-forget — the boundary arrives as the session.compacted event on the bus.
func (s server) summarize(ctx context.Context, client *http.Client, base, sid string) error {
	body, _ := json.Marshal(map[string]any{"providerID": s.provider, "modelID": s.model})
	return s.doJSON(ctx, client, http.MethodPost, base+"/session/"+url.PathEscape(sid)+"/summarize", body, nil)
}

// decide POSTs a permission decision. response is opencode's vocabulary: once (allow this call),
// always (allow for the rest of the turn — the autonomous default), reject (deny). Best-effort.
func (s server) decide(ctx context.Context, client *http.Client, base, sid, permID, response string) {
	if sid == "" || permID == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{"response": response})
	u := base + "/session/" + url.PathEscape(sid) + "/permissions/" + url.PathEscape(permID)
	_ = s.doJSON(ctx, client, http.MethodPost, u, body, nil)
}

// abort asks opencode to stop the running turn. Best-effort.
func (s server) abort(ctx context.Context, client *http.Client, base, sid string) {
	if sid == "" {
		return
	}
	_ = s.doJSON(ctx, client, http.MethodPost, base+"/session/"+url.PathEscape(sid)+"/abort", nil, nil)
}

// doJSON performs a JSON request and, when out != nil, decodes a 2xx body into it. A non-2xx status is
// an error. Bodies are always drained+closed so connections are reused.
func (s server) doJSON(ctx context.Context, client *http.Client, method, u string, body []byte, out any) error {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// freePort reserves an ephemeral loopback port and releases it, returning the number to hand opencode.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitHealthy polls GET /global/health until 200 or the deadline / ctx cancellation.
func waitHealthy(ctx context.Context, base string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.NewTimer(healthDeadline)
	defer deadline.Stop()
	tick := time.NewTicker(healthPoll)
	defer tick.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/global/health", nil)
		if resp, err := client.Do(req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timeout")
		case <-tick.C:
		}
	}
}

// serializeEmit wraps an Emit so concurrent goroutines (the SSE reader, the pump, the handshake) can
// emit safely; the runner's emit accumulates unsynchronized transcript state and assumes serial calls.
func serializeEmit(emit agent.Emit) agent.Emit {
	var mu sync.Mutex
	return func(ev agent.Event) {
		mu.Lock()
		defer mu.Unlock()
		emit(ev)
	}
}
