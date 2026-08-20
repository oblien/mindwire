package claude

import (
	"encoding/json"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestEncodeInbound pins the stream-json control-protocol wire shapes the persistent transport writes
// to stdin for each unified Inbound. The correlators come from Meta (echoed from Interaction.Meta), so
// the encoding is stateless.
func TestEncodeInbound(t *testing.T) {
	decode := func(t *testing.T, in agent.Inbound) map[string]any {
		t.Helper()
		b, ok := encodeInbound(in)
		if !ok {
			t.Fatalf("encodeInbound(%+v) returned ok=false", in)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("encodeInbound produced invalid JSON: %v", err)
		}
		return m
	}

	t.Run("permission allow → control_response echoing the request id", func(t *testing.T) {
		m := decode(t, agent.Inbound{
			Kind: "response", InteractionID: "ctl-123", Decision: "allow",
			Meta: map[string]string{"respondVia": "control", "toolUseId": "toolu_9"},
		})
		if m["type"] != "control_response" {
			t.Fatalf("type = %v, want control_response", m["type"])
		}
		resp := m["response"].(map[string]any)
		if resp["request_id"] != "ctl-123" {
			t.Errorf("request_id = %v, want ctl-123 (the control-request id)", resp["request_id"])
		}
		if resp["subtype"] != "success" {
			t.Errorf("subtype = %v, want success", resp["subtype"])
		}
		if inner := resp["response"].(map[string]any); inner["behavior"] != "allow" {
			t.Errorf("behavior = %v, want allow", inner["behavior"])
		}
	})

	t.Run("permission deny → behavior deny with a message", func(t *testing.T) {
		m := decode(t, agent.Inbound{
			Kind: "response", InteractionID: "ctl-1", Decision: "deny", Text: "not allowed",
			Meta: map[string]string{"respondVia": "control"},
		})
		inner := m["response"].(map[string]any)["response"].(map[string]any)
		if inner["behavior"] != "deny" {
			t.Errorf("behavior = %v, want deny", inner["behavior"])
		}
		if inner["message"] != "not allowed" {
			t.Errorf("message = %v, want the deny reason", inner["message"])
		}
	})

	t.Run("question answer → tool_result keyed by the tool_use id", func(t *testing.T) {
		m := decode(t, agent.Inbound{
			Kind: "response", InteractionID: "toolu_42", Text: "Option A",
			Meta: map[string]string{"respondVia": "tool_result", "toolUseId": "toolu_42"},
		})
		if m["type"] != "user" {
			t.Fatalf("type = %v, want user", m["type"])
		}
		content := m["message"].(map[string]any)["content"].([]any)
		block := content[0].(map[string]any)
		if block["type"] != "tool_result" || block["tool_use_id"] != "toolu_42" {
			t.Errorf("block = %+v, want a tool_result keyed by toolu_42", block)
		}
		if block["content"] != "Option A" {
			t.Errorf("content = %v, want the answer text", block["content"])
		}
	})

	t.Run("input → plain user message", func(t *testing.T) {
		m := decode(t, agent.Inbound{Kind: "input", Text: "keep going"})
		if m["type"] != "user" {
			t.Fatalf("type = %v, want user", m["type"])
		}
		if m["message"].(map[string]any)["content"] != "keep going" {
			t.Errorf("content = %v, want the input text", m["message"])
		}
	})

	t.Run("interrupt → control_request interrupt", func(t *testing.T) {
		m := decode(t, agent.Inbound{Kind: "interrupt"})
		if m["type"] != "control_request" {
			t.Fatalf("type = %v, want control_request", m["type"])
		}
		if m["request"].(map[string]any)["subtype"] != "interrupt" {
			t.Errorf("subtype = %v, want interrupt", m["request"])
		}
	})

	t.Run("empty input is skipped", func(t *testing.T) {
		if _, ok := encodeInbound(agent.Inbound{Kind: "input", Text: "   "}); ok {
			t.Error("blank input should not encode")
		}
	})

	t.Run("set_model → control_request carrying the model", func(t *testing.T) {
		m := decode(t, agent.Inbound{Kind: "set_model", Text: "opus"})
		if m["type"] != "control_request" {
			t.Fatalf("type = %v, want control_request", m["type"])
		}
		req := m["request"].(map[string]any)
		if req["subtype"] != "set_model" {
			t.Errorf("subtype = %v, want set_model", req["subtype"])
		}
		if req["model"] != "opus" {
			t.Errorf("model = %v, want opus", req["model"])
		}
	})

	t.Run("set_model with empty text omits model (reset to default)", func(t *testing.T) {
		m := decode(t, agent.Inbound{Kind: "set_model", Text: "  "})
		req := m["request"].(map[string]any)
		if req["subtype"] != "set_model" {
			t.Errorf("subtype = %v, want set_model", req["subtype"])
		}
		if _, present := req["model"]; present {
			t.Errorf("model must be omitted when empty, got %+v", req)
		}
	})

	t.Run("set_permission_mode → control_request carrying the mode", func(t *testing.T) {
		m := decode(t, agent.Inbound{Kind: "set_permission_mode", Text: "plan"})
		if m["type"] != "control_request" {
			t.Fatalf("type = %v, want control_request", m["type"])
		}
		req := m["request"].(map[string]any)
		if req["subtype"] != "set_permission_mode" {
			t.Errorf("subtype = %v, want set_permission_mode", req["subtype"])
		}
		if req["mode"] != "plan" {
			t.Errorf("mode = %v, want plan", req["mode"])
		}
	})

	t.Run("set_permission_mode with empty mode is skipped", func(t *testing.T) {
		if _, ok := encodeInbound(agent.Inbound{Kind: "set_permission_mode", Text: ""}); ok {
			t.Error("blank permission mode should not encode")
		}
	})
}
