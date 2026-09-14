package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/runner"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// Exercise the installed CLI, including its argument parser and native session resume.
// Both providers point at a local server; credentials and Codex state are test-only.
// Run with CODEX_LOCAL=1 go test ./internal/agent/codex -run TestExecLocalProviderResume -v.
func TestExecLocalProviderResume(t *testing.T) {
	testLocalProviderResume(t, false)
}

func TestAppServerLocalProviderResume(t *testing.T) {
	testLocalProviderResume(t, true)
}

func testLocalProviderResume(t *testing.T, streaming bool) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 to test the installed Codex CLI against a local model server")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatal(err)
	}
	authEnvs := []string{"AZURE_OPENAI_API_KEY", "AZURE_OPENAI_ACCESS_TOKEN"}
	if streaming {
		authEnvs = append(authEnvs, "CODEX_API_KEY")
	}
	for _, envName := range authEnvs {
		t.Run(envName, func(t *testing.T) {
			codexDir := t.TempDir()
			t.Setenv("CODEX_HOME", codexDir)
			const credential = "mindwire-local-test-credential"
			type request struct {
				Path          string
				Authenticated bool
			}
			var mu sync.Mutex
			var requests []request
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/responses") {
					http.NotFound(w, r)
					return
				}
				authenticated := r.Header.Get("Authorization") == "Bearer "+credential
				mu.Lock()
				requests = append(requests, request{r.URL.Path, authenticated})
				index := len(requests)
				mu.Unlock()
				if r.URL.Path != "/foundry/responses" || !authenticated {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = io.WriteString(w, `{"error":{"message":"Missing bearer or basic authentication in header","type":"invalid_request_error"}}`)
					return
				}
				writeLocalResponse(w, index)
			}))
			defer server.Close()

			// A local unauthenticated default makes a lost provider override observable without
			// contacting OpenAI. The Foundry table has the same env-key contract as auth.Step.
			config := fmt.Sprintf(`model = "gpt-5.5"
model_provider = "default-sentinel"

[model_providers.default-sentinel]
name = "Local fallback sentinel"
base_url = %q
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0

[model_providers.azure-foundry]
name = "Local Foundry fixture"
base_url = %q
env_key = %q
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0
`, server.URL+"/fallback", server.URL+"/foundry", envName)
			if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			creds := mapStore{
				ckMethod: "azureFoundry", keySandbox: "read-only",
				agent.ProviderCredKey(azureProviderID): credential,
				agent.ProviderEnvKey(azureProviderID):  envName,
				pkModels(azureProviderID):              "gpt-5.5",
			}
			if envName == "CODEX_API_KEY" {
				creds = mapStore{ckMethod: "apiKey", ckAPIKey: credential,
					ckBaseURL: server.URL + "/foundry", keySandbox: "read-only"}
			}
			store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			r := runner.New(store, adapter{}, newAuth(creds), creds, stream.New(), t.TempDir())
			var threadID string
			for index := 1; index <= 3; index++ {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				var inbound chan agent.Inbound
				if streaming {
					inbound = make(chan agent.Inbound)
				}
				result, parts := r.RunTurn(ctx, runner.Turn{
					ChatID: "chat", RunID: fmt.Sprintf("run-%d", index), Message: fmt.Sprintf("Local prompt %d", index),
					Inbound: inbound,
					Options: agent.TurnOptions{ContinueLatest: streaming && index == 3},
				})
				cancel()
				mu.Lock()
				seen := append([]request(nil), requests...)
				mu.Unlock()
				if result.IsError {
					t.Fatalf("turn %d failed: %s; requests=%+v", index, result.Text, seen)
				}
				if want := fmt.Sprintf("Local reply %d", index); result.Text != want || len(parts) != 1 {
					t.Fatalf("turn %d: result=%q parts=%+v, want one %q reply", index, result.Text, parts, want)
				}
				if index == 1 {
					threadID = result.SessionID
				}
				if threadID == "" || result.SessionID != threadID || store.Session("codex", "chat") != threadID {
					t.Fatalf("turn %d did not continue the original thread", index)
				}
				if len(seen) != index || seen[index-1] != (request{"/foundry/responses", true}) {
					t.Fatalf("turn %d changed provider/auth or retried: %+v", index, seen)
				}
			}
		})
	}
}

func writeLocalResponse(w http.ResponseWriter, index int) {
	writeLocalReply(w, index, fmt.Sprintf("Local reply %d", index))
}

func writeLocalReply(w http.ResponseWriter, index int, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	itemID := fmt.Sprintf("msg_local_%d", index)
	item := map[string]any{
		"id": itemID, "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer",
		"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}},
	}
	started := map[string]any{
		"id": itemID, "type": "message", "role": "assistant", "status": "in_progress", "phase": "final_answer",
		"content": []any{},
	}
	response := map[string]any{
		"id": fmt.Sprintf("resp_local_%d", index), "object": "response", "status": "completed", "output": []any{item},
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15},
	}
	for _, event := range []map[string]any{
		{"type": "response.output_item.added", "output_index": 0, "item": started},
		{"type": "response.output_text.delta", "item_id": itemID, "output_index": 0, "content_index": 0, "delta": text},
		{"type": "response.output_item.done", "output_index": 0, "item": item},
		{"type": "response.completed", "response": response},
	} {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
	}
}

func TestAppServerLocalOptions(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 to check options through the installed CLI")
	}
	codexDir := t.TempDir()
	t.Setenv("CODEX_HOME", codexDir)
	const instructions = "Local options fixture: return the requested JSON object."
	var mu sync.Mutex
	var modelRequests []map[string]any
	var toolLists int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/mcp" {
			if r.Header.Get("X-Fixture-Token") != "local-mcp-token" {
				t.Error("per-turn MCP header was lost")
			}
			var result any
			switch request["method"] {
			case "initialize":
				result = map[string]any{"protocolVersion": "2024-11-05",
					"capabilities": map[string]any{"tools": map[string]any{}},
					"serverInfo":   map[string]any{"name": "fixture", "version": "1"}}
			case "tools/list":
				mu.Lock()
				toolLists++
				mu.Unlock()
				result = map[string]any{"tools": []any{}}
			default:
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result})
			return
		}
		if r.Header.Get("Authorization") != "Bearer local-api-key" ||
			r.Header.Get("OpenAI-Organization") != "local-org" || r.Header.Get("OpenAI-Project") != "local-project" {
			t.Error("model credentials or routing headers were lost")
		}
		mu.Lock()
		modelRequests = append(modelRequests, request)
		index := len(modelRequests)
		mu.Unlock()
		writeLocalReply(w, index, `{"ok":true}`)
	}))
	defer server.Close()
	config := fmt.Sprintf("model = \"gpt-5.5\"\nmodel_provider = \"local\"\n[model_providers.local]\nname = \"Local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\n", server.URL)
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	mcp, _ := json.Marshal(map[string]any{"fixture": map[string]any{
		"url": server.URL + "/mcp", "httpHeaders": map[string]string{"X-Fixture-Token": "local-mcp-token"}}})
	input := agent.TurnInput{
		Message: "Check the image.", CWD: t.TempDir(), Inbound: make(chan agent.Inbound),
		Config: map[string]string{keyModel: "gpt-5.5", keyEffort: "low", keyAddDir: t.TempDir(), keyAutoCompact: "120000"},
		Env: map[string]string{"CODEX_API_KEY": "local-api-key", "OPENAI_BASE_URL": server.URL,
			"OPENAI_ORGANIZATION": "local-org", "OPENAI_PROJECT": "local-project"},
		Options: agent.TurnOptions{SystemPrompt: instructions, MCPServers: mcp,
			OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`),
			Attachments:  []agent.Attachment{{Name: "pixel.png", Mime: "image/png", Data: picture.Bytes()}}},
	}
	for turn := 1; turn <= 2; turn++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		result, err := (adapter{}).RunStream(ctx, input, func(agent.Event) {})
		cancel()
		if err != nil || result.IsError || result.Text != `{"ok":true}` {
			t.Fatalf("turn %d: %+v %v", turn, result, err)
		}
		input.SessionID = result.SessionID
	}
	mu.Lock()
	defer mu.Unlock()
	if len(modelRequests) != 2 || toolLists < 2 {
		t.Fatalf("requests=%d MCP tool discovery=%d, want both on start and resume", len(modelRequests), toolLists)
	}
	for _, request := range modelRequests {
		wire, _ := json.Marshal(request)
		if request["instructions"] != instructions || !bytes.Contains(wire, []byte("data:image/png;base64,")) ||
			!bytes.Contains(wire, []byte(`"effort":"low"`)) || !bytes.Contains(wire, []byte(`"type":"json_schema"`)) {
			t.Fatalf("instructions, image, reasoning or output schema did not reach the model")
		}
	}
}
