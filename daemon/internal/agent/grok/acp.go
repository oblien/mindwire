package grok

// ACP transport for Grok Build. The public CLI documentation defines this as the
// integration transport (grok agent [options] stdio), including session loading,
// structured tool updates, cancellation, and permission requests. Keep all wire
// structs local and permissive: ACP extensions evolve independently of MindWire.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type acpClient struct {
	in      io.WriteCloser
	mu      sync.Mutex
	next    atomic.Int64
	pending sync.Map // map[string]chan rpcMessage
	notify  chan rpcMessage
	request chan rpcMessage
	closed  chan struct{}
}

func rpcKey(id json.RawMessage) string { return string(bytes.TrimSpace(id)) }

func newACPClient(in io.WriteCloser, out io.Reader) *acpClient {
	c := &acpClient{in: in, notify: make(chan rpcMessage, 128), request: make(chan rpcMessage, 16), closed: make(chan struct{})}
	go func() {
		defer close(c.closed)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
		for sc.Scan() {
			var m rpcMessage
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			if len(m.ID) > 0 && (len(m.Result) > 0 || m.Error != nil) {
				if ch, ok := c.pending.LoadAndDelete(rpcKey(m.ID)); ok {
					ch.(chan rpcMessage) <- m
				}
				continue
			}
			if len(m.ID) > 0 && m.Method != "" {
				c.request <- m
				continue
			}
			if m.Method != "" {
				c.notify <- m
			}
		}
		close(c.notify)
		close(c.request)
	}()
	return c
}

func (c *acpClient) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.in.Write(append(b, '\n'))
	return err
}

func (c *acpClient) requestTo(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.next.Add(1)
	key := strconv.FormatInt(id, 10)
	ch := make(chan rpcMessage, 1)
	c.pending.Store(key, ch)
	defer c.pending.Delete(key)
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	decode := func(m rpcMessage) (json.RawMessage, error) {
		if m.Error != nil {
			return nil, fmt.Errorf("grok ACP %s: %s", method, m.Error.Message)
		}
		return m.Result, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case m := <-ch:
		return decode(m)
	case <-c.closed:
		// A response can be decoded immediately before stdout reaches EOF. Prefer
		// that queued reply over the close signal; otherwise a fast ACP process
		// flakes between its terminal response and process exit.
		select {
		case m := <-ch:
			return decode(m)
		default:
			return nil, fmt.Errorf("grok ACP closed while waiting for %s", method)
		}
	}
}

func (c *acpClient) respond(id json.RawMessage, result any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func (c *acpClient) notifyTo(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *acpClient) close() { _ = c.in.Close() }

func acpCommand(in agent.TurnInput) []string {
	// Disable background self-updates in every programmatic run. They create
	// nondeterministic CI/daemon behavior and Grok documents this flag for ACP.
	args := []string{"--no-auto-update", "agent"}
	if v := strings.TrimSpace(in.Config[keyModel]); v != "" {
		args = append(args, "--model", v)
	}
	// The default MindWire execution mode is autonomous. "ask" deliberately
	// removes this flag and surfaces ACP session/request_permission interactions.
	if !strings.EqualFold(strings.TrimSpace(in.Config[keyPermission]), "ask") {
		args = append(args, "--always-approve")
	}
	return append(args, "stdio")
}

func runACP(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	if err := validateACPInput(in); err != nil {
		return agent.TurnResult{SessionID: agent.FirstNonEmpty(in.Options.SessionID, in.SessionID), Text: err.Error(), IsError: true}, err
	}
	cmd := exec.CommandContext(ctx, "grok", acpCommand(in)...)
	if in.CWD != "" {
		cmd.Dir = in.CWD
	}
	cmd.Env = os.Environ()
	for k, v := range in.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	proc.Group(cmd)
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
	if err = cmd.Start(); err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	proc.Report(ctx, cmd.Process.Pid)
	c := newACPClient(stdin, stdout)
	defer c.close()

	result, runErr := acpRun(ctx, c, in, emit)
	_ = stdin.Close()
	waitErr := cmd.Wait()
	if runErr != nil {
		msg := runErr.Error()
		if s := strings.TrimSpace(stderr.String()); s != "" {
			msg += ": " + s
		}
		emit(agent.Event{Type: agent.EventError, Error: msg})
		return agent.TurnResult{Text: msg, IsError: true, SessionID: result.SessionID}, runErr
	}
	if waitErr != nil && !result.IsError {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		emit(agent.Event{Type: agent.EventError, Error: msg})
		return agent.TurnResult{Text: msg, IsError: true, SessionID: result.SessionID}, waitErr
	}
	return result, nil
}

func acpRun(ctx context.Context, c *acpClient, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	if err := validateACPInput(in); err != nil {
		return agent.TurnResult{SessionID: agent.FirstNonEmpty(in.Options.SessionID, in.SessionID)}, err
	}
	sessionID := agent.FirstNonEmpty(strings.TrimSpace(in.Options.SessionID), strings.TrimSpace(in.SessionID))
	initRaw, err := c.requestTo(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false}})
	if err != nil {
		return agent.TurnResult{}, err
	}
	var init struct {
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
		DefaultAuthMethodID string `json:"defaultAuthMethodId"`
	}
	_ = json.Unmarshal(initRaw, &init)
	method := ""
	for _, m := range init.AuthMethods {
		if m.ID == "xai.api_key" && strings.TrimSpace(in.Env["XAI_API_KEY"]) != "" {
			method = m.ID
			break
		}
	}
	if method == "" {
		method = init.DefaultAuthMethodID
	}
	if method != "" {
		if _, err := c.requestTo(ctx, "authenticate", map[string]any{"methodId": method, "_meta": map[string]bool{"headless": true}}); err != nil {
			return agent.TurnResult{}, err
		}
	}

	if in.Options.ContinueLatest {
		raw, err := c.requestTo(ctx, "session/resume", map[string]any{"cwd": agent.FirstNonEmpty(in.CWD, ".")})
		if err != nil {
			return agent.TurnResult{}, err
		}
		var resumed struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(raw, &resumed)
		sessionID = resumed.SessionID
		if sessionID == "" {
			return agent.TurnResult{}, fmt.Errorf("grok ACP session/resume returned no sessionId")
		}
	} else if sessionID != "" {
		if _, err := c.requestTo(ctx, "session/load", map[string]any{"sessionId": sessionID}); err != nil {
			return agent.TurnResult{SessionID: sessionID}, err
		}
	} else {
		meta := map[string]any{}
		if v := strings.TrimSpace(in.Config[keyRules]); v != "" {
			meta["rules"] = v
		}
		if v := agent.FirstNonEmpty(strings.TrimSpace(in.Options.SystemPrompt), strings.TrimSpace(in.Config[keySystemPrompt])); v != "" {
			meta["systemPromptOverride"] = v
		}
		if strings.EqualFold(strings.TrimSpace(in.Config[keyPermission]), "ask") {
			meta["autoMode"] = false
		} else {
			meta["yoloMode"] = true
		}
		params := map[string]any{"cwd": agent.FirstNonEmpty(in.CWD, "."), "mcpServers": json.RawMessage(`[]`), "_meta": meta}
		if len(in.Options.MCPServers) > 0 {
			params["mcpServers"] = in.Options.MCPServers
		}
		raw, err := c.requestTo(ctx, "session/new", params)
		if err != nil {
			return agent.TurnResult{}, err
		}
		var created struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(raw, &created)
		sessionID = created.SessionID
		if sessionID == "" {
			return agent.TurnResult{}, fmt.Errorf("grok ACP session/new returned no sessionId")
		}
		emit(agent.Event{Type: agent.EventSession, SessionID: sessionID})
	}
	if in.Options.ForkOnResume {
		raw, err := c.requestTo(ctx, "session/fork", map[string]any{"sessionId": sessionID})
		if err != nil {
			return agent.TurnResult{SessionID: sessionID}, err
		}
		var forked struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(raw, &forked)
		if forked.SessionID == "" {
			return agent.TurnResult{SessionID: sessionID}, fmt.Errorf("grok ACP session/fork returned no sessionId")
		}
		sessionID = forked.SessionID
		emit(agent.Event{Type: agent.EventSession, SessionID: sessionID, Meta: map[string]any{"forked": true}})
	}

	promptDone := make(chan struct {
		raw json.RawMessage
		err error
	}, 1)
	go func() {
		raw, err := c.requestTo(ctx, "session/prompt", map[string]any{"sessionId": sessionID, "prompt": []map[string]string{{"type": "text", "text": in.Message}}})
		promptDone <- struct {
			raw json.RawMessage
			err error
		}{raw, err}
	}()
	text := ""
	pendingPermission := map[string]json.RawMessage{}
	notifications := c.notify
	requests := c.request
	for {
		select {
		case <-ctx.Done():
			for _, id := range pendingPermission {
				_ = c.respond(id, map[string]any{"outcome": map[string]string{"outcome": "cancelled"}})
			}
			_ = c.notifyTo("session/cancel", map[string]any{"sessionId": sessionID})
			return agent.TurnResult{SessionID: sessionID, IsError: true, Text: ctx.Err().Error()}, ctx.Err()
		case done := <-promptDone:
			if done.err != nil {
				return agent.TurnResult{SessionID: sessionID}, done.err
			}
			// JSON-RPC preserves stdout order, but select does not preserve channel
			// priority: the terminal prompt response could win over updates that the
			// reader already queued. Drain those earlier updates before materializing
			// the final text, otherwise a fast agent can finish with an empty result.
			for {
				select {
				case n, ok := <-c.notify:
					if !ok {
						goto settled
					}
					if n.Method == "session/update" || n.Method == "x.ai/session/update" {
						text = handleACPUpdate(n.Params, sessionID, text, emit)
					}
				default:
					goto settled
				}
			}
		settled:
			result := agent.TurnResult{Text: text, SessionID: sessionID}
			emit(agent.Event{Type: agent.EventResult, SessionID: sessionID, Result: &agent.ResultInfo{Text: text, SessionID: sessionID}})
			return result, nil
		case n, ok := <-notifications:
			if !ok {
				// stdout may close just after the terminal prompt reply. The reply is
				// already queued in promptDone, so disable this case and let it win.
				notifications = nil
				continue
			}
			if n.Method == "session/update" || n.Method == "x.ai/session/update" {
				text = handleACPUpdate(n.Params, sessionID, text, emit)
			}
		case req, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			if req.Method != "session/request_permission" {
				_ = c.respond(req.ID, map[string]any{})
				continue
			}
			inter := permissionInteraction(req)
			pendingPermission[inter.ID] = req.ID
			emit(agent.Event{Type: agent.EventInteraction, SessionID: sessionID, Interaction: inter})
		case input, ok := <-in.Inbound:
			if !ok {
				in.Inbound = nil
				continue
			}
			if input.Kind == "interrupt" {
				_ = c.notifyTo("session/cancel", map[string]any{"sessionId": sessionID})
				continue
			}
			if input.Kind != "response" {
				continue
			}
			id, found := pendingPermission[input.InteractionID]
			if !found {
				continue
			}
			delete(pendingPermission, input.InteractionID)
			outcome := map[string]any{"outcome": "cancelled"}
			if input.Decision != "" && input.Decision != "deny" && input.Decision != "reject" {
				outcome = map[string]any{"outcome": "selected", "optionId": input.Decision}
			}
			_ = c.respond(id, map[string]any{"outcome": outcome})
		}
	}
}

func validateACPInput(in agent.TurnInput) error {
	sessionID := agent.FirstNonEmpty(strings.TrimSpace(in.Options.SessionID), strings.TrimSpace(in.SessionID))
	// ACP supplies these parameters only on session/new. Never pretend that an
	// override was applied to an existing native session when Grok cannot receive
	// it there; persistent MCP configuration remains available for that case.
	newSessionOnly := len(in.Options.MCPServers) > 0 ||
		strings.TrimSpace(in.Options.SystemPrompt) != "" ||
		strings.TrimSpace(in.Config[keySystemPrompt]) != "" ||
		strings.TrimSpace(in.Config[keyRules]) != ""
	if newSessionOnly && (sessionID != "" || in.Options.ContinueLatest) {
		return fmt.Errorf("Grok Build applies per-turn MCP servers and prompt context only when creating a new session; use persistent MCP config or start a fresh session")
	}
	return nil
}

func permissionInteraction(req rpcMessage) *agent.Interaction {
	var p struct {
		ToolCall struct {
			Title string `json:"title"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
		} `json:"options"`
	}
	_ = json.Unmarshal(req.Params, &p)
	options := make([]agent.Action, 0, len(p.Options))
	for _, o := range p.Options {
		options = append(options, agent.Action{ID: o.OptionID, Label: agent.FirstNonEmpty(o.Name, o.OptionID)})
	}
	return &agent.Interaction{ID: rpcKey(req.ID), Kind: "approval", Title: agent.FirstNonEmpty(p.ToolCall.Title, "Grok Build requests permission"), Options: options, NeedsResponse: true, Meta: map[string]any{"acp": true}}
}

func handleACPUpdate(params json.RawMessage, sessionID, text string, emit agent.Emit) string {
	var p struct {
		Update struct {
			SessionUpdate string          `json:"sessionUpdate"`
			ToolCallID    string          `json:"toolCallId"`
			Title         string          `json:"title"`
			Status        string          `json:"status"`
			RawInput      json.RawMessage `json:"rawInput"`
			Content       json.RawMessage `json:"content"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &p) != nil {
		return text
	}
	u := p.Update
	var content struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(u.Content, &content)
	switch u.SessionUpdate {
	case "agent_message_chunk":
		if content.Text != "" {
			text += content.Text
			emit(agent.Event{Type: agent.EventText, SessionID: sessionID, Text: content.Text, Delta: true})
		}
	case "agent_thought_chunk":
		if content.Text != "" {
			emit(agent.Event{Type: agent.EventThinking, SessionID: sessionID, Text: content.Text, Delta: true})
		}
	case "tool_call":
		emit(agent.Event{Type: agent.EventToolUse, SessionID: sessionID, Tool: &agent.ToolEvent{ID: u.ToolCallID, Name: u.Title, Input: u.RawInput}})
	case "tool_call_update":
		emit(agent.Event{Type: agent.EventToolResult, SessionID: sessionID, Tool: &agent.ToolEvent{ID: u.ToolCallID, Output: content.Text, IsError: strings.EqualFold(u.Status, "failed")}})
	}
	return text
}
