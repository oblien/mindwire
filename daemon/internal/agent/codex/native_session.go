package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

var _ agent.NativeSessionModule = adapter{}

type nativeThread struct {
	ID                   string `json:"id"`
	CWD                  string `json:"cwd"`
	ParentThreadID       string `json:"parentThreadId"`
	CanAcceptDirectInput *bool  `json:"canAcceptDirectInput"`
	Status               struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	} `json:"status"`
}
type nativeTurn struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Items     []json.RawMessage `json:"items"`
	StartedAt int64             `json:"startedAt"`
	Error     json.RawMessage   `json:"error"`
}

func readNativeThread(ctx context.Context, c *sharedRPC, q agent.NativeSessionQuery) (nativeThread, error) {
	var result struct {
		Thread nativeThread `json:"thread"`
	}
	err := c.call(ctx, "thread/read", map[string]any{"threadId": q.SessionID, "includeTurns": false}, &result)
	if err != nil {
		return result.Thread, err
	}
	return result.Thread, validateNativeThread(q, result.Thread)
}

func validateNativeThread(q agent.NativeSessionQuery, thread nativeThread) error {
	actual, err := workspacepath.Canonical(thread.CWD)
	if err != nil {
		return err
	}
	expected, err := workspacepath.Canonical(q.CWD)
	if err != nil {
		return err
	}
	if thread.ID != q.SessionID || actual != expected {
		return agent.NativeError("native_session_mismatch", "The running conversation belongs to a different project.")
	}
	return nil
}

func nativeActivity(q agent.NativeSessionQuery, thread nativeThread) agent.SessionActivity {
	a := agent.SessionActivity{SessionID: q.SessionID, CWD: thread.CWD, Owner: "native", State: "idle", Capabilities: agent.NativeSessionCapabilities{Status: true, Observe: true, Input: true, Interrupt: true, Respond: true}}
	if thread.CanAcceptDirectInput != nil && !*thread.CanAcceptDirectInput || thread.CanAcceptDirectInput == nil && thread.ParentThreadID != "" {
		a.Capabilities.Input = false
		a.Capabilities.Interrupt = false
		a.Capabilities.Respond = false
		a.Reason = "parent_owned_session"
	}
	switch thread.Status.Type {
	case "active":
		a.State = "running"
		if len(thread.Status.ActiveFlags) > 0 {
			a.State = "waiting"
		}
	case "idle":
	case "systemError":
		a.State = "stopped"
	default:
		a.Owner = "unknown"
		a.State = "unavailable"
		a.Reason = "not_in_shared_server"
		a.Capabilities = agent.NativeSessionCapabilities{Status: true}
	}
	return a
}

func (adapter) SessionActivity(ctx context.Context, q agent.NativeSessionQuery) (agent.SessionActivity, error) {
	if err := agent.ValidateNativeSessionQuery(q); err != nil {
		return agent.SessionActivity{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := dialSharedRPC(ctx)
	if errors.Is(err, errNoSharedServer) {
		return agent.NativeSessionUnavailable(q, "shared_server_unavailable"), nil
	}
	if err != nil {
		return agent.SessionActivity{}, err
	}
	defer c.close()
	thread, err := readNativeThread(ctx, c, q)
	if err != nil {
		var native *agent.NativeSessionError
		if errors.As(err, &native) && native.Code == "native_session_not_found" {
			return agent.NativeSessionUnavailable(q, "not_in_shared_server"), nil
		}
		return agent.SessionActivity{}, err
	}
	return nativeActivity(q, thread), nil
}

type nativeSession struct {
	rpc          *sharedRPC
	q            agent.NativeSessionQuery
	initial      agent.NativeSessionInitial
	updates      chan agent.NativeSessionUpdate
	mu           sync.Mutex
	activity     agent.SessionActivity
	pending      map[string]approval
	answering    map[string]bool
	actionMu     sync.Mutex
	state        *streamState // only the notification reader mutates item state
	turnID       string
	seedSequence uint64
	stateBytes   int
}

func (adapter) OpenNativeSession(ctx context.Context, q agent.NativeSessionQuery) (agent.NativeSessionConnection, error) {
	if err := agent.ValidateNativeSessionQuery(q); err != nil {
		return nil, err
	}
	c, err := dialSharedRPC(ctx)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			c.close()
		}
	}()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	thread, err := readNativeThread(startup, c, q)
	if err != nil {
		return nil, err
	}
	activity := nativeActivity(q, thread)
	if !activity.Capabilities.Observe {
		return nil, agent.NativeError("native_attach_unavailable", "This conversation is not loaded in Codex's shared server. Start Codex with its shared server to enable live attachment.")
	}
	// The native listener orders this recent-turn snapshot and subscription
	// together. Never hydrate the entire history or override the PC's settings.
	var resumed struct {
		Thread           nativeThread `json:"thread"`
		InitialTurnsPage *struct {
			Data []nativeTurn `json:"data"`
		} `json:"initialTurnsPage"`
	}
	sequence, err := c.callAt(startup, "thread/resume", map[string]any{"threadId": q.SessionID, "excludeTurns": true, "initialTurnsPage": map[string]any{"limit": 1, "sortDirection": "desc", "itemsView": "full"}}, &resumed)
	if err != nil {
		return nil, err
	}
	if resumed.InitialTurnsPage == nil {
		return nil, agent.NativeError("native_attach_update_required", "Update Codex to a version that supports a recent-turn snapshot when attaching.")
	}
	if err := validateNativeThread(q, resumed.Thread); err != nil {
		return nil, err
	}
	s := &nativeSession{rpc: c, q: q, updates: make(chan agent.NativeSessionUpdate, 256), pending: map[string]approval{}, answering: map[string]bool{}, state: newStreamState(), activity: nativeActivity(q, resumed.Thread), seedSequence: sequence}
	s.state.cwd = q.CWD
	seed := func(ev agent.Event) {
		if ev.At == "" {
			ev.At = time.Now().UTC().Format(time.RFC3339Nano)
		}
		s.initial.Events = append(s.initial.Events, ev)
	}
	if len(resumed.InitialTurnsPage.Data) > 0 {
		turn := resumed.InitialTurnsPage.Data[0]
		s.turnID = turn.ID
		if !terminalStatus(turn.Status) {
			s.activity.TurnID = turn.ID
			if turn.StartedAt > 0 {
				s.activity.StartedAt = time.Unix(turn.StartedAt, 0).UTC().Format(time.RFC3339Nano)
			}
		}
		// Even a single native turn can be very large. Keep a recent item window;
		// clients can obtain the omitted part through normal paginated history.
		start, size := len(turn.Items), 0
		for start > 0 && size+len(turn.Items[start-1]) <= 2<<20 {
			start--
			size += len(turn.Items[start])
		}
		s.initial.HistoryRequired = start != 0
		s.initial.TurnID = turn.ID
		s.stateBytes = size
		for _, item := range turn.Items[start:] {
			s.seedItem(item, terminalStatus(turn.Status), seed)
		}
		if terminalStatus(turn.Status) {
			seed(s.result(turn))
		}
	}
	s.initial.Activity = s.activity
	failed = false
	go s.read()
	return s, nil
}

func (s *nativeSession) Initial() agent.NativeSessionInitial       { return s.initial }
func (s *nativeSession) Updates() <-chan agent.NativeSessionUpdate { return s.updates }
func (s *nativeSession) Close()                                    { s.rpc.close() }

func (s *nativeSession) publish(u agent.NativeSessionUpdate) bool {
	select {
	case s.updates <- u:
		return true
	case <-s.rpc.ctx.Done():
		return false
	default:
		s.Close()
		return false
	}
}

func (s *nativeSession) emit(ev agent.Event) {
	if ev.At == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	s.publish(agent.NativeSessionUpdate{Event: &ev, TurnID: s.turnID})
}

func (s *nativeSession) seedItem(raw json.RawMessage, completed bool, emit agent.Emit) {
	var item asItem
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	if item.Type == "userMessage" {
		ev := nativeUserItem(item)
		var identity struct {
			ClientID string `json:"clientId"`
		}
		_ = json.Unmarshal(raw, &identity)
		if identity.ClientID != "" {
			ev.Input.ID = identity.ClientID
		}
		emit(ev)
		return
	}
	phase := phaseUpdated
	if completed || item.Status == "completed" {
		phase = phaseCompleted
	}
	emitItem(phase, raw, emit, s.state)
}

func nativeUserItem(item asItem) agent.Event {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Path string `json:"path"`
		URL  string `json:"url"`
	}
	_ = json.Unmarshal(item.Content, &blocks)
	var text []string
	var attachments []agent.Attachment
	for _, b := range blocks {
		if b.Type == "text" {
			text = append(text, b.Text)
		} else if b.Path != "" {
			attachments = append(attachments, agent.Attachment{Path: b.Path})
		}
	}
	return agent.Event{Type: agent.EventInput, ItemID: item.ID, Input: &agent.UserInput{ID: item.ID, Text: strings.Join(text, "\n"), Attachments: attachments, Status: "accepted"}}
}

func (s *nativeSession) result(turn nativeTurn) agent.Event {
	text := s.state.finalText
	isError := turn.Status == "failed"
	if isError {
		text = errorText(turn.Error, "Codex turn failed.")
	}
	return agent.Event{Type: agent.EventResult, SessionID: s.q.SessionID, Result: &agent.ResultInfo{Text: text, SessionID: s.q.SessionID, IsError: isError}, Meta: map[string]any{"nativeTurnStatus": turn.Status}}
}

func (s *nativeSession) read() {
	defer close(s.updates)
	defer s.Close()
	for msg := range s.rpc.frames {
		// Global notices received before the atomic resume response describe an
		// older state. Pending questions are replayed by Codex after this barrier.
		if msg.sequence <= s.seedSequence {
			continue
		}
		var route struct {
			ThreadID       string     `json:"threadId"`
			ConversationID string     `json:"conversationId"`
			TurnID         string     `json:"turnId"`
			Turn           nativeTurn `json:"turn"`
		}
		_ = json.Unmarshal(msg.Params, &route)
		tid := agent.FirstNonEmpty(route.ThreadID, route.ConversationID)
		if tid != "" && tid != s.q.SessionID {
			continue
		}
		if msg.Method != "" && len(msg.ID) > 0 {
			if tid != s.q.SessionID {
				continue
			}
			for _, prompt := range serverRequestPrompts(msg.rpcIn) {
				s.mu.Lock()
				s.pending[prompt.interaction.ID] = prompt
				s.mu.Unlock()
				s.emit(agent.Event{Type: agent.EventInteraction, Interaction: prompt.interaction})
			}
			continue
		}
		if tid != s.q.SessionID {
			continue
		}
		if route.TurnID != "" && s.turnID != "" && route.TurnID != s.turnID {
			continue
		}
		// Decoder state is a recent-turn view, not another transcript database.
		// Rebase to native history if an exceptionally long turn exceeds its cap.
		s.stateBytes += len(msg.Params)
		if s.stateBytes > 8<<20 {
			s.state = newStreamState()
			s.state.cwd = s.q.CWD
			s.stateBytes = len(msg.Params)
			s.publish(agent.NativeSessionUpdate{TurnID: s.turnID, Reset: true, HistoryChanged: true, Partial: true})
		}
		if len(msg.Params) > 8<<20 {
			if msg.Method != "turn/completed" {
				continue
			}
			route.Turn.Items = nil
		}
		switch msg.Method {
		case "turn/started":
			if route.Turn.ID == "" || route.Turn.ID == s.turnID {
				continue
			}
			s.turnID = route.Turn.ID
			s.state = newStreamState()
			s.state.cwd = s.q.CWD
			s.stateBytes = 0
			s.mu.Lock()
			s.activity.State = "running"
			s.activity.TurnID = s.turnID
			s.activity.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
			a := s.activity
			s.mu.Unlock()
			s.publish(agent.NativeSessionUpdate{Activity: &a, TurnID: s.turnID, Reset: true})
		case "thread/status/changed":
			var p struct {
				Status json.RawMessage `json:"status"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			var thread nativeThread
			_ = json.Unmarshal(p.Status, &thread.Status)
			s.mu.Lock()
			a := nativeActivity(s.q, thread)
			s.activity.State = a.State
			s.activity.Owner = a.Owner
			if a.State != "running" && a.State != "waiting" {
				s.activity.TurnID = ""
			}
			a = s.activity
			s.mu.Unlock()
			s.publish(agent.NativeSessionUpdate{Activity: &a, TurnID: s.turnID})
		case "item/started", "item/updated", "item/completed":
			var p struct {
				Item json.RawMessage `json:"item"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if route.TurnID != "" && s.turnID != "" && route.TurnID != s.turnID {
				continue
			}
			s.seedItem(p.Item, msg.Method == "item/completed", s.emit)
		case "turn/completed":
			if route.Turn.ID != s.turnID {
				continue
			}
			for _, item := range route.Turn.Items {
				s.seedItem(item, true, s.emit)
			}
			s.emit(s.result(route.Turn))
			s.mu.Lock()
			s.activity.TurnID = ""
			s.activity.State = "idle"
			s.activity.StartedAt = ""
			a := s.activity
			for id, p := range s.pending {
				if !p.interaction.IsMessageQuestion() {
					delete(s.pending, id)
					delete(s.answering, id)
				}
			}
			s.mu.Unlock()
			s.publish(agent.NativeSessionUpdate{Activity: &a, TurnID: s.turnID, HistoryChanged: true})
		case "serverRequest/resolved":
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			s.mu.Lock()
			var resolved []*agent.Interaction
			for id, ap := range s.pending {
				if string(ap.rawID) == string(p.RequestID) {
					it := *ap.interaction
					it.NeedsResponse = false
					resolved = append(resolved, &it)
					delete(s.pending, id)
					delete(s.answering, id)
				}
			}
			s.mu.Unlock()
			for _, it := range resolved {
				s.emit(agent.Event{Type: agent.EventInteraction, Interaction: it})
			}
		case "thread/closed":
			return
		case "turn/plan/updated":
			if it := planInteraction(msg.Params); it != nil {
				s.emit(agent.Event{Type: agent.EventInteraction, Interaction: it})
			}
		case "item/agentMessage/delta", "item/plan/delta", "item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded", "item/reasoning/textDelta", "item/commandExecution/outputDelta", "item/fileChange/patchUpdated", "item/fileChange/outputDelta", "item/mcpToolCall/progress":
			if route.TurnID == "" || route.TurnID == s.turnID {
				s.state.emitDelta(msg.Method, msg.Params, s.emit)
			}
		case "turn/diff/updated":
			var p struct {
				Diff string `json:"diff"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			s.state.turnDiff = p.Diff
			s.state.emitTurnDiff(s.turnID, s.emit)
		case "thread/tokenUsage/updated":
			if u, ok := tokenUsageFrom(msg.Params); ok {
				s.emit(agent.Event{Type: agent.EventStatus, Meta: usageMeta(u)})
			}
		case "error":
			var p struct {
				Error     json.RawMessage `json:"error"`
				WillRetry bool            `json:"willRetry"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if p.WillRetry {
				s.emit(agent.Event{Type: agent.EventStatus, Meta: map[string]any{"willRetry": true, "message": errorText(p.Error, "Codex is reconnecting.")}})
			} else {
				s.emit(agent.Event{Type: agent.EventError, Error: errorText(p.Error, "Codex reported an error.")})
			}
		default:
			s.state.emitNotice(msg.Method, msg.Params, s.emit)
		}
	}
}

func (s *nativeSession) Action(ctx context.Context, action agent.NativeSessionAction) (agent.NativeSessionReceipt, error) {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	s.mu.Lock()
	activity := s.activity
	s.mu.Unlock()
	result := agent.NativeSessionReceipt{RequestID: action.RequestID, Status: "accepted"}
	switch action.Kind {
	case "input":
		if !activity.Capabilities.Input {
			return result, agent.NativeActionUnsupported("sending")
		}
		input, err := nativeInput(action)
		if err != nil {
			return result, err
		}
		params := map[string]any{"threadId": s.q.SessionID, "input": input, "clientUserMessageId": action.RequestID}
		if action.ExpectedTurnID != "" {
			params["expectedTurnId"] = action.ExpectedTurnID
			var ack struct {
				TurnID string `json:"turnId"`
			}
			if err = s.rpc.call(ctx, "turn/steer", params, &ack); err != nil {
				return result, err
			}
			result.TurnID = ack.TurnID
		} else {
			// Native start_or_steer owns idle/active admission atomically; there is
			// no check-then-start race or daemon emulation of the harness's queue.
			var ack struct {
				Turn nativeTurn `json:"turn"`
			}
			if err = s.rpc.call(ctx, "turn/start", params, &ack); err != nil {
				return result, err
			}
			result.TurnID = ack.Turn.ID
		}
		if result.TurnID == "" {
			return result, agent.NativeError("native_action_unknown", "Codex did not identify the accepted turn. Check the conversation before sending again.")
		}
	case "interrupt":
		if !activity.Capabilities.Interrupt {
			return result, agent.NativeActionUnsupported("stopping")
		}
		if action.ExpectedTurnID == "" {
			return result, agent.NativeError("native_turn_required", "Specify the turn being stopped.")
		}
		if err := s.rpc.call(ctx, "turn/interrupt", map[string]any{"threadId": s.q.SessionID, "turnId": action.ExpectedTurnID}, nil); err != nil {
			return result, err
		}
		result.TurnID = action.ExpectedTurnID
	case "response":
		if !activity.Capabilities.Respond {
			return result, agent.NativeActionUnsupported("answering")
		}
		if action.Response == nil {
			return result, agent.NativeError("native_action_invalid", "An interaction response is required.")
		}
		s.mu.Lock()
		ap, ok := s.pending[action.Response.InteractionID]
		answering := s.answering[action.Response.InteractionID]
		s.mu.Unlock()
		if !ok || answering {
			return result, agent.NativeError("native_interaction_expired", "That question was already answered or is no longer pending.")
		}
		reply, err := ap.interaction.ValidateResponse(*action.Response)
		if err != nil {
			return result, agent.NativeError("native_response_invalid", err.Error())
		}
		in := agent.Inbound{InteractionID: reply.InteractionID, Decision: reply.Decision, Options: reply.Options, Text: reply.Text, Answers: reply.Answers}
		if ap.interaction.Feedback == "always" && reply.Text != "" {
			if activity.TurnID == "" {
				return result, agent.NativeError("native_turn_changed", "The turn has ended. Check its current state before replying.")
			}
			if err = s.rpc.call(ctx, "turn/steer", map[string]any{"threadId": s.q.SessionID, "expectedTurnId": activity.TurnID, "input": []any{map[string]any{"type": "text", "text": "Feedback on " + ap.interaction.Title + ":\n" + reply.Text}}}, nil); err != nil {
				return result, err
			}
		}
		s.mu.Lock()
		s.answering[action.Response.InteractionID] = true
		s.mu.Unlock()
		if err = s.rpc.write(ctx, rpcResponse{ID: ap.rawID, Result: decisionResult(ap, in)}); err != nil {
			return result, err
		}
		// JSON-RPC replies have no response acknowledgement. Keep the question
		// pending until the shared server broadcasts serverRequest/resolved.
		result.Status = "submitted"
		result.TurnID = activity.TurnID
	default:
		return result, agent.NativeActionUnsupported(action.Kind)
	}
	return result, nil
}

func nativeInput(action agent.NativeSessionAction) ([]any, error) {
	input := []any{}
	text := strings.TrimSpace(action.Text)
	var files []agent.Attachment
	for _, a := range action.Attachments {
		if mime := agent.NativeImageMime(a); mime != "" {
			if len(a.Data) > 0 {
				input = append(input, map[string]any{"type": "image", "url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(a.Data)})
			} else if a.Path != "" {
				input = append(input, map[string]any{"type": "localImage", "path": a.Path})
			} else {
				return nil, agent.NativeError("native_attachment_invalid", "Upload the image before sending it.")
			}
		} else {
			if a.Path == "" || len(a.Data) > 0 {
				return nil, agent.NativeError("native_attachment_invalid", "Upload files before sending them to a running conversation.")
			}
			files = append(files, a)
		}
	}
	if len(files) > 0 {
		text = appServer{message: text, files: files}.inputText()
	}
	if text != "" {
		input = append([]any{map[string]any{"type": "text", "text": text}}, input...)
	}
	if len(input) == 0 {
		return nil, agent.NativeError("native_action_invalid", "A message or attachment is required.")
	}
	return input, nil
}
