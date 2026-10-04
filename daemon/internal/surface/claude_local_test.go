package surface

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Only the Messages API is scripted. The installed Claude CLI, MCP discovery,
// permissions, image delivery, normalization and desktop service remain real.
func localClaudeDesktopModel(t *testing.T, s *Service, env map[string]string) {
	t.Helper()
	var step atomic.Int32
	var imageReceived atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/count_tokens") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"input_tokens":100}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/messages") || r.Method != "POST" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[],"has_more":false}`)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		var request struct {
			Stream bool `json:"stream"`
			Tools  []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		if json.Unmarshal(body, &request) != nil {
			http.Error(w, "invalid Messages request", 400)
			return
		}
		if strings.Contains(string(body), `"image/png"`) && strings.Contains(string(body), `"base64"`) {
			imageReceived.Store(true)
		}
		operations := []string{"desktops", "project_desktop", "capture", "action", "release"}
		index := int(step.Load())
		var block map[string]any
		reason := "end_turn"
		if index >= len(operations) {
			block = map[string]any{"type": "text", "text": "Desktop protocol complete."}
		} else {
			operation, name := operations[index], ""
			for _, tool := range request.Tools {
				if strings.HasSuffix(tool.Name, "__surface_"+operation) && strings.Contains(tool.Name, MCPName) {
					name = tool.Name
					break
				}
			}
			if name == "" {
				t.Errorf("Claude did not discover surface_%s", operation)
				block = map[string]any{"type": "text", "text": "Missing desktop tool."}
			} else {
				args := map[string]any{}
				switch operation {
				case "project_desktop":
					args = map[string]any{"requestId": "cli-open", "mode": "control"}
				case "capture", "action", "release":
					selected, err := s.ForDesktop("ds_0000000000000001")
					if err != nil {
						t.Error(err)
						http.Error(w, "desktop unavailable", 500)
						return
					}
					control := selected.Snapshot().Controller
					if control == nil {
						t.Error("Claude did not acquire the shared controller")
						http.Error(w, "missing controller", 500)
						return
					}
					args["sessionId"] = control.SessionID
					if operation == "action" {
						args["requestId"], args["controlGeneration"] = "cli-escape", control.Generation
						args["action"] = map[string]any{"kind": "key", "keys": []string{"Escape"}}
					}
				}
				step.Add(1)
				block = map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_desktop_%d", index), "name": name, "input": args}
				reason = "tool_use"
			}
		}
		message := map[string]any{"id": fmt.Sprintf("msg_desktop_%d", index), "type": "message", "role": "assistant", "model": "claude-haiku-4-5", "content": []any{block}, "stop_reason": reason, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 100, "output_tokens": 20}}
		if !request.Stream {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(message)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		start := map[string]any{}
		for k, v := range message {
			start[k] = v
		}
		start["content"], start["stop_reason"] = []any{}, nil
		write := func(kind string, data map[string]any) {
			data["type"] = kind
			encoded, _ := json.Marshal(data)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, encoded)
		}
		write("message_start", map[string]any{"message": start})
		if reason == "tool_use" {
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": block["id"], "name": block["name"], "input": map[string]any{}}})
			args, _ := json.Marshal(block["input"])
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(args)}})
		} else {
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": block["text"]}})
		}
		write("content_block_stop", map[string]any{"index": 0})
		write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 20}})
		write("message_stop", map[string]any{})
	}))
	t.Cleanup(server.Close)
	env["CLAUDE_CONFIG_DIR"] = t.TempDir()
	env["ANTHROPIC_BASE_URL"], env["ANTHROPIC_API_KEY"] = server.URL, "fixture-api-key"
	for _, key := range []string{"ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_CUSTOM_HEADERS"} {
		env[key] = ""
	}
	for _, key := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		env[key] = "0"
	}
	env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
	env["ENABLE_TOOL_SEARCH"] = "false"
	t.Cleanup(func() {
		if step.Load() != 5 {
			t.Errorf("native Claude completed %d/5 desktop operations", step.Load())
		}
		if !imageReceived.Load() {
			t.Error("MCP screenshot did not reach Claude as image input")
		}
	})
}
