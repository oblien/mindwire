package acp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/proc"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
)

// Flavor contains only provider differences. The shared runtime owns framing,
// process lifetime, sessions, permissions, streaming and terminal outcomes.
type Flavor struct {
	ID, Name, Binary   string
	Args               func(agent.TurnInput) []string
	Validate           func(agent.TurnInput) error
	AuthMethod         func(Initialization, agent.TurnInput) string
	AuthMeta           map[string]any
	NewSessionMeta     func(agent.TurnInput) map[string]any
	ResumeMethod       string
	ResumeLatestMethod string
	Configure          func(context.Context, *Client, Session, agent.TurnInput) error
	Interaction        func(Message) *agent.Interaction
	Reply              func(Message, agent.Inbound) (any, error)
	Update             func(Message, *Stream)
	OnOutput           func(string)
	PromptID           func(json.RawMessage) string
	Completion         func(Message) (string, json.RawMessage)
	PromptMeta         func(agent.TurnInput) map[string]any
	Capabilities       func() agent.Capabilities
}

type Initialization struct {
	ProtocolVersion     int                            `json:"protocolVersion"`
	AgentInfo           struct{ Name, Version string } `json:"agentInfo"`
	AuthMethods         []struct{ ID, Name string }    `json:"authMethods"`
	DefaultAuthMethodID string                         `json:"defaultAuthMethodId"`
	Meta                struct {
		DefaultAuthMethodID string `json:"defaultAuthMethodId"`
		AgentVersion        string `json:"agentVersion"`
	} `json:"_meta"`
	AgentCapabilities struct {
		LoadSession         bool                                         `json:"loadSession"`
		PromptCapabilities  struct{ Image, Audio, EmbeddedContext bool } `json:"promptCapabilities"`
		SessionCapabilities struct{ Resume, Fork json.RawMessage }       `json:"sessionCapabilities"`
		MCPCapabilities     struct {
			HTTP bool `json:"http"`
			SSE  bool `json:"sse"`
		} `json:"mcpCapabilities"`
	} `json:"agentCapabilities"`
}

type Session struct {
	ID     string `json:"sessionId"`
	Models struct {
		Current   string `json:"currentModelId"`
		Available []struct {
			ID   string `json:"modelId"`
			Name string `json:"name"`
		} `json:"availableModels"`
	} `json:"models"`
	Modes struct {
		Current   string                      `json:"currentModeId"`
		Available []struct{ ID, Name string } `json:"availableModes"`
	} `json:"modes"`
	ConfigOptions []ConfigOption `json:"configOptions"`
}

type ConfigOption struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Category     string          `json:"category,omitempty"`
	Type         string          `json:"type"`
	CurrentValue string          `json:"currentValue"`
	Options      json.RawMessage `json:"options"`
}

// Process is one owned native process. Close drains normal termination, then
// kills only this process group if the provider will not stop after stdin EOF.
type Process struct {
	Client *Client
	cmd    *exec.Cmd
	cancel context.CancelFunc
	once   sync.Once
}

func Start(ctx context.Context, f Flavor, in agent.TurnInput) (*Process, error) {
	ctx, cancel := context.WithCancel(ctx)
	var args []string
	if f.Args != nil {
		args = f.Args(in)
	}
	cmd := toolchain.CommandContext(ctx, f.Binary, args...)
	cmd.Dir = in.CWD
	for k, v := range in.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	proc.Group(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		cancel()
		return nil, err
	}
	// Protocol errors provide structured diagnostics; native stderr can contain credentials.
	cmd.Stderr = io.Discard
	if f.OnOutput != nil {
		cmd.Stderr = &lineOutput{output: f.OnOutput}
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		cancel()
		return nil, err
	}
	proc.Report(ctx, cmd.Process.Pid)
	return &Process{Client: NewClientWithOutput(stdin, stdout, f.OnOutput), cmd: cmd, cancel: cancel}, nil
}

func (p *Process) Close() {
	p.once.Do(func() {
		p.Client.Close()
		done := make(chan struct{})
		go func() { _ = p.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			p.cancel()
			<-done
		}
		p.cancel()
	})
}

func Initialize(ctx context.Context, c *Client) (Initialization, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := c.Setup(ctx, "initialize", map[string]any{"protocolVersion": 1,
		"clientInfo":         map[string]string{"name": "mindwire", "version": agent.Version},
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false}})
	var init Initialization
	if err == nil {
		err = json.Unmarshal(raw, &init)
		init.DefaultAuthMethodID = agent.FirstNonEmpty(init.DefaultAuthMethodID, init.Meta.DefaultAuthMethodID)
		init.AgentInfo.Version = agent.FirstNonEmpty(init.AgentInfo.Version, init.Meta.AgentVersion)
	}
	if err == nil && init.ProtocolVersion != 0 && init.ProtocolVersion != 1 {
		err = fmt.Errorf("unsupported ACP protocol version %d", init.ProtocolVersion)
	}
	return init, err
}

func Run(ctx context.Context, f Flavor, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	if f.Validate != nil {
		if err := f.Validate(in); err != nil {
			return agent.TurnResult{IsError: true, Text: err.Error()}, err
		}
	}
	p, err := Start(ctx, f, in)
	if err != nil {
		return agent.TurnResult{IsError: true, Text: err.Error()}, err
	}
	defer p.Close()
	return RunSession(ctx, p.Client, f, in, emit)
}

func RunSession(ctx context.Context, c *Client, f Flavor, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	if f.Validate != nil {
		if err := f.Validate(in); err != nil {
			return agent.TurnResult{}, err
		}
	}
	cwd, err := filepath.Abs(agent.FirstNonEmpty(in.CWD, "."))
	if err != nil {
		return agent.TurnResult{}, err
	}
	if canonical, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = canonical
	}
	init, err := Initialize(ctx, c)
	if err != nil {
		return agent.TurnResult{}, err
	}
	if f.AuthMethod != nil {
		if method := f.AuthMethod(init, in); method != "" {
			if _, err := c.Setup(ctx, "authenticate", map[string]any{"methodId": method, "_meta": f.AuthMeta}); err != nil {
				return agent.TurnResult{}, err
			}
		}
	}
	mcp, err := MCPServers(in.Options.MCPServers, in.Env)
	if err != nil {
		return agent.TurnResult{}, err
	}
	for _, raw := range mcp {
		var server struct{ Type string }
		if err := json.Unmarshal(raw, &server); err != nil {
			return agent.TurnResult{}, err
		}
		if server.Type == "http" && !init.AgentCapabilities.MCPCapabilities.HTTP || server.Type == "sse" && !init.AgentCapabilities.MCPCapabilities.SSE {
			return agent.TurnResult{}, fmt.Errorf("%s does not advertise %s MCP servers", f.Name, server.Type)
		}
	}
	sid := agent.FirstNonEmpty(in.Options.SessionID, in.SessionID)
	method := "session/new"
	params := map[string]any{"cwd": cwd, "mcpServers": mcp}
	if sid != "" {
		method = agent.FirstNonEmpty(f.ResumeMethod, "session/load")
		if method == "session/load" && init.ProtocolVersion != 0 && !init.AgentCapabilities.LoadSession {
			return agent.TurnResult{SessionID: sid}, errors.New(f.Name + " does not advertise session loading")
		}
		params["sessionId"] = sid
	} else if in.Options.ContinueLatest {
		if f.ResumeLatestMethod == "" {
			return agent.TurnResult{}, errors.New("ACP requires an explicit session ID to resume")
		}
		method = f.ResumeLatestMethod
	}
	if method == "session/new" && f.NewSessionMeta != nil {
		params["_meta"] = f.NewSessionMeta(in)
	}
	setupCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	raw, err := c.Setup(setupCtx, method, params)
	cancel()
	if err != nil {
		return agent.TurnResult{SessionID: sid}, err
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return agent.TurnResult{SessionID: sid}, err
	}
	session.ID = agent.FirstNonEmpty(session.ID, sid)
	if value, ok := c.configOptions.Load(session.ID); ok {
		session.ConfigOptions = value.([]ConfigOption)
	}
	if session.ID == "" {
		return agent.TurnResult{}, fmt.Errorf("%s %s returned no sessionId", f.Name, method)
	}
	if f.Configure != nil {
		if err := f.Configure(ctx, c, session, in); err != nil {
			return agent.TurnResult{SessionID: session.ID}, err
		}
	}
	if value, ok := c.configOptions.Load(session.ID); ok {
		session.ConfigOptions = value.([]ConfigOption)
	}
	caps := Capabilities()
	if f.Capabilities != nil {
		caps = f.Capabilities()
	}
	caps.Negotiated, caps.NativeVersion = true, init.AgentInfo.Version
	caps.ImageInput = init.AgentCapabilities.PromptCapabilities.Image
	caps.Resume = init.AgentCapabilities.LoadSession || len(init.AgentCapabilities.SessionCapabilities.Resume) > 0
	if caps.SetModel {
		caps.SetModel = len(session.Models.Available) > 0
		for _, o := range session.ConfigOptions {
			caps.SetModel = caps.SetModel || o.Category == "model" || o.ID == "model"
		}
	}
	initialCaps := caps
	emit(agent.Event{Type: agent.EventSession, SessionID: session.ID, Capabilities: &initialCaps, Meta: map[string]any{"protocol": "acp", "nativeVersion": init.AgentInfo.Version, "nativeCapabilities": init.AgentCapabilities, "models": session.Models, "configOptions": session.ConfigOptions}})
	prompt, err := Prompt(in, init.AgentCapabilities.PromptCapabilities.Image)
	if err != nil {
		return agent.TurnResult{SessionID: session.ID}, err
	}
	type reply struct {
		raw json.RawMessage
		err error
	}
	done := make(chan reply, 1)
	promptSent := make(chan struct{})
	go func() {
		params := map[string]any{"sessionId": session.ID, "prompt": prompt}
		if f.PromptMeta != nil {
			params["_meta"] = f.PromptMeta(in)
		}
		raw, err := c.CallStarted(ctx, "session/prompt", params, promptSent)
		done <- reply{raw, err}
	}()
	stream := NewStream(session.ID, emit)
	defer stream.Finish(true)
	requests, notifications := c.Requests, c.Notifications
	pending := map[string]Message{}
	waitingPrompt := ""
	completed := map[string]json.RawMessage{}
	var deferredResult *reply
	interruptPending := false
	modelControl := caps.SetModel
	// Keep the adapter's declared upper bound even when options arrive after new/resume.
	if f.Capabilities != nil {
		modelControl = f.Capabilities().SetModel
	} else {
		modelControl = Capabilities().SetModel
	}
	update := func(m Message) {
		var scope struct{ SessionID string }
		if json.Unmarshal(m.Params, &scope) == nil && scope.SessionID != "" && scope.SessionID != session.ID {
			return
		}
		if modelControl && m.Method == "session/update" {
			if value, ok := c.configOptions.Load(session.ID); ok {
				s := session
				s.ConfigOptions = value.([]ConfigOption)
				enabled := len(sessionModels(s)) > 0
				if caps.SetModel != enabled {
					caps.SetModel = enabled
					copy := caps
					emit(agent.Event{Type: agent.EventStatus, SessionID: session.ID, Capabilities: &copy})
				}
			}
		}
		if f.Completion != nil {
			if id, raw := f.Completion(m); id != "" {
				if id == waitingPrompt {
					waitingPrompt = ""
					done <- reply{raw: raw}
				} else if len(completed) < 16 {
					completed[id] = raw
				}
			}
		}
		if m.Method == "session/update" {
			stream.Update(m.Params)
		} else if f.Update != nil {
			f.Update(m, stream)
		}
	}
	defer func() {
		for _, req := range pending {
			_ = c.Reject(req.ID, -32800, "Turn ended before a response")
		}
	}()
	for {
		select {
		case <-promptSent:
			promptSent = nil
			if interruptPending {
				if err := c.Notify("session/cancel", map[string]string{"sessionId": session.ID}); err != nil {
					return agent.TurnResult{SessionID: session.ID}, err
				}
			}
		case <-ctx.Done():
			return agent.TurnResult{SessionID: session.ID, Text: stream.Text, Cancelled: true}, ctx.Err()
		case r := <-done:
			if r.err != nil {
				return agent.TurnResult{SessionID: session.ID}, r.err
			}
			if f.PromptID != nil {
				if id := f.PromptID(r.raw); id != "" {
					waitingPrompt = id
					if raw, ok := completed[id]; ok {
						waitingPrompt = ""
						done <- reply{raw: raw}
						delete(completed, id)
					} else if notifications == nil {
						return agent.TurnResult{SessionID: session.ID}, errors.New(f.Name + " exited before completing the prompt")
					}
					continue
				}
			}
			for draining := true; draining; {
				select {
				case m, ok := <-notifications:
					if !ok {
						draining = false
					} else {
						update(m)
					}
				default:
					draining = false
				}
			}
			var response struct {
				StopReason string `json:"stopReason"`
			}
			if err := json.Unmarshal(r.raw, &response); err != nil {
				return agent.TurnResult{SessionID: session.ID}, err
			}
			if response.StopReason == "" {
				return agent.TurnResult{SessionID: session.ID}, fmt.Errorf("%s returned no terminal stop reason", f.Name)
			}
			result := agent.TurnResult{SessionID: session.ID, Text: stream.Text, Subtype: response.StopReason}
			switch response.StopReason {
			case "cancelled", "canceled":
				result.Cancelled = true
			case "error", "refusal", "rate_limit":
				result.IsError = true
				if result.Text == "" {
					result.Text = f.Name + " stopped: " + response.StopReason
				}
			}
			if !result.Cancelled && !result.IsError && stream.Background.Active() {
				deferredResult = &r
				emit(agent.Event{Type: agent.EventStatus, SessionID: session.ID, Result: &agent.ResultInfo{Text: stream.Text, SessionID: session.ID}, Meta: map[string]any{"foregroundComplete": true, "backgroundActive": true}})
				continue
			}
			stream.Finish(result.Cancelled || result.IsError)
			emit(agent.Event{Type: agent.EventResult, SessionID: session.ID, Result: &agent.ResultInfo{Text: result.Text, SessionID: session.ID, Cancelled: result.Cancelled, IsError: result.IsError, Subtype: result.Subtype, Usage: stream.Usage}})
			return result, nil
		case m, ok := <-notifications:
			if !ok {
				notifications = nil
				if waitingPrompt != "" || deferredResult != nil {
					return agent.TurnResult{SessionID: session.ID}, errors.New(f.Name + " exited before completing the prompt")
				}
				continue
			}
			update(m)
			if deferredResult != nil && !stream.Background.Active() {
				done <- *deferredResult
				deferredResult = nil
			}
		case req, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			var it *agent.Interaction
			if req.Method == "session/request_permission" {
				it = Permission(req, f.Name)
			} else if f.Interaction != nil {
				it = f.Interaction(req)
			}
			if it == nil {
				if err := c.Reject(req.ID, -32601, "Unsupported client method"); err != nil {
					return agent.TurnResult{SessionID: session.ID}, err
				}
				continue
			}
			pending[it.ID] = req
			emit(agent.Event{Type: agent.EventInteraction, SessionID: session.ID, Interaction: it})
		case input, ok := <-in.Inbound:
			if !ok {
				in.Inbound = nil
				continue
			}
			if input.Kind == "interrupt" {
				if promptSent != nil {
					interruptPending = true
					continue
				}
				if err := c.Notify("session/cancel", map[string]string{"sessionId": session.ID}); err != nil {
					return agent.TurnResult{SessionID: session.ID}, err
				}
				continue
			}
			if input.Kind == "set_model" {
				// Setup consumes native notifications, so a live control must use
				// Call concurrently while this loop keeps relaying the stream.
				go func(input agent.Inbound) {
					controlCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
					defer cancel()
					err := SetModel(controlCtx, c, session, input.Text, false)
					if input.Ack != nil {
						select {
						case input.Ack <- err:
						case <-ctx.Done():
						}
					}
				}(input)
				continue
			}
			if input.Kind != "response" {
				if input.Ack != nil {
					input.Ack <- agent.ErrInputClosed
				}
				continue
			}
			req, ok := pending[input.InteractionID]
			if !ok {
				continue
			}
			var response any
			var err error
			if req.Method == "session/request_permission" {
				response, err = PermissionReply(req, input)
			} else if f.Reply != nil {
				response, err = f.Reply(req, input)
			} else {
				err = errors.New("missing ACP extension response handler")
			}
			if err != nil {
				return agent.TurnResult{SessionID: session.ID}, err
			}
			if err = c.Respond(req.ID, response); err != nil {
				return agent.TurnResult{SessionID: session.ID}, err
			}
			delete(pending, input.InteractionID)
		}
	}
}

func Prompt(in agent.TurnInput, images bool) ([]map[string]any, error) {
	out := []map[string]any{}
	if text := strings.TrimSpace(in.Options.ContextInstructions); text != "" {
		out = append(out, map[string]any{"type": "text", "text": text})
	}
	var files []agent.Attachment
	for _, attachment := range in.Options.Attachments {
		if !images || !strings.HasPrefix(attachment.Mime, "image/") {
			files = append(files, attachment)
			continue
		}
		data := attachment.Data
		if len(data) == 0 && attachment.Path != "" {
			var err error
			data, err = os.ReadFile(attachment.Path)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, map[string]any{"type": "image", "mimeType": attachment.Mime, "data": base64.StdEncoding.EncodeToString(data)})
	}
	out = append(out, map[string]any{"type": "text", "text": in.Message + agent.AttachmentReferenceText(files)})
	return out, nil
}

// MCPServers accepts the canonical named map and existing native ACP arrays.
func MCPServers(raw json.RawMessage, env map[string]string) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return []json.RawMessage{}, nil
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		return list, nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	if wrapped, ok := entries["mcpServers"]; ok {
		entries = nil
		if err := json.Unmarshal(wrapped, &entries); err != nil {
			return nil, err
		}
	}
	list = []json.RawMessage{}
	for name, value := range entries {
		var server agent.MCPServer
		if err := json.Unmarshal(value, &server); err != nil {
			return nil, err
		}
		v := map[string]any{"name": name}
		if server.URL != "" {
			v["type"], v["url"] = "http", server.URL
			headers := []map[string]string{}
			for k, value := range server.HTTPHeaders {
				headers = append(headers, map[string]string{"name": k, "value": value})
			}
			if server.BearerTokenEnvVar != "" {
				token, overridden := env[server.BearerTokenEnvVar]
				if !overridden {
					token = os.Getenv(server.BearerTokenEnvVar)
				}
				if token == "" {
					return nil, fmt.Errorf("MCP server %s requires environment credential %s", name, server.BearerTokenEnvVar)
				}
				headers = append(headers, map[string]string{"name": "Authorization", "value": "Bearer " + token})
			}
			v["headers"] = headers
		} else {
			if server.Command == "" {
				return nil, fmt.Errorf("MCP server %s has no command or URL", name)
			}
			if server.Cwd != "" {
				return nil, fmt.Errorf("ACP MCP server %s cannot override its working directory", name)
			}
			v["command"], v["args"] = server.Command, append([]string{}, server.Args...)
			vars := []map[string]string{}
			for k, value := range server.Env {
				vars = append(vars, map[string]string{"name": k, "value": value})
			}
			v["env"] = vars
		}
		encoded, _ := json.Marshal(v)
		list = append(list, encoded)
	}
	return list, nil
}
