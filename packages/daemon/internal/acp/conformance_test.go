package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

type peer struct {
	t   *testing.T
	dec *json.Decoder
	enc *json.Encoder
}

func (p peer) read(method string) Message {
	p.t.Helper()
	var m Message
	if err := p.dec.Decode(&m); err != nil {
		p.t.Fatal(err)
	}
	if method != "" && m.Method != method {
		p.t.Fatalf("method %q, want %q", m.Method, method)
	}
	return m
}
func (p peer) send(v any) {
	p.t.Helper()
	if err := p.enc.Encode(v); err != nil {
		p.t.Fatal(err)
	}
}
func (p peer) reply(m Message, result any) {
	p.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
}
func (p peer) update(v any) {
	p.send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "session", "update": v}})
}
func fixture(t *testing.T, run func(peer)) *Client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	t.Cleanup(func() { inR.Close(); inW.Close(); outR.Close(); outW.Close() })
	go func() { defer outW.Close(); run(peer{t, json.NewDecoder(inR), json.NewEncoder(outW)}) }()
	c := NewClient(inW, outR)
	t.Cleanup(c.Close)
	return c
}
func (p peer) setup() {
	p.reply(p.read("initialize"), map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"version": "1.2.3"}, "agentCapabilities": map[string]any{"loadSession": true, "promptCapabilities": map[string]bool{"image": true}, "mcpCapabilities": map[string]bool{"http": true}}})
	p.reply(p.read("session/new"), map[string]string{"sessionId": "session"})
}
func runFixture(t *testing.T, c *Client, f Flavor, in agent.TurnInput) (agent.TurnResult, []agent.Event, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var events []agent.Event
	result, err := RunSession(ctx, c, f, in, func(e agent.Event) { events = append(events, e) })
	return result, events, err
}

func TestCurrentACPInteractionsToolsAndCancellation(t *testing.T) {
	inbound := make(chan agent.Inbound, 1)
	c := fixture(t, func(p peer) {
		p.setup()
		prompt := p.read("session/prompt")
		p.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "tool", "title": "Read", "rawInput": map[string]string{"path": "README.md"}, "status": "pending"})
		p.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool", "status": "in_progress", "content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": "partial"}}}})
		p.send(map[string]any{"id": "permission-7", "method": "session/request_permission", "params": map[string]any{"sessionId": "session", "toolCall": map[string]string{"title": "Read README"}, "options": []map[string]string{{"optionId": "native-allow-once", "name": "Allow once", "kind": "allow_once"}, {"optionId": "native-reject", "kind": "reject_once"}}}})
		response := p.read("")
		if string(response.ID) != `"permission-7"` || !strings.Contains(string(response.Result), `"optionId":"native-allow-once"`) {
			t.Errorf("lost native approval identity: %+v", response)
		}
		p.send(map[string]any{"id": 90, "method": "fs/unknown", "params": map[string]any{}})
		response = p.read("")
		if response.Error == nil || response.Error.Code != -32601 {
			t.Error("unknown client method silently succeeded")
		}
		p.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool", "status": "completed", "content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": "file contents"}}}})
		p.reply(prompt, map[string]string{"stopReason": "cancelled"})
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var events []agent.Event
	result, err := RunSession(ctx, c, Flavor{Name: "Fixture"}, agent.TurnInput{Message: "read", Inbound: inbound}, func(e agent.Event) {
		events = append(events, e)
		if e.Interaction != nil {
			inbound <- agent.Inbound{Kind: "response", InteractionID: e.Interaction.ID, Decision: "native-allow-once"}
		}
	})
	if err != nil || !result.Cancelled || result.IsError {
		t.Fatalf("cancel result: %+v %v", result, err)
	}
	results := 0
	for _, e := range events {
		if e.Type == agent.EventToolResult {
			results++
			if e.Tool.Status != "completed" || e.Tool.Output != "file contents" {
				t.Fatalf("tool result: %+v", e.Tool)
			}
		}
	}
	if results != 1 {
		t.Fatalf("running tool updates became results: %d", results)
	}
	if events[0].Capabilities == nil || !events[0].Capabilities.Negotiated || !events[0].Capabilities.ImageInput || events[0].Capabilities.SetModel {
		t.Fatalf("not negotiated: %+v", events[0])
	}
}

func TestAsyncPromptCompletionAndEarlyExit(t *testing.T) {
	for _, mode := range []string{"before", "after", "exit"} {
		t.Run(mode, func(t *testing.T) {
			c := fixture(t, func(p peer) {
				p.setup()
				prompt := p.read("session/prompt")
				completion := func() {
					p.send(map[string]any{"method": "vendor/complete", "params": map[string]string{"id": "prompt-1", "stopReason": "end_turn"}})
				}
				if mode == "before" {
					completion()
				}
				p.reply(prompt, map[string]string{"promptId": "prompt-1"})
				if mode == "after" {
					completion()
				}
			})
			f := Flavor{Name: "Fixture", PromptID: func(raw json.RawMessage) string {
				var v struct{ PromptID string }
				json.Unmarshal(raw, &v)
				return v.PromptID
			}, Completion: func(m Message) (string, json.RawMessage) {
				if m.Method != "vendor/complete" {
					return "", nil
				}
				var p struct{ ID string }
				json.Unmarshal(m.Params, &p)
				return p.ID, m.Params
			}}
			result, _, err := runFixture(t, c, f, agent.TurnInput{Message: "test"})
			if mode == "exit" {
				if err == nil || strings.Contains(err.Error(), "deadline") {
					t.Fatalf("exit was not reported promptly: %v", err)
				}
			} else if err != nil || result.Subtype != "end_turn" {
				t.Fatalf("completion race: %+v %v", result, err)
			}
		})
	}
}

func TestBackgroundWorkOutlivesForegroundResult(t *testing.T) {
	c := fixture(t, func(p peer) {
		p.setup()
		prompt := p.read("session/prompt")
		p.send(map[string]any{"method": "vendor/task", "params": map[string]string{"id": "task", "status": "running"}})
		p.reply(prompt, map[string]string{"stopReason": "end_turn"})
		p.send(map[string]any{"method": "vendor/task", "params": map[string]string{"id": "task", "status": "completed", "output": "finished"}})
	})
	f := Flavor{Name: "Fixture", Update: func(m Message, s *Stream) {
		if m.Method == "vendor/task" {
			var task agent.TaskInfo
			json.Unmarshal(m.Params, &task)
			s.Task(task, false)
		}
	}}
	result, events, err := runFixture(t, c, f, agent.TurnInput{Message: "test"})
	if err != nil || result.IsError {
		t.Fatalf("result: %+v %v", result, err)
	}
	settled := false
	for _, e := range events {
		if e.Task != nil && e.Task.Status == "completed" {
			settled = true
		}
		if e.Type == agent.EventResult && !settled {
			t.Fatal("foreground closed the task owner")
		}
	}
	if !settled {
		t.Fatal("lost task completion")
	}
}

func TestResumeRefreshesCanonicalMCPWithoutReplayingText(t *testing.T) {
	c := fixture(t, func(p peer) {
		p.reply(p.read("initialize"), map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}}})
		load := p.read("session/load")
		if !strings.Contains(string(load.Params), `"value":"Bearer scoped-token"`) {
			t.Error("missing fresh MCP credentials")
		}
		for range 300 {
			p.update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"text": "old"}})
		}
		p.reply(load, map[string]string{"sessionId": "session"})
		prompt := p.read("session/prompt")
		p.update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"text": "new"}})
		p.reply(prompt, map[string]string{"stopReason": "end_turn"})
	})
	result, _, err := runFixture(t, c, Flavor{}, agent.TurnInput{Message: "next", SessionID: "session", Env: map[string]string{"TOKEN": "scoped-token"}, Options: agent.TurnOptions{MCPServers: json.RawMessage(`{"mcpServers":{"product":{"url":"http://127.0.0.1:1234/mcp","bearerTokenEnvVar":"TOKEN"}}}`)}})
	if err != nil || result.Text != "new" {
		t.Fatalf("resume: %+v %v", result, err)
	}
}

func TestProviderErrorPreservesIncompleteTools(t *testing.T) {
	c := fixture(t, func(p peer) {
		p.setup()
		p.read("session/prompt")
		p.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "tool", "title": "Build"})
	})
	_, events, err := runFixture(t, c, Flavor{}, agent.TurnInput{})
	if err == nil {
		t.Fatal("process exit became success")
	}
	last := events[len(events)-1]
	if last.Tool == nil || last.Tool.Status != "incomplete" || !last.Tool.IsError {
		t.Fatalf("unfinished tool: %+v", last)
	}
}

func TestCanonicalMCPRejectsMissingCredential(t *testing.T) {
	_, err := MCPServers(json.RawMessage(`{"tool":{"url":"https://example.test/mcp","bearerTokenEnvVar":"MISSING"}}`), nil)
	if err == nil {
		t.Fatal("missing credential silently omitted")
	}
	_, err = MCPServers(json.RawMessage(`{"tool":{"command":"node","cwd":"/other"}}`), nil)
	if err == nil {
		t.Fatal("unsupported working directory silently dropped")
	}
	t.Setenv("MINDWIRE_MCP_FIXTURE_TOKEN", "inherited-token")
	raw := json.RawMessage(`{"tool":{"url":"https://example.test/mcp","bearerTokenEnvVar":"MINDWIRE_MCP_FIXTURE_TOKEN"}}`)
	servers, err := MCPServers(raw, nil)
	if err != nil || !strings.Contains(string(servers[0]), "Bearer inherited-token") {
		t.Fatalf("native environment reference lost: %v", err)
	}
	if _, err = MCPServers(raw, map[string]string{"MINDWIRE_MCP_FIXTURE_TOKEN": ""}); err == nil {
		t.Fatal("explicitly cleared credential inherited its old value")
	}
}

func TestModelControlUsesNegotiatedConfigID(t *testing.T) {
	c := fixture(t, func(p peer) {
		m := p.read("session/set_config_option")
		if !strings.Contains(string(m.Params), `"configId":"model-picker"`) {
			t.Errorf("wrong native config: %s", m.Params)
		}
		p.send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32602, "message": "unavailable model"}})
	})
	err := SetModel(t.Context(), c, Session{ID: "session", ConfigOptions: []ConfigOption{{ID: "model-picker", Category: "model"}}}, "unknown", false)
	if err == nil || !strings.Contains(fmt.Sprint(err), "unavailable model") {
		t.Fatalf("control error: %v", err)
	}
}

func TestImmediateInterruptCannotOvertakePrompt(t *testing.T) {
	inbound := make(chan agent.Inbound, 1)
	inbound <- agent.Inbound{Kind: "interrupt"}
	c := fixture(t, func(p peer) {
		p.setup()
		prompt := p.read("session/prompt")
		p.read("session/cancel")
		p.reply(prompt, map[string]string{"stopReason": "cancelled"})
	})
	r, _, err := runFixture(t, c, Flavor{}, agent.TurnInput{Inbound: inbound})
	if err != nil || !r.Cancelled {
		t.Fatalf("interrupt: %+v %v", r, err)
	}
}

func TestDelayedNativeModelOptionsAndLiveAcknowledgement(t *testing.T) {
	inbound := make(chan agent.Inbound, 1)
	ack := make(chan error, 1)
	c := fixture(t, func(p peer) {
		p.setup()
		prompt := p.read("session/prompt")
		p.update(map[string]any{"sessionUpdate": "config_option_update", "configOptions": []any{map[string]any{"id": "account-model", "category": "model", "options": []any{map[string]string{"value": "fast", "name": "Fast"}}}}})
		control := p.read("session/set_config_option")
		if !strings.Contains(string(control.Params), `"configId":"account-model"`) {
			t.Error("lost delayed model config ID")
		}
		p.reply(control, map[string]any{})
		select {
		case err := <-ack:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("native control not acknowledged")
		}
		p.reply(prompt, map[string]string{"stopReason": "end_turn"})
	})
	var first *agent.Capabilities
	updated := false
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err := RunSession(ctx, c, Flavor{}, agent.TurnInput{Inbound: inbound}, func(e agent.Event) {
		if e.Type == agent.EventSession {
			first = e.Capabilities
		}
		if e.Type == agent.EventStatus && e.Capabilities != nil && e.Capabilities.SetModel {
			updated = true
			inbound <- agent.Inbound{Kind: "set_model", Text: "fast", Ack: ack}
		}
	})
	if err != nil || !updated || first == nil || first.SetModel {
		t.Fatalf("capabilities mutated or not refreshed: %+v %v %v", first, updated, err)
	}
}

func TestSetupWaitsForAsynchronousModelDiscovery(t *testing.T) {
	c := fixture(t, func(p peer) {
		p.setup()
		p.update(map[string]any{"sessionUpdate": "config_option_update", "configOptions": []any{map[string]any{"id": "native-model", "category": "model", "options": []any{map[string]string{"value": "model-1", "name": "Model 1"}}}}})
		control := p.read("session/set_config_option")
		p.reply(control, map[string]any{})
		p.reply(p.read("session/prompt"), map[string]string{"stopReason": "end_turn"})
	})
	f := Flavor{Configure: func(ctx context.Context, c *Client, s Session, in agent.TurnInput) error {
		return Configure(ctx, c, s, "model-1", "")
	}}
	_, events, err := runFixture(t, c, f, agent.TurnInput{})
	if err != nil || len(events) == 0 || !events[0].Capabilities.SetModel {
		t.Fatalf("delayed discovery: %v", err)
	}
}
