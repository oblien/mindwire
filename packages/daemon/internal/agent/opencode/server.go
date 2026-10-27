package opencode

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/proc"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
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
	env                 map[string]string // auth env (AuthModule.EnvForRun) overlaid on os.Environ
	message             string            // the user text part (may carry an appended "Attached files" block)
	extraParts          []map[string]any  // extra prompt parts beyond the text (e.g. image `file` parts)
	provider            string            // "" ⇒ omit model from the prompt (opencode uses its configured default)
	model               string
	agentName           string
	system              string
	contextInstructions string
	cwd                 string
	resumeID            string
	interactive         bool   // true ⇒ pump permission asks to the user; false ⇒ auto-approve
	compact             bool   // true ⇒ this run is an on-demand compaction (summarize) rather than a prompt
	apiV2               bool   // detected from the running server, not the installed CLI's version string
	password            string // fresh loopback server credential for this turn
}

// Run spawns the per-turn opencode server, drives one turn over it, and tears it down. On any failure
// before a terminal result it surfaces the captured stderr as an error event, mirroring codex's Run.
func (s server) Run(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return agent.TurnResult{}, err
	}
	s.password = fmt.Sprintf("%x", secret)
	port, err := freePort()
	if err != nil {
		emit(agent.Event{Type: agent.EventError, Error: err.Error()})
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}

	// runCtx bounds the child + all HTTP/SSE to this turn; cancel on return tree-kills via proc.Group.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmdline := fmt.Sprintf("opencode serve --hostname 127.0.0.1 --port %d", port)
	cmd := exec.CommandContext(runCtx, "bash", "-lc", toolchain.Shell(cmdline))
	proc.Group(cmd) // whole-tree SIGKILL on cancel
	if s.cwd != "" {
		cmd.Dir = s.cwd
	}
	cmd.Env = toolchain.Environment()
	for k, v := range s.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// V1 and V2 use different environment names. Both accept Basic auth as opencode.
	cmd.Env = append(cmd.Env, "OPENCODE_PASSWORD="+s.password,
		"OPENCODE_SERVER_PASSWORD="+s.password, "OPENCODE_SERVER_USERNAME=opencode")
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
	if v2, herr := s.waitHealthy(runCtx, base); herr != nil {
		result = agent.TurnResult{Text: "opencode serve did not become healthy: " + herr.Error(), IsError: true}
	} else {
		s.apiV2 = v2
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
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
	var interrupted bool // guarded by mu; set before the abort can emit session.idle
	// emitTerminal fires the single EventResult (first wins) and closes done.
	emitTerminal := func(res agent.TurnResult, meta map[string]any) {
		mu.Lock()
		if interrupted && !res.IsError {
			res.Cancelled = true
		}
		first := !got
		if first {
			got, result = true, res
		}
		usage := lastUsage
		mu.Unlock()
		if first {
			ev := agent.Event{Type: agent.EventResult, SessionID: res.SessionID,
				Result: &agent.ResultInfo{Text: res.Text, IsError: res.IsError, Cancelled: res.Cancelled, SessionID: res.SessionID}}
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
	eventPath := "/event"
	if s.apiV2 {
		eventPath = "/api/event"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+eventPath, nil)
	s.authorize(req)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return agent.TurnResult{Text: "subscribe /event: " + err.Error(), IsError: true}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return agent.TurnResult{Text: fmt.Sprintf("opencode %s returned HTTP %d (%s), expected an event stream", eventPath, resp.StatusCode, resp.Header.Get("Content-Type")), IsError: true}, false
	}

	connected := make(chan struct{})
	var connectOnce sync.Once
	v2stream := newV2Stream()
	var streamErr error // guarded by mu
	go func() {
		defer resp.Body.Close()
		err := parseSSE(resp.Body, func(ev sseEvent) bool {
			select {
			case <-done:
				return false // terminal already reached — stop reading
			default:
			}
			frames := []sseEvent{ev}
			if s.apiV2 {
				mu.Lock()
				sid := sessionID
				mu.Unlock()
				frames = v2stream.translate(ev, sid, emit)
			}
			for _, frame := range frames {
				s.route(ctx, frame, &mu, &sessionID, &lastMeta, &lastUsage, st, answer, &order,
					connected, &connectOnce, emit, emitTerminal, finalText, client, base)
			}
			return true
		})
		mu.Lock()
		streamErr = err
		mu.Unlock()
		closeDone() // stream ended (server gone / ctx cancelled) — unblock any waiter
	}()

	// Wait for server.connected before creating the session.
	select {
	case <-connected:
	case <-done:
		mu.Lock()
		r, ok, readErr := result, got, streamErr
		mu.Unlock()
		if ok {
			return r, true
		}
		msg := "opencode event stream closed before ready"
		if readErr != nil {
			msg += ": " + readErr.Error()
		}
		return agent.TurnResult{Text: msg, IsError: true}, false
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
	caps := (adapter{}).Capabilities()
	caps.Negotiated = true
	if s.apiV2 {
		caps.SystemPrompt = false
	}
	emit(agent.Event{Type: agent.EventSession, SessionID: sid, Capabilities: &caps})

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
	go s.pump(ctx, client, base, sid, inbound, done, func(err error) {
		emitTerminal(agent.TurnResult{SessionID: sid, IsError: true, Text: err.Error()}, nil)
	}, func() {
		mu.Lock()
		interrupted = true
		mu.Unlock()
	})

	// 5. Wait for the terminal event (session.idle/error) or cancellation.
	select {
	case <-done:
	case <-ctx.Done():
		emitTerminal(agent.TurnResult{SessionID: sid, Text: finalText(), Cancelled: true}, nil)
		// Turn cancelled — best-effort abort on a detached context (ctx is already done).
		_ = s.abort(context.Background(), client, base, sid)
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
	ctx context.Context, ev sseEvent,
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
		if json.Unmarshal(ev.Properties, &p) != nil || !mine(p.Part.SessionID) || p.Part.Synthetic {
			return
		}
		if role := st.roles[p.Part.MessageID]; role != "" && role != "assistant" {
			return
		}
		st.parts[p.Part.ID] = p.Part
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
		if json.Unmarshal(ev.Properties, &p) != nil || !mine(p.Info.SessionID) {
			return
		}
		st.roles[p.Info.ID] = p.Info.Role
		if p.Info.Role != "assistant" {
			return
		}
		if m := usageMeta(p.Info); m != nil {
			u := usageStruct(p.Info) // typed mirror for the terminal result's Usage
			mu.Lock()
			*lastMeta = m
			*lastUsage = u
			mu.Unlock()
		}

	case "message.part.delta":
		var delta struct{ SessionID, MessageID, PartID, Field, Delta string }
		if json.Unmarshal(ev.Properties, &delta) != nil || !mine(delta.SessionID) || delta.Field != "text" {
			return
		}
		part, ok := st.parts[delta.PartID]
		if !ok || part.Synthetic || st.roles[delta.MessageID] == "user" {
			return
		}
		part.Text += delta.Delta
		st.parts[part.ID] = part
		id, full := st.emitPart(part, emit)
		if id != "" {
			mu.Lock()
			if _, seen := answer[id]; !seen {
				*order = append(*order, id)
			}
			answer[id] = full
			mu.Unlock()
		}

	case "question.asked":
		var question ocQuestion
		if json.Unmarshal(ev.Properties, &question) == nil && mine(question.SessionID) {
			emit(agent.Event{Type: agent.EventInteraction, SessionID: ours(), Interaction: question.interaction()})
		}

	case "permission.updated", "permission.asked":
		var perm ocPermission
		if json.Unmarshal(ev.Properties, &perm) != nil || !mine(perm.SessionID) {
			return
		}
		if s.interactive {
			it := permissionInteraction(perm)
			if ev.Type == "permission.asked" {
				it.Meta["opencodePermissionAPI"] = "request"
			}
			if perm.Permission != "" {
				it.Title = perm.Permission + ": " + strings.Join(perm.Patterns, ", ")
			}
			emit(agent.Event{Type: agent.EventInteraction, Interaction: it})
		} else {
			// Never persist a project-wide permission grant on behalf of one run.
			go func() {
				if err := s.decide(ctx, client, base, perm.SessionID, perm.ID, "once", ev.Type == "permission.asked"); err != nil {
					emitTerminal(agent.TurnResult{SessionID: ours(), IsError: true, Text: err.Error()}, nil)
				}
			}()
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
func (s server) pump(ctx context.Context, client *http.Client, base, sid string, inbound <-chan agent.Inbound, done <-chan struct{}, fail func(error), interrupt func()) {
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
				if msg.Meta["opencodeQuestion"] != "" {
					if err := s.replyQuestion(ctx, client, base, msg); err != nil {
						fail(err)
					}
					continue
				}
				if s.apiV2 && msg.Meta["opencodeForm"] != "" {
					if err := s.replyV2Form(ctx, client, base, sid, msg); err != nil {
						fail(err)
					}
					continue
				}
				pid := agent.FirstNonEmpty(msg.InteractionID, msg.Meta["permissionID"])
				if pid == "" {
					continue
				}
				decision := "once"
				if agent.Denied(msg.Decision) {
					decision = "reject"
				}
				if err := s.decide(ctx, client, base, sid, pid, decision, msg.Meta["opencodePermissionAPI"] == "request"); err != nil {
					fail(err)
				}
			case "interrupt":
				interrupt()
				if err := s.abort(ctx, client, base, sid); err != nil {
					fail(err)
				}
			}
		}
	}
}

// ── REST helpers ──

// ensureSession reuses the resume id when present, else creates a session bound to the turn's cwd.
func (s server) ensureSession(ctx context.Context, client *http.Client, base string) (string, error) {
	if s.apiV2 {
		return s.ensureV2Session(ctx, client, base)
	}
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
	if s.apiV2 {
		return s.promptV2(ctx, client, base, sid)
	}
	parts := []any{}
	if s.contextInstructions != "" {
		// A native synthetic context part leaves the selected agent's built-in prompt
		// and tools intact. It is not user-authored text or another conversation.
		parts = append(parts, map[string]any{"type": "text", "text": s.contextInstructions, "synthetic": true})
	}
	parts = append(parts, map[string]any{"type": "text", "text": s.message})
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
	if s.apiV2 {
		return s.doJSON(ctx, client, http.MethodPost, base+"/api/session/"+url.PathEscape(sid)+"/compact", []byte(`{}`), nil)
	}
	body, _ := json.Marshal(map[string]any{"providerID": s.provider, "modelID": s.model})
	return s.doJSON(ctx, client, http.MethodPost, base+"/session/"+url.PathEscape(sid)+"/summarize", body, nil)
}

// decide POSTs a permission decision. response is opencode's vocabulary: once (allow this call),
// always (allow for the rest of the turn — the autonomous default), reject (deny). Best-effort.
func (s server) decide(ctx context.Context, client *http.Client, base, sid, permID, response string, requestAPI bool) error {
	if sid == "" || permID == "" {
		return errors.New("missing OpenCode permission identity")
	}
	body, _ := json.Marshal(map[string]any{"response": response})
	u := base + "/session/" + url.PathEscape(sid) + "/permissions/" + url.PathEscape(permID)
	if requestAPI {
		u = base + "/permission/" + url.PathEscape(permID) + "/reply"
		body, _ = json.Marshal(map[string]any{"reply": response})
	}
	if s.apiV2 {
		u = base + "/api/session/" + url.PathEscape(sid) + "/permission/" + url.PathEscape(permID) + "/reply"
		body, _ = json.Marshal(map[string]any{"decision": response})
	}
	return s.doJSON(ctx, client, http.MethodPost, u, body, nil)
}

// abort asks opencode to stop the running turn and returns the native acknowledgement.
func (s server) abort(ctx context.Context, client *http.Client, base, sid string) error {
	if sid == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	path := "/session/" + url.PathEscape(sid) + "/abort"
	if s.apiV2 {
		path = "/api/session/" + url.PathEscape(sid) + "/interrupt?resume=false"
	}
	return s.doJSON(ctx, client, http.MethodPost, base+path, nil, nil)
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
	s.authorize(req)
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

func (s server) authorize(req *http.Request) {
	if s.password != "" {
		req.SetBasicAuth("opencode", s.password)
	}
}

// V2 serves its HTML app at unknown V1 routes with HTTP 200. Only a JSON health
// response establishes readiness; otherwise probe V2's authenticated server info.
func (s server) waitHealthy(ctx context.Context, base string) (bool, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.NewTimer(healthDeadline)
	defer deadline.Stop()
	tick := time.NewTicker(healthPoll)
	defer tick.Stop()
	for {
		for _, path := range []string{"/global/health", "/api/info"} {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
			s.authorize(req)
			if resp, err := client.Do(req); err == nil {
				var info struct {
					Healthy bool   `json:"healthy"`
					Version string `json:"version"`
				}
				valid := resp.StatusCode == http.StatusOK && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") &&
					json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&info) == nil
				resp.Body.Close()
				if valid && ((path == "/global/health" && info.Healthy) || (path == "/api/info" && info.Version != "")) {
					return path == "/api/info", nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return false, errors.New("timeout")
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
