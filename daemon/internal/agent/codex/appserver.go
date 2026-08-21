package codex

// appserver.go is the codebase's first BESPOKE Driver: a turn that must pause for the user (a
// non-`never` approval policy) runs over `codex app-server` — an experimental JSON-RPC-style
// persistent transport spoken over stdio — instead of the one-shot `codex exec` hot path. The
// architecture sanctions this: "a long-lived process spoken to over a protocol implements Driver
// directly." driver.Persistent can't be reused because it is a dumb line-pump — it writes a static
// preamble then relays Inbound and cannot AWAIT a response between writes, whereas Codex's handshake
// needs request→await→next-request correlation (turn/start needs the threadId returned by
// thread/start).
//
// Wire framing (verified from `codex app-server generate-json-schema`, codex-cli 0.146.0): NOT
// standard JSON-RPC — there is no "jsonrpc":"2.0" version field. Each stdout line is one message:
//
//	request       {id, method, params}   we send these; the server replies with a response
//	notification  {method, params}        server→client events (no id, no reply)
//	server-request{id, method, params}    server→client asks (approvals) — we must reply with a response
//	response      {id, result} | {id, error}
//
// Everything is camelCase (the exec `--json` stream is snake_case — a different surface, parse.go).
//
// UNVERIFIED AT RUNTIME: the shapes are schema-derived but the live behavior could not be exercised
// (the local ChatGPT OAuth token is expired) — the Stage-7 smoke test is the blocking acceptance gate.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
)

const (
	clientName = "mindwire"

	// Approval-response families: a server-request answer must take the native decision shape of the
	// request that raised it, and the two generations differ. We tag each emitted interaction with its
	// family (in the approvals map) so the Inbound handler can encode the right reply statelessly.
	famReviewDecision = "reviewDecision" // legacy execCommandApproval / applyPatchApproval → {"decision":"approved"|{"denied":…}}
	famV2Decision     = "v2Decision"     // item/*/requestApproval (experimental) → {"decision":"accept"|"decline"}
	famAnswers        = "answers"        // item/tool/requestUserInput → {"answers":{…}}
)

var errTransportClosed = errors.New("codex app-server transport closed")

// appServer runs one turn over `codex app-server`. RunStream pre-resolves the turn parameters into
// these fields (mirroring how buildExecCommand resolves the exec flags) so the driver only builds the
// typed JSON-RPC params. Secrets never appear here — they flow through env, exactly like the CLI path.
type appServer struct {
	command  string            // shell command to launch the server (assembled by RunStream)
	env      map[string]string // auth/runtime env (from AuthModule.EnvForRun via in.Env)
	message  string            // the user's turn message
	model    string            // "" ⇒ omit (CLI default)
	effort   string            // reasoning effort; "" ⇒ omit
	sandbox  string            // sandbox posture (enum)
	approval string            // approval policy (enum)
	cwd      string            // working directory for the thread
	resumeID string            // thread id to resume; "" ⇒ start a fresh thread
	compact  bool              // on-demand compaction: after resume, send thread/compact/start instead of turn/start
}

// Run spawns the app-server process, drives one turn over its stdio, and returns the terminal result.
// It owns the process/stderr plumbing (like driver.CLI/Persistent); converse owns the protocol.
func (a appServer) Run(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", a.command)
	proc.Group(cmd) // cancel/interrupt kills the whole app-server tree, not just the bash parent
	cmd.Env = os.Environ()
	for k, v := range a.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	// Expose this turn's process group to resource monitoring (no-op if none is wired on ctx).
	proc.Report(ctx, cmd.Process.Pid)

	result, got := a.converse(ctx, stdin, stdout, in.Inbound, emit)
	_ = stdin.Close() // signal the server we're done so it exits and stdout hits EOF
	werr := cmd.Wait()

	if !got {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && werr != nil {
			msg = werr.Error()
		}
		if msg == "" {
			msg = strings.TrimSpace(result.Text)
		}
		if msg == "" {
			msg = "no result from codex app-server"
		}
		emit(agent.Event{Type: agent.EventError, Error: msg})
		return agent.TurnResult{Text: msg, IsError: true}, werr
	}
	return result, nil
}

// --- wire frames -------------------------------------------------------------------------------

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params any             `json:"params,omitempty"`
}

type rpcNotification struct {
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result"`
}

// rpcIn is any inbound line. Classification: method+id ⇒ server-request; method only ⇒ notification;
// id only ⇒ response (result or error).
type rpcIn struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcErr         `json:"error"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type pendingResp struct {
	result json.RawMessage
	err    *rpcErr
}

// approval is the correlator for one outstanding server-request, keyed by the interaction id we
// emitted. It carries the raw JSON-RPC id to echo in the reply and the decision family to encode.
type approval struct {
	rawID      json.RawMessage
	family     string
	questionID string // famAnswers only: the first question's id
}

// serializeEmit wraps an Emit so it is safe to call from multiple goroutines. The app-server transport
// emits from several goroutines at once (the stdout reader, the turn-await watcher, the handshake), but
// the runner's emit accumulates unsynchronized transcript state and assumes serial calls — as the CLI
// path (single parse goroutine) provides. Every app-server emission goes through this.
func serializeEmit(emit agent.Emit) agent.Emit {
	var mu sync.Mutex
	return func(ev agent.Event) {
		mu.Lock()
		defer mu.Unlock()
		emit(ev)
	}
}

// converse runs the full protocol state machine over an already-open stdio pair. It is split out from
// Run so tests drive it with in-memory pipes and a scripted server (no real binary). It returns the
// terminal result and whether one was seen (got=false ⇒ Run surfaces stderr/exit as the error).
func (a appServer) converse(ctx context.Context, w io.Writer, r io.Reader, inbound <-chan agent.Inbound, rawEmit agent.Emit) (agent.TurnResult, bool) {
	// emit is driven from several goroutines here (the reader, the turn-await watcher, and this
	// handshake path's EventSession) — unlike the CLI path, which parses on a single goroutine. The
	// runner's emit accumulates shared, unsynchronized transcript state, so serialize every emission.
	emit := serializeEmit(rawEmit)

	var wmu sync.Mutex
	bw := bufio.NewWriter(w)
	writeJSON := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		wmu.Lock()
		defer wmu.Unlock()
		if _, err := bw.Write(b); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
		return bw.Flush()
	}

	var idmu sync.Mutex
	idSeq := 0
	pending := map[string]chan pendingResp{}
	nextID := func() json.RawMessage {
		idmu.Lock()
		defer idmu.Unlock()
		idSeq++
		raw, _ := json.Marshal("mw-" + strconv.Itoa(idSeq))
		return raw
	}
	// call sends a request and returns a channel that will receive its response.
	call := func(method string, params any) (chan pendingResp, error) {
		id := nextID()
		ch := make(chan pendingResp, 1)
		idmu.Lock()
		pending[string(id)] = ch
		idmu.Unlock()
		return ch, writeJSON(rpcRequest{ID: id, Method: method, Params: params})
	}
	// sendRequest fires a request we don't await (steer/interrupt); its response is dropped by the reader.
	sendRequest := func(method string, params any) error {
		return writeJSON(rpcRequest{ID: nextID(), Method: method, Params: params})
	}

	// shared turn state
	var smu sync.Mutex
	var threadID, turnID, sessionID string
	var tokens map[string]any
	var tokensUsage *agent.Usage // typed mirror of tokens, attached additively to the terminal result
	approvals := map[string]approval{}

	// compactTrigger tags every compaction boundary this connection surfaces: "manual" when we drove a
	// thread/compact/start (a.compact), "auto" when Codex compacted mid-turn on its own. A compact turn
	// terminates on the boundary; an ordinary turn merely surfaces it and keeps going.
	compactTrigger := "auto"
	if a.compact {
		compactTrigger = "manual"
	}

	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }

	var result agent.TurnResult
	var got bool
	// emitTerminal records the turn's result and emits the single EventResult (first caller wins), then
	// unblocks everyone waiting on done. Two sources can race here — the turn/completed notification and
	// the turn/start response — and exactly one must surface.
	emitTerminal := func(res agent.TurnResult, meta map[string]any) {
		first := false
		smu.Lock()
		if !got {
			got, result, first = true, res, true
		}
		usage := tokensUsage
		smu.Unlock()
		if first {
			ev := agent.Event{Type: agent.EventResult, SessionID: res.SessionID,
				Result: &agent.ResultInfo{Text: res.Text, IsError: res.IsError, SessionID: res.SessionID}}
			if meta != nil {
				ev.Meta = meta
			}
			if usage != nil {
				ev.Result.Usage = usage // additive typed tokens alongside the Meta usage
			}
			emit(ev)
		}
		closeDone()
	}
	tokenMeta := func() map[string]any {
		smu.Lock()
		defer smu.Unlock()
		return tokens
	}

	// Reader goroutine: the single owner of stdout. Routes responses to waiters, maps notifications to
	// unified events, and turns server-requests into interactions. Stops mapping once terminal (late
	// frames are drained, not surfaced) so no event escapes after the result.
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
		terminated := false
		finalText := "" // last agentMessage seen mid-turn (fallback for the terminal text)
		st := newStreamState()
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var msg rpcIn
			if json.Unmarshal([]byte(line), &msg) != nil {
				continue
			}
			switch {
			case msg.Method != "" && len(msg.ID) > 0:
				if terminated {
					continue
				}
				if inter, ap := serverRequestInteraction(msg); inter != nil {
					smu.Lock()
					approvals[inter.ID] = ap
					smu.Unlock()
					emit(agent.Event{Type: agent.EventInteraction, Interaction: inter})
				}

			case msg.Method != "":
				if terminated {
					continue
				}
				switch msg.Method {
				case "thread/started":
					var p struct {
						Thread struct {
							ID        string `json:"id"`
							SessionID string `json:"sessionId"`
						} `json:"thread"`
					}
					if json.Unmarshal(msg.Params, &p) == nil {
						sid := agent.FirstNonEmpty(p.Thread.SessionID, p.Thread.ID)
						smu.Lock()
						if threadID == "" {
							threadID = p.Thread.ID
						}
						if sessionID == "" {
							sessionID = sid
						}
						smu.Unlock()
					}
				case "turn/started":
					var p struct {
						Turn struct {
							ID string `json:"id"`
						} `json:"turn"`
					}
					if json.Unmarshal(msg.Params, &p) == nil && p.Turn.ID != "" {
						smu.Lock()
						if turnID == "" {
							turnID = p.Turn.ID
						}
						smu.Unlock()
					}
				case "item/started":
					var p struct {
						Item json.RawMessage `json:"item"`
					}
					if json.Unmarshal(msg.Params, &p) == nil {
						emitItem(phaseStarted, p.Item, emit, st)
					}
				case "item/updated":
					// Streaming growth: a text/thinking item's suffix is emitted as a Delta=true event, a
					// tool_use is announced once. Mirrors the exec transport's item.updated handling.
					var p struct {
						Item json.RawMessage `json:"item"`
					}
					if json.Unmarshal(msg.Params, &p) == nil {
						if t := emitItem(phaseUpdated, p.Item, emit, st); t != "" {
							finalText = t
						}
					}
				case "item/completed":
					var p struct {
						Item json.RawMessage `json:"item"`
					}
					if json.Unmarshal(msg.Params, &p) == nil {
						// A contextCompaction item is the compaction boundary (preferred terminal signal for a
						// compact turn; also emitted when Codex auto-compacts mid-turn). Surface it as
						// EventCompaction; in compact mode it's the terminal result.
						if ci := compactionItem(p.Item, compactTrigger); ci != nil {
							smu.Lock()
							sid := sessionID
							smu.Unlock()
							emit(agent.Event{Type: agent.EventCompaction, SessionID: sid, Compaction: ci})
							if a.compact {
								terminated = true
								emitTerminal(agent.TurnResult{Text: "Conversation compacted.", SessionID: sid}, tokenMeta())
							}
						} else if t := emitItem(phaseCompleted, p.Item, emit, st); t != "" {
							finalText = t
						}
					}
				case "turn/plan/updated":
					if inter := planInteraction(msg.Params); inter != nil {
						emit(agent.Event{Type: agent.EventInteraction, Interaction: inter})
					}
				case "thread/tokenUsage/updated":
					if u, ok := tokenUsageFrom(msg.Params); ok {
						m := usageMeta(u)
						smu.Lock()
						tokens = m
						tokensUsage = usageStruct(u)
						smu.Unlock()
						// Live token telemetry: surface the running usage as a status event (in addition to
						// stashing it for the terminal result's Meta + typed Usage).
						emit(agent.Event{Type: agent.EventStatus, Meta: m})
					}
				case "turn/completed":
					text, isErr := turnOutcome(msg.Params)
					if text == "" {
						text = finalText
					}
					smu.Lock()
					sid := sessionID
					smu.Unlock()
					terminated = true
					emitTerminal(agent.TurnResult{Text: text, SessionID: sid, IsError: isErr}, tokenMeta())
				case "thread/compacted":
					// Legacy terminal signal for a compaction, superseded by the contextCompaction item above.
					// Handle it too so an older server still terminates a compact turn (and a mid-turn
					// auto-compaction still surfaces). Harmless if the item already fired — emitTerminal is
					// first-wins and EventCompaction is idempotent for the client.
					smu.Lock()
					sid := sessionID
					smu.Unlock()
					emit(agent.Event{Type: agent.EventCompaction, SessionID: sid,
						Compaction: &agent.CompactionInfo{Trigger: compactTrigger}})
					if a.compact {
						terminated = true
						emitTerminal(agent.TurnResult{Text: "Conversation compacted.", SessionID: sid}, tokenMeta())
					}
				case "error":
					var p struct {
						Error struct {
							Message string `json:"message"`
						} `json:"error"`
						WillRetry bool `json:"willRetry"`
					}
					_ = json.Unmarshal(msg.Params, &p)
					m := strings.TrimSpace(p.Error.Message)
					if m == "" {
						m = "codex error"
					}
					// Non-terminal on its own; a turn/completed with status=failed is the terminal signal.
					emit(agent.Event{Type: agent.EventError, Error: m})
				}

			case len(msg.ID) > 0:
				idmu.Lock()
				ch := pending[string(msg.ID)]
				delete(pending, string(msg.ID))
				idmu.Unlock()
				if ch != nil {
					ch <- pendingResp{result: msg.Result, err: msg.Error}
				}
			}
		}
		closeDone() // stdout ended → nothing more will arrive
	}()

	await := func(ch chan pendingResp) (json.RawMessage, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
			return nil, errTransportClosed
		case resp := <-ch:
			if resp.err != nil {
				return nil, fmt.Errorf("app-server error %d: %s", resp.err.Code, resp.err.Message)
			}
			return resp.result, nil
		}
	}

	// 1. initialize handshake — arms the experimental v2 API (item/*/requestApproval etc.).
	ch, err := call("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": clientName, "version": agent.Version},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err != nil {
		return agent.TurnResult{Text: "initialize: " + err.Error()}, false
	}
	if _, err := await(ch); err != nil {
		return agent.TurnResult{Text: "initialize: " + err.Error()}, false
	}
	// 2. initialized notification.
	if err := writeJSON(rpcNotification{Method: "initialized"}); err != nil {
		return agent.TurnResult{Text: err.Error()}, false
	}

	// 3. start or resume the thread, capturing its id (needed for turn/start).
	if a.resumeID != "" {
		ch, err = call("thread/resume", a.resumeParams())
	} else {
		ch, err = call("thread/start", a.startParams())
	}
	if err != nil {
		return agent.TurnResult{Text: "thread: " + err.Error()}, false
	}
	res, err := await(ch)
	if err != nil {
		return agent.TurnResult{Text: "thread: " + err.Error()}, false
	}
	var ts struct {
		Thread struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
		} `json:"thread"`
	}
	_ = json.Unmarshal(res, &ts)
	tid := agent.FirstNonEmpty(ts.Thread.ID, a.resumeID)
	sid := agent.FirstNonEmpty(ts.Thread.SessionID, tid)
	smu.Lock()
	if threadID == "" {
		threadID = tid
	} else {
		tid = threadID
	}
	if sessionID == "" {
		sessionID = sid
	} else {
		sid = sessionID
	}
	smu.Unlock()
	if sid != "" {
		emit(agent.Event{Type: agent.EventSession, SessionID: sid})
	}

	// 4. Kick off the work. In compact mode: thread/compact/start, whose immediate {} response is a mere
	// "accepted" ACK, not a terminal — the terminal signal is the streamed contextCompaction item (or a
	// legacy thread/compacted notification), which the reader turns into the terminal result. Otherwise:
	// turn/start, whose response may resolve only when the turn ends (carrying the final turn), so we
	// don't block on it here — we await it in a goroutine as a second terminal source, and capture the
	// turn id (for steer/interrupt) from either the response or the turn/started notification.
	if a.compact {
		compactCh, err := call("thread/compact/start", map[string]any{"threadId": tid})
		if err != nil {
			return agent.TurnResult{Text: "compact: " + err.Error()}, false
		}
		go func() {
			// Await ONLY to surface an early rejection (e.g. thread not compactable); a successful {} ack
			// means accepted, not done, so we keep waiting for the reader's compaction boundary.
			if _, err := await(compactCh); err != nil {
				if errors.Is(err, errTransportClosed) || errors.Is(err, context.Canceled) {
					return
				}
				emitTerminal(agent.TurnResult{Text: "compact: " + err.Error(), IsError: true}, nil)
			}
		}()
	} else {
		turnCh, err := call("turn/start", a.turnParams(tid))
		if err != nil {
			return agent.TurnResult{Text: "turn: " + err.Error()}, false
		}
		go func() {
			res, err := await(turnCh)
			if err != nil {
				if errors.Is(err, errTransportClosed) || errors.Is(err, context.Canceled) {
					return // done/ctx already drives the terminal path
				}
				emitTerminal(agent.TurnResult{Text: "turn: " + err.Error(), IsError: true}, nil)
				return
			}
			if uid := turnIDOf(res); uid != "" {
				smu.Lock()
				if turnID == "" {
					turnID = uid
				}
				smu.Unlock()
			}
			text, isErr := turnOutcome(res)
			smu.Lock()
			s := sessionID
			smu.Unlock()
			emitTerminal(agent.TurnResult{Text: text, SessionID: s, IsError: isErr}, tokenMeta())
		}()
	}

	// Inbound pump: answer approvals, steer, or interrupt mid-turn. Stateless-correlator pattern —
	// the approval reply id rides in the approvals map (keyed by the interaction id we handed out).
	go func() {
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
					smu.Lock()
					ap, found := approvals[msg.InteractionID]
					smu.Unlock()
					if !found {
						continue
					}
					_ = writeJSON(rpcResponse{ID: ap.rawID, Result: decisionResult(ap, msg)})
				case "input":
					text := strings.TrimSpace(msg.Text)
					if text == "" {
						continue
					}
					smu.Lock()
					t, u := threadID, turnID
					smu.Unlock()
					if t == "" || u == "" {
						continue
					}
					_ = sendRequest("turn/steer", map[string]any{
						"threadId":       t,
						"expectedTurnId": u,
						"input":          []any{map[string]any{"type": "text", "text": text}},
					})
				case "interrupt":
					smu.Lock()
					t, u := threadID, turnID
					smu.Unlock()
					if t == "" || u == "" {
						continue
					}
					_ = sendRequest("turn/interrupt", map[string]any{"threadId": t, "turnId": u})
				}
			}
		}
	}()

	// Wait for the turn to finish (or the context to cancel). On cancel, ask the server to interrupt so
	// it stops the running turn cleanly before we close stdin.
	select {
	case <-done:
	case <-ctx.Done():
		smu.Lock()
		t, u := threadID, turnID
		smu.Unlock()
		if t != "" && u != "" {
			_ = sendRequest("turn/interrupt", map[string]any{"threadId": t, "turnId": u})
		}
	}

	smu.Lock()
	res2, ok := result, got
	smu.Unlock()
	if !ok {
		return agent.TurnResult{Text: "codex app-server ended without a result"}, false
	}
	return res2, true
}

// --- request params ----------------------------------------------------------------------------

func (a appServer) startParams() map[string]any {
	p := map[string]any{
		"sandbox":        a.sandbox,
		"approvalPolicy": a.approval,
	}
	if a.model != "" {
		p["model"] = a.model
	}
	if a.cwd != "" {
		p["cwd"] = a.cwd
	}
	return p
}

func (a appServer) resumeParams() map[string]any {
	p := map[string]any{
		"threadId":       a.resumeID,
		"sandbox":        a.sandbox,
		"approvalPolicy": a.approval,
	}
	if a.model != "" {
		p["model"] = a.model
	}
	return p
}

func (a appServer) turnParams(threadID string) map[string]any {
	p := map[string]any{
		"threadId": threadID,
		"input":    []any{map[string]any{"type": "text", "text": a.message}},
	}
	if a.model != "" {
		p["model"] = a.model
	}
	if a.effort != "" {
		p["effort"] = a.effort
	}
	return p
}

// --- server-request → interaction --------------------------------------------------------------

// serverRequestInteraction maps a server-request (an approval or a user-input ask) to a unified
// interaction plus the correlator needed to answer it. The interaction id is the raw JSON-RPC id (a
// string or number) rendered as text; the approval carries that raw id back for the reply. Unknown
// request methods still map to a yes/no approval so the turn can't hang unanswered.
func serverRequestInteraction(msg rpcIn) (*agent.Interaction, approval) {
	id := string(msg.ID)
	allowDeny := []agent.Action{{ID: "allow", Label: "Approve"}, {ID: "deny", Label: "Reject"}}

	switch msg.Method {
	case "execCommandApproval", "applyPatchApproval":
		var p struct {
			Command string   `json:"command"`
			Cwd     string   `json:"cwd"`
			Reason  string   `json:"reason"`
			Parsed  []string `json:"parsedCmd"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		title := "Approve command?"
		if msg.Method == "applyPatchApproval" {
			title = "Apply file changes?"
		} else if p.Command != "" {
			title = "Run: " + p.Command
		}
		return &agent.Interaction{
			ID: id, Kind: "approval", Title: title, Detail: p.Reason,
			Options: allowDeny, NeedsResponse: true,
			Meta: map[string]any{"method": msg.Method},
		}, approval{rawID: msg.ID, family: famReviewDecision}

	case "item/commandExecution/requestApproval":
		var p struct {
			Command string `json:"command"`
			Cwd     string `json:"cwd"`
			Reason  string `json:"reason"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		title := "Approve command?"
		if p.Command != "" {
			title = "Run: " + p.Command
		}
		return &agent.Interaction{
			ID: id, Kind: "approval", Title: title, Detail: p.Reason,
			Options: allowDeny, NeedsResponse: true,
			Meta: map[string]any{"method": msg.Method},
		}, approval{rawID: msg.ID, family: famV2Decision}

	case "item/fileChange/requestApproval", "item/permissions/requestApproval":
		var p struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		return &agent.Interaction{
			ID: id, Kind: "approval", Title: "Apply file changes?", Detail: p.Reason,
			Options: allowDeny, NeedsResponse: true,
			Meta: map[string]any{"method": msg.Method},
		}, approval{rawID: msg.ID, family: famV2Decision}

	case "item/tool/requestUserInput":
		var p struct {
			Questions []struct {
				ID       string `json:"id"`
				Header   string `json:"header"`
				Question string `json:"question"`
			} `json:"questions"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		title, detail, qid := "Codex needs your input", "", ""
		if len(p.Questions) > 0 {
			title = agent.FirstNonEmpty(p.Questions[0].Question, title)
			detail = p.Questions[0].Header
			qid = p.Questions[0].ID
		}
		return &agent.Interaction{
			ID: id, Kind: "choice", Title: title, Detail: detail, NeedsResponse: true,
			Meta: map[string]any{"method": msg.Method},
		}, approval{rawID: msg.ID, family: famAnswers, questionID: qid}

	default:
		// Any other server-request (e.g. an mcp elicitation) — surface as a yes/no so the turn proceeds.
		return &agent.Interaction{
			ID: id, Kind: "approval", Title: "Codex needs your approval", Detail: msg.Method,
			Options: allowDeny, NeedsResponse: true,
			Meta: map[string]any{"method": msg.Method},
		}, approval{rawID: msg.ID, family: famV2Decision}
	}
}

// decisionResult encodes an approval answer into the native decision shape for its family.
func decisionResult(ap approval, in agent.Inbound) any {
	deny := agent.Denied(in.Decision)
	switch ap.family {
	case famReviewDecision:
		if deny {
			reason := strings.TrimSpace(in.Text)
			if reason == "" {
				reason = "Denied by user"
			}
			return map[string]any{"decision": map[string]any{"denied": map[string]any{"rejection": reason}}}
		}
		return map[string]any{"decision": "approved"}
	case famAnswers:
		answer := strings.TrimSpace(in.Text)
		if answer == "" && len(in.Options) > 0 {
			answer = strings.Join(in.Options, ", ")
		}
		if answer == "" {
			answer = in.Decision
		}
		answers := map[string]any{}
		if ap.questionID != "" {
			answers[ap.questionID] = answer
		}
		return map[string]any{"answers": answers}
	default: // famV2Decision
		if deny {
			return map[string]any{"decision": "decline"}
		}
		return map[string]any{"decision": "accept"}
	}
}

// --- notification item → event -----------------------------------------------------------------

// asItem is a ThreadItem (camelCase, app-server). Fields are the union across item types; only the
// ones relevant to a given type are populated. Unknown fields are ignored (forward-compatible).
// fromAsItem (normalize.go) maps this into the transport-independent normItem.
type asItem struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Text       string          `json:"text"`             // agentMessage / plan
	Content    []string        `json:"content"`          // reasoning
	Summary    []string        `json:"summary"`          // reasoning (alt)
	Command    string          `json:"command"`          // commandExecution
	Cwd        string          `json:"cwd"`              // commandExecution (may be absent on app-server turns)
	Aggregated string          `json:"aggregatedOutput"` // commandExecution
	ExitCode   *int            `json:"exitCode"`
	Status     string          `json:"status"` // inProgress|completed|failed|declined
	Changes    []normChange    `json:"changes"`
	Query      string          `json:"query"`  // webSearch
	Server     string          `json:"server"` // mcpToolCall
	Tool       string          `json:"tool"`   // mcpToolCall / dynamicToolCall
	Result     json.RawMessage `json:"result"` // mcpToolCall
}

// emitItem maps one item (at started/updated/completed) to events, returning the item's text when it is
// a completed agent message (the running final-answer text). It normalizes the app-server item into
// normItem and shares the streaming emit tail with the exec transport (normalize.go): text/thinking
// stream as Delta=true suffixes across phases, so nothing is double-counted.
func emitItem(phase itemPhase, raw json.RawMessage, emit agent.Emit, st *streamState) string {
	var it asItem
	if json.Unmarshal(raw, &it) != nil {
		return ""
	}
	return emitNorm(fromAsItem(it), phase, raw, emit, st)
}

// compactionItem detects a contextCompaction ThreadItem and returns its normalized CompactionInfo — the
// app-server's compaction boundary, emitted as item/started+item/completed during a thread/compact/start
// (on-demand) or when Codex auto-compacts mid-turn. trigger says which ("manual" | "auto"). Returns nil
// for any other item type. Fields beyond {id, type} are best-effort (the schema only guarantees those);
// a summary is captured when present so the reloaded transcript can show it.
func compactionItem(raw json.RawMessage, trigger string) *agent.CompactionInfo {
	var it struct {
		Type    string `json:"type"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	}
	if json.Unmarshal(raw, &it) != nil || it.Type != "contextCompaction" {
		return nil
	}
	return &agent.CompactionInfo{Trigger: trigger, Summary: agent.FirstNonEmpty(it.Summary, it.Text)}
}

// planInteraction maps a turn/plan/updated notification to a unified todos interaction (via the shared
// buildTodos tail).
func planInteraction(params json.RawMessage) *agent.Interaction {
	var p struct {
		Plan []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"plan"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.Plan) == 0 {
		return nil
	}
	rows := make([]todoRow, 0, len(p.Plan))
	for _, s := range p.Plan {
		rows = append(rows, todoRow{Content: s.Step, Status: s.Status})
	}
	return buildTodos(rows)
}

// tokenUsageFrom decodes a thread/tokenUsage/updated notification into the normalized tokenUsage
// (the app-server transport always carries a grand total, so HasTotal is set). ok=false on decode
// failure. Shared by tokenUsageMeta (Meta shape) and usageStruct (typed result Usage).
func tokenUsageFrom(params json.RawMessage) (tokenUsage, bool) {
	var p struct {
		TokenUsage struct {
			Total struct {
				InputTokens           int `json:"inputTokens"`
				CachedInputTokens     int `json:"cachedInputTokens"`
				OutputTokens          int `json:"outputTokens"`
				ReasoningOutputTokens int `json:"reasoningOutputTokens"`
				TotalTokens           int `json:"totalTokens"`
			} `json:"total"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(params, &p) != nil {
		return tokenUsage{}, false
	}
	t := p.TokenUsage.Total
	return tokenUsage{
		InputTokens:           t.InputTokens,
		CachedInputTokens:     t.CachedInputTokens,
		OutputTokens:          t.OutputTokens,
		ReasoningOutputTokens: t.ReasoningOutputTokens,
		TotalTokens:           t.TotalTokens,
		HasTotal:              true,
	}, true
}

// tokenUsageMeta pulls the cumulative token counts from a thread/tokenUsage/updated notification into
// the Meta shape used across transports (Codex reports tokens, not USD). Returns nil on decode failure.
func tokenUsageMeta(params json.RawMessage) map[string]any {
	u, ok := tokenUsageFrom(params)
	if !ok {
		return nil
	}
	return usageMeta(u)
}

// --- turn envelope helpers ---------------------------------------------------------------------

type turnEnvelope struct {
	Turn struct {
		ID     string `json:"id"`
		Status string `json:"status"` // completed|interrupted|failed|inProgress
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Items []asItem `json:"items"`
	} `json:"turn"`
}

func turnIDOf(raw json.RawMessage) string {
	var te turnEnvelope
	if json.Unmarshal(raw, &te) != nil {
		return ""
	}
	return te.Turn.ID
}

// turnOutcome derives a completed turn's final text and error flag from its envelope. The final text is
// the last agentMessage item; a failed turn with no text falls back to its error message.
func turnOutcome(raw json.RawMessage) (string, bool) {
	var te turnEnvelope
	if json.Unmarshal(raw, &te) != nil {
		return "", false
	}
	text := ""
	for _, it := range te.Turn.Items {
		if it.Type == "agentMessage" && strings.TrimSpace(it.Text) != "" {
			text = it.Text
		}
	}
	isErr := te.Turn.Status == "failed"
	if isErr && text == "" && te.Turn.Error != nil {
		text = te.Turn.Error.Message
	}
	return text, isErr
}
