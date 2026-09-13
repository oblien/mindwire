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
// Wire framing (verified from `codex app-server generate-json-schema`, codex-cli 0.153.4): NOT
// standard JSON-RPC — there is no "jsonrpc":"2.0" version field. Each stdout line is one message:
//
//	request       {id, method, params}   we send these; the server replies with a response
//	notification  {method, params}        server→client events (no id, no reply)
//	server-request{id, method, params}    server→client asks (approvals) — we must reply with a response
//	response      {id, result} | {id, error}
//
// Everything is camelCase (the exec `--json` stream is snake_case — a different surface, parse.go).
//
// Protocol reference: https://learn.chatgpt.com/docs/app-server. turn/start returns an
// inProgress acknowledgement; only a terminal turn status ends the run.

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
	famPermissions    = "permissions"
	famElicitation    = "elicitation"
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
		msg := ""
		if result.IsError {
			msg = strings.TrimSpace(result.Text)
		}
		if msg == "" {
			msg = strings.TrimSpace(stderr.String())
		}
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
		return agent.TurnResult{Text: msg, SessionID: result.SessionID, IsError: true}, werr
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
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcErr) text() string {
	message := e.Message
	if detail := errorText(e.Data, ""); detail != "" && !strings.Contains(message, detail) {
		message = strings.TrimSpace(message + "\n" + detail)
	}
	return fmt.Sprintf("Codex RPC error %d: %s", e.Code, message)
}

type pendingResp struct {
	result json.RawMessage
	err    *rpcErr
}

// approval is the correlator for one outstanding server-request, keyed by the interaction id we
// emitted. It carries the raw JSON-RPC id to echo in the reply and the decision family to encode.
type approval struct {
	rawID         json.RawMessage
	family        string
	questionID    string // famAnswers only: this prompt's question id
	questionCount int
	permissions   json.RawMessage
	interaction   *agent.Interaction
}

// serializeEmit wraps an Emit so it is safe to call from multiple goroutines. The app-server transport
// emits from the stdout reader, handshake and inbound-response goroutines, but
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
	// emit is driven by the reader, inbound responses and this handshake path's EventSession,
	// unlike the CLI path, which parses on a single goroutine. The
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
	pendingMethods := map[string]string{}
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
		pendingMethods[string(id)] = method
		idmu.Unlock()
		return ch, writeJSON(rpcRequest{ID: id, Method: method, Params: params})
	}
	// Steering/interrupt acknowledgements do not block ingress, but their errors must
	// still reach the transcript (for example, trying to steer a review turn).
	sendRequest := func(method string, params any) error {
		id := nextID()
		idmu.Lock()
		pendingMethods[string(id)] = method
		idmu.Unlock()
		return writeJSON(rpcRequest{ID: id, Method: method, Params: params})
	}

	// shared turn state
	var smu sync.Mutex
	var threadID, turnID, sessionID string
	turnReady := make(chan struct{})
	var turnReadyOnce sync.Once
	recordTurnID := func(id string) {
		if id == "" {
			return
		}
		smu.Lock()
		if turnID == "" {
			turnID = id
		}
		smu.Unlock()
		turnReadyOnce.Do(func() { close(turnReady) })
	}
	var tokens map[string]any
	var tokensUsage *agent.Usage // typed mirror of tokens, attached additively to the terminal result
	approvals := map[string]approval{}
	questionAnswers := map[string]map[string]any{}

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
	var failureText string
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
		lastError := ""
		st := newStreamState()
		st.compactTrigger = compactTrigger
		// Both notifications and any terminal start response are reconciled on this reader.
		// It is the sole owner of item state, so final snapshots cannot race streaming deltas.
		completeTurn := func(raw json.RawMessage) {
			var te turnEnvelope
			if err := json.Unmarshal(raw, &te); err != nil {
				lastError = "Could not decode Codex turn: " + err.Error()
				st.decodeFailure(lastError, emit)
				return
			}
			recordTurnID(te.Turn.ID)
			smu.Lock()
			sid := sessionID
			smu.Unlock()
			if !terminalStatus(te.Turn.Status) {
				return
			}
			for _, item := range te.Turn.Items {
				emitItem(phaseCompleted, item, emit, st)
			}
			st.emitTurnDiff(te.Turn.ID, emit)
			text, isErr := turnOutcome(raw)
			if isErr {
				text = errorText(te.Turn.Error, agent.FirstNonEmpty(lastError, text, "Codex turn failed."))
			} else {
				text = st.finalText
				if text == "" && !st.visible {
					text, isErr = agent.FirstNonEmpty(lastError, st.decodeError, st.problem, "Codex finished without a response."), true
				}
				if a.compact && !isErr && text == "" {
					text = "Conversation compacted."
				}
			}
			terminated = true
			emitTerminal(agent.TurnResult{Text: text, SessionID: sid, IsError: isErr}, tokenMeta())
		}
		for sc.Scan() {
			if terminated {
				continue
			}
			line := strings.TrimSpace(sc.Text())
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var msg rpcIn
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				lastError = "Could not decode Codex event: " + err.Error()
				st.decodeFailure(lastError, emit)
				continue
			}
			switch {
			case msg.Method != "" && len(msg.ID) > 0:
				if terminated {
					continue
				}
				prompts := serverRequestPrompts(msg)
				if len(prompts) == 0 {
					message := "Unsupported Codex request: " + msg.Method
					_ = writeJSON(map[string]any{"id": msg.ID, "error": rpcErr{Code: -32601, Message: message}})
					lastError = message
					st.emitInteraction(&agent.Interaction{ID: "codex:unsupported:" + string(msg.ID), Kind: "error", Title: "Codex request unavailable", Detail: message,
						Meta: map[string]any{"source": "codex", "method": msg.Method}}, emit)
				}
				for _, prompt := range prompts {
					inter, ap := prompt.interaction, prompt
					smu.Lock()
					approvals[inter.ID] = ap
					smu.Unlock()
					emit(agent.Event{Type: agent.EventInteraction, Interaction: inter})
				}

			case msg.Method != "":
				if terminated {
					continue
				}
				var route struct {
					ThreadID string `json:"threadId"`
					TurnID   string `json:"turnId"`
					Turn     struct {
						ID string `json:"id"`
					} `json:"turn"`
				}
				_ = json.Unmarshal(msg.Params, &route)
				if route.TurnID == "" {
					route.TurnID = route.Turn.ID
				}
				smu.Lock()
				wrongThread := route.ThreadID != "" && threadID != "" && route.ThreadID != threadID
				wrongTurn := route.TurnID != "" && turnID != "" && route.TurnID != turnID
				smu.Unlock()
				if wrongThread || wrongTurn {
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
						recordTurnID(p.Turn.ID)
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
						emitItem(phaseUpdated, p.Item, emit, st)
					}
				case "item/completed":
					var p struct {
						Item json.RawMessage `json:"item"`
					}
					if json.Unmarshal(msg.Params, &p) == nil {
						emitItem(phaseCompleted, p.Item, emit, st)
						// A contextCompaction item is the compaction boundary (preferred terminal signal for a
						// compact turn; also emitted when Codex auto-compacts mid-turn). Surface it as
						// EventCompaction; in compact mode it's the terminal result.
						if ci := compactionItem(p.Item, compactTrigger); ci != nil {
							smu.Lock()
							sid := sessionID
							smu.Unlock()
							if a.compact {
								terminated = true
								emitTerminal(agent.TurnResult{Text: "Conversation compacted.", SessionID: sid}, tokenMeta())
							}
						}
					}
				case "turn/plan/updated":
					if inter := planInteraction(msg.Params); inter != nil {
						st.visible = true
						st.emitInteraction(inter, emit)
					}
				case "item/agentMessage/delta", "item/plan/delta", "item/reasoning/summaryTextDelta",
					"item/reasoning/summaryPartAdded", "item/reasoning/textDelta",
					"item/commandExecution/outputDelta", "item/fileChange/patchUpdated",
					"item/fileChange/outputDelta", "item/mcpToolCall/progress":
					st.emitDelta(msg.Method, msg.Params, emit)
				case "turn/diff/updated":
					var p struct {
						TurnID string `json:"turnId"`
						Diff   string `json:"diff"`
					}
					if json.Unmarshal(msg.Params, &p) == nil {
						st.turnDiff = p.Diff
						emit(agent.Event{Type: agent.EventStatus, Meta: map[string]any{"turnId": p.TurnID, "diff": p.Diff}})
					}
				case "warning", "guardianWarning", "configWarning", "deprecationNotice",
					"hook/started", "hook/completed", "mcpServer/startupStatus/updated",
					"item/commandExecution/terminalInteraction", "model/rerouted", "model/verification",
					"modelProvider/authRecoveryStarted", "modelProvider/authRecoveryCompleted",
					"item/autoApprovalReview/started", "item/autoApprovalReview/completed", "autoApprovalReview/strictReviewRequired":
					st.emitNotice(msg.Method, msg.Params, emit)
				case "serverRequest/resolved":
					var p struct {
						RequestID json.RawMessage `json:"requestId"`
					}
					if json.Unmarshal(msg.Params, &p) == nil {
						var resolved []agent.Interaction
						smu.Lock()
						for id, ap := range approvals {
							if string(ap.rawID) == string(p.RequestID) {
								inter := *ap.interaction
								inter.NeedsResponse = false
								resolved = append(resolved, inter)
								delete(approvals, id)
							}
						}
						delete(questionAnswers, string(p.RequestID))
						smu.Unlock()
						for _, inter := range resolved {
							emit(agent.Event{Type: agent.EventInteraction, Interaction: &inter})
						}
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
					completeTurn(msg.Params)
				case "thread/compacted":
					// Legacy terminal signal for a compaction, superseded by the contextCompaction item above.
					// Handle it too so an older server still terminates a compact turn (and a mid-turn
					// auto-compaction still surfaces). The stream state pairs it with the modern item.
					smu.Lock()
					sid := sessionID
					smu.Unlock()
					st.emitCompaction("", "", true, emit)
					if a.compact {
						terminated = true
						emitTerminal(agent.TurnResult{Text: "Conversation compacted.", SessionID: sid}, tokenMeta())
					}
				case "error":
					var p struct {
						Error     json.RawMessage `json:"error"`
						WillRetry bool            `json:"willRetry"`
					}
					_ = json.Unmarshal(msg.Params, &p)
					lastError = errorText(p.Error, "Codex error.")
					if p.WillRetry {
						emit(agent.Event{Type: agent.EventStatus, Meta: map[string]any{"message": lastError, "willRetry": true}})
					} else {
						emit(agent.Event{Type: agent.EventError, Error: lastError})
					}
				}

			case len(msg.ID) > 0:
				idmu.Lock()
				ch := pending[string(msg.ID)]
				method := pendingMethods[string(msg.ID)]
				delete(pending, string(msg.ID))
				delete(pendingMethods, string(msg.ID))
				idmu.Unlock()
				if !terminated && (method == "turn/start" || method == "thread/compact/start") {
					if msg.Error != nil {
						smu.Lock()
						sid := sessionID
						smu.Unlock()
						terminated = true
						emitTerminal(agent.TurnResult{Text: msg.Error.text(), IsError: true, SessionID: sid}, nil)
					} else if method == "turn/start" {
						completeTurn(msg.Result)
					}
				}
				if !terminated && msg.Error != nil && (method == "turn/steer" || method == "turn/interrupt") {
					st.emitInteraction(&agent.Interaction{ID: "codex:rpc:" + string(msg.ID), Kind: "error",
						Title: "Codex request failed", Detail: msg.Error.text(),
						Meta: map[string]any{"source": "codex", "method": method}}, emit)
				}
				if ch != nil {
					ch <- pendingResp{result: msg.Result, err: msg.Error}
				}
			}
		}
		if err := sc.Err(); err != nil && lastError == "" {
			lastError = "Codex stream read error: " + err.Error()
		}
		smu.Lock()
		failureText = agent.FirstNonEmpty(lastError, st.decodeError)
		smu.Unlock()
		closeDone() // stdout ended → nothing more will arrive
	}()

	await := func(ch chan pendingResp) (json.RawMessage, error) {
		var resp pendingResp
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
			// A server may send its rejection and close stdout immediately. Drain the queued
			// response before reporting EOF, otherwise the actionable RPC error is lost.
			select {
			case resp = <-ch:
			default:
				return nil, errTransportClosed
			}
		case resp = <-ch:
		}
		if resp.err != nil {
			return nil, errors.New(resp.err.text())
		}
		return resp.result, nil
	}

	// 1. initialize handshake — arms the experimental v2 API (item/*/requestApproval etc.).
	ch, err := call("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": clientName, "version": agent.Version},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err != nil {
		return agent.TurnResult{Text: "initialize: " + err.Error(), IsError: true}, false
	}
	if _, err := await(ch); err != nil {
		return agent.TurnResult{Text: "initialize: " + err.Error(), IsError: true}, false
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
		return agent.TurnResult{Text: "thread: " + err.Error(), IsError: true}, false
	}
	res, err := await(ch)
	if err != nil {
		return agent.TurnResult{Text: "thread: " + err.Error(), IsError: true}, false
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
	// turn/start returns the initial inProgress turn. The reader captures that id and continues
	// consuming items until turn/completed; the acknowledgement must never close the transport.
	if a.compact {
		_, err := call("thread/compact/start", map[string]any{"threadId": tid})
		if err != nil {
			return agent.TurnResult{Text: "compact: " + err.Error()}, false
		}
	} else {
		_, err := call("turn/start", a.turnParams(tid))
		if err != nil {
			return agent.TurnResult{Text: "turn: " + err.Error()}, false
		}
	}

	// Inbound pump: answer approvals, steer, or interrupt mid-turn. Stateless-correlator pattern —
	// the approval reply id rides in the approvals map (keyed by the interaction id we handed out).
	awaitTurn := func() (string, string, bool) {
		select {
		case <-turnReady:
			smu.Lock()
			defer smu.Unlock()
			return threadID, turnID, true
		case <-done:
			return "", "", false
		case <-ctx.Done():
			return "", "", false
		}
	}
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
					if !found {
						smu.Unlock()
						continue
					}
					delete(approvals, msg.InteractionID)
					reply := decisionResult(ap, msg)
					ready := true
					if ap.family == famAnswers && ap.questionCount > 1 {
						key := string(ap.rawID)
						if questionAnswers[key] == nil {
							questionAnswers[key] = map[string]any{}
						}
						questionAnswers[key][ap.questionID] = map[string]any{"answers": answerValues(msg)}
						ready = len(questionAnswers[key]) == ap.questionCount
						if ready {
							reply = map[string]any{"answers": questionAnswers[key]}
							delete(questionAnswers, key)
						}
					}
					smu.Unlock()
					if ap.interaction != nil {
						resolved := *ap.interaction
						resolved.NeedsResponse = false
						emit(agent.Event{Type: agent.EventInteraction, Interaction: &resolved})
					}
					if ready {
						if err := writeJSON(rpcResponse{ID: ap.rawID, Result: reply}); err != nil {
							emit(agent.Event{Type: agent.EventError, Error: "Could not answer Codex: " + err.Error()})
						}
					}
				case "input":
					text := strings.TrimSpace(msg.Text)
					if text == "" {
						continue
					}
					t, u, ready := awaitTurn()
					if !ready {
						return
					}
					if err := sendRequest("turn/steer", map[string]any{
						"threadId":       t,
						"expectedTurnId": u,
						"input":          []any{map[string]any{"type": "text", "text": text}},
					}); err != nil {
						emit(agent.Event{Type: agent.EventError, Error: "Could not send Codex input: " + err.Error()})
					}
				case "interrupt":
					t, u, ready := awaitTurn()
					if !ready {
						return
					}
					if err := sendRequest("turn/interrupt", map[string]any{"threadId": t, "turnId": u}); err != nil {
						emit(agent.Event{Type: agent.EventError, Error: "Could not interrupt Codex: " + err.Error()})
					}
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
	failure, sid := failureText, sessionID
	smu.Unlock()
	if !ok {
		return agent.TurnResult{Text: agent.FirstNonEmpty(failure, "Codex app-server ended without a result."), SessionID: sid, IsError: failure != ""}, false
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
// requests return nil and the reader sends an explicit unsupported-method RPC error.
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

	case "item/fileChange/requestApproval":
		var p struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		return &agent.Interaction{
			ID: id, Kind: "approval", Title: "Apply file changes?", Detail: p.Reason,
			Options: allowDeny, NeedsResponse: true,
			Meta: map[string]any{"method": msg.Method},
		}, approval{rawID: msg.ID, family: famV2Decision}
	case "item/permissions/requestApproval":
		var p struct {
			Reason      string          `json:"reason"`
			Permissions json.RawMessage `json:"permissions"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		return &agent.Interaction{ID: id, Kind: "approval", Title: "Allow additional access?", Detail: p.Reason,
				Options: allowDeny, NeedsResponse: true, Meta: map[string]any{"method": msg.Method, "permissions": p.Permissions}},
			approval{rawID: msg.ID, family: famPermissions, permissions: p.Permissions}

	case "item/tool/requestUserInput":
		prompts := questionPrompts(msg)
		if len(prompts) > 0 {
			return prompts[0].interaction, prompts[0]
		}
		return nil, approval{}
	case "mcpServer/elicitation/request":
		var p struct {
			Mode    string `json:"mode"`
			Message string `json:"message"`
			URL     string `json:"url"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if p.Mode != "url" {
			return nil, approval{}
		}
		return &agent.Interaction{ID: id, Kind: "approval", Title: p.Message, Detail: p.URL,
			Options: allowDeny, NeedsResponse: true}, approval{rawID: msg.ID, family: famElicitation}

	default:
		return nil, approval{}
	}
}

func serverRequestPrompts(msg rpcIn) []approval {
	if msg.Method == "item/tool/requestUserInput" {
		return questionPrompts(msg)
	}
	inter, ap := serverRequestInteraction(msg)
	if inter == nil {
		return nil
	}
	ap.interaction = inter
	return []approval{ap}
}

func questionPrompts(msg rpcIn) []approval {
	var p struct {
		Questions []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			IsOther  bool   `json:"isOther"`
			IsSecret bool   `json:"isSecret"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(msg.Params, &p) != nil {
		return nil
	}
	prompts := make([]approval, 0, len(p.Questions))
	for _, q := range p.Questions {
		id := string(msg.ID)
		if len(p.Questions) > 1 {
			id += ":" + q.ID
		}
		inter := &agent.Interaction{ID: id, Kind: "input", Title: q.Question, Detail: q.Header, NeedsResponse: true,
			Meta: map[string]any{"method": msg.Method, "allowOther": q.IsOther, "isSecret": q.IsSecret}}
		for _, option := range q.Options {
			inter.Options = append(inter.Options, agent.Action{ID: option.Label, Label: option.Label})
		}
		if len(inter.Options) > 0 {
			inter.Kind = "choice"
		}
		prompts = append(prompts, approval{rawID: msg.ID, family: famAnswers, questionID: q.ID, questionCount: len(p.Questions), interaction: inter})
	}
	return prompts
}

func answerValues(in agent.Inbound) []string {
	if strings.TrimSpace(in.Text) != "" {
		return []string{in.Text}
	}
	if len(in.Options) > 0 {
		return in.Options
	}
	if in.Decision != "" {
		return []string{in.Decision}
	}
	return []string{}
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
		answers := map[string]any{}
		if ap.questionID != "" {
			answers[ap.questionID] = map[string]any{"answers": answerValues(in)}
		}
		return map[string]any{"answers": answers}
	case famPermissions:
		permissions := ap.permissions
		if deny || len(permissions) == 0 || string(permissions) == "null" {
			permissions = json.RawMessage(`{}`)
		}
		return map[string]any{"permissions": permissions, "scope": "turn"}
	case famElicitation:
		if deny {
			return map[string]any{"action": "decline"}
		}
		return map[string]any{"action": "accept"}
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
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	Text          string                 `json:"text"` // agentMessage / plan
	Message       string                 `json:"message"`
	Content       json.RawMessage        `json:"content"` // reasoning; user input uses a different shape
	Summary       json.RawMessage        `json:"summary"`
	Phase         string                 `json:"phase"`
	Command       string                 `json:"command"`          // commandExecution
	Cwd           string                 `json:"cwd"`              // commandExecution (may be absent on app-server turns)
	Aggregated    *string                `json:"aggregatedOutput"` // commandExecution
	ExitCode      *int                   `json:"exitCode"`
	Status        string                 `json:"status"` // inProgress|completed|failed|declined
	Changes       []normChange           `json:"changes"`
	Query         string                 `json:"query"`  // webSearch
	Server        string                 `json:"server"` // mcpToolCall
	Tool          string                 `json:"tool"`   // mcpToolCall / dynamicToolCall
	Result        json.RawMessage        `json:"result"` // mcpToolCall
	Error         json.RawMessage        `json:"error"`
	Arguments     json.RawMessage        `json:"arguments"`
	ContentItems  json.RawMessage        `json:"contentItems"`
	Success       *bool                  `json:"success"`
	Path          string                 `json:"path"`
	SavedPath     string                 `json:"savedPath"`
	Failure       json.RawMessage        `json:"failure"`
	Review        string                 `json:"review"`
	Name          string                 `json:"name"`
	Output        json.RawMessage        `json:"output"`
	Action        *webAction             `json:"action"`
	Results       json.RawMessage        `json:"results"`
	Prompt        string                 `json:"prompt"`
	Agents        map[string]collabState `json:"agentsStates"`
	AgentPath     string                 `json:"agentPath"`
	AgentThreadID string                 `json:"agentThreadId"`
	ActivityKind  string                 `json:"kind"`
	DurationMS    float64                `json:"durationMs"`
	Questions     []asyncQuestion        `json:"questions"`
}

// emitItem maps one item (at started/updated/completed) to events, returning the item's text when it is
// a completed agent message (the running final-answer text). It normalizes the app-server item into
// normItem and shares the streaming emit tail with the exec transport (normalize.go): text/thinking
// stream as Delta=true suffixes across phases, so nothing is double-counted.
func emitItem(phase itemPhase, raw json.RawMessage, emit agent.Emit, st *streamState) string {
	var it asItem
	if err := json.Unmarshal(raw, &it); err != nil {
		st.decodeFailure("Could not decode Codex item: "+err.Error(), emit)
		return ""
	}
	if it.Type == "" {
		return ""
	}
	// Old servers sometimes repeat the final message without an id inside the turn envelope.
	if it.ID == "" && it.Type == "agentMessage" && it.Text == st.finalText {
		return st.finalText
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
		TurnID      string `json:"turnId"`
		Explanation string `json:"explanation"`
		Plan        []struct {
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
	inter := buildTodos(rows)
	if inter != nil {
		inter.ID = p.TurnID + ":plan"
		inter.Detail = p.Explanation
	}
	return inter
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
				CacheWriteInputTokens int `json:"cacheWriteInputTokens"`
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
		CacheWriteInputTokens: t.CacheWriteInputTokens,
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
		ID     string            `json:"id"`
		Status string            `json:"status"` // completed|interrupted|failed|inProgress
		Error  json.RawMessage   `json:"error"`
		Items  []json.RawMessage `json:"items"`
	} `json:"turn"`
}

func terminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "interrupted"
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
	final := false
	for _, raw := range te.Turn.Items {
		var it asItem
		if json.Unmarshal(raw, &it) == nil && it.Type == "agentMessage" && strings.TrimSpace(it.Text) != "" && (it.Phase == "final_answer" || !final) {
			text = it.Text
			final = it.Phase == "final_answer"
		}
	}
	isErr := te.Turn.Status == "failed" || te.Turn.Status == "interrupted"
	if isErr {
		fallback := "Codex turn failed."
		if te.Turn.Status == "interrupted" {
			fallback = "Codex turn interrupted."
		}
		text = errorText(te.Turn.Error, fallback)
	}
	return text, isErr
}
