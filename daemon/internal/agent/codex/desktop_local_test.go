package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/surface"
)

type desktopFixtureProvider struct{ inputs atomic.Int32 }

func (*desktopFixtureProvider) Status(context.Context) (surface.ProviderStatus, error) {
	return surface.ProviderStatus{Supported: true, Enabled: true, Available: true, Capabilities: surface.Capabilities{View: true, Capture: true, Keyboard: true}}, nil
}
func (*desktopFixtureProvider) Connect(context.Context) (surface.Geometry, error) {
	return surface.Geometry{Width: 128, Height: 96, Revision: 1}, nil
}
func (*desktopFixtureProvider) Capture(context.Context) (image.Image, surface.Geometry, error) {
	img := image.NewRGBA(image.Rect(0, 0, 128, 96))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 30, G: 60, B: 200, A: 255}), image.Point{}, draw.Src)
	return img, surface.Geometry{Width: 128, Height: 96, Revision: 1}, nil
}
func (p *desktopFixtureProvider) Apply(context.Context, surface.Action) (string, error) {
	p.inputs.Add(1)
	return "", nil
}
func (*desktopFixtureProvider) Release(context.Context) error { return nil }
func (*desktopFixtureProvider) Close() error                  { return nil }
func (*desktopFixtureProvider) ExpiresAt() time.Time          { return time.Now().Add(time.Hour) }

// The installed Codex app-server drives the real surface MCP server. Only the
// model's Responses API is scripted, so this needs no external model credentials.
func TestAppServerDesktopMCP(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 to exercise the installed Codex CLI")
	}
	codexDir := t.TempDir()
	t.Setenv("CODEX_HOME", codexDir)
	db, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := surface.New(db)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	provider := &desktopFixtureProvider{}
	if err = service.Configure(provider); err != nil {
		t.Fatal(err)
	}
	var approvals atomic.Int32
	service.SetApproval(func(context.Context, surface.Actor, string) error { approvals.Add(1); return nil })
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	overlay, env, cleanup, err := service.BindRun(ctx, surface.Actor{Kind: "agent", Name: "Codex", RunID: "codex-desktop", ChatID: "chat"}, "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	var requests atomic.Int32
	var step atomic.Int32
	var imageReceived atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		data, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if r.Header.Get("Authorization") != "Bearer fixture-api-key" {
			t.Error("native model auth was lost")
		}
		var request map[string]any
		if json.Unmarshal(data, &request) != nil {
			t.Error("invalid native model request")
			w.WriteHeader(400)
			return
		}
		requestIndex := int(requests.Add(1))
		index := int(step.Load()) + 1
		if requestIndex > 10 {
			t.Error("tool discovery did not converge")
			writeLocalReply(w, requestIndex, "Stop.")
			return
		}
		if strings.Contains(string(data), "data:image/png;base64,") {
			imageReceived.Store(true)
		}
		operations := []string{"status", "open", "capture", "action", "release"}
		if index > len(operations) {
			writeLocalReply(w, index, "Desktop protocol complete.")
			return
		}
		operation := operations[index-1]
		name := ""
		namespace := ""
		var findName func(any, string)
		findName = func(v any, scope string) {
			switch v := v.(type) {
			case map[string]any:
				if v["type"] == "namespace" {
					scope, _ = v["name"].(string)
				}
				if n, ok := v["name"].(string); ok && strings.HasSuffix(n, "surface_"+operation) {
					if strings.Contains(n, "mindwire_desktop") || strings.Contains(scope, "mindwire_desktop") {
						name, namespace = n, scope
					}
				}
				for _, item := range v {
					findName(item, scope)
				}
			case []any:
				for _, item := range v {
					findName(item, scope)
				}
			}
		}
		findName(request, "")
		if name == "" {
			if requestIndex == 1 {
				item := map[string]any{"id": "search_desktop", "type": "tool_search_call", "call_id": "search_desktop", "execution": "client", "arguments": map[string]any{"query": "mindwire_desktop surface_status surface_open surface_capture surface_action surface_release", "limit": 5}, "status": "completed"}
				writeLocalItems(w, requestIndex, item)
				return
			}
			t.Errorf("desktop %s was not advertised to the native model", operation)
			writeLocalReply(w, index, "Missing desktop tool.")
			return
		}
		step.Add(1)
		arguments := map[string]any{}
		switch operation {
		case "open":
			arguments = map[string]any{"requestId": "codex-open", "mode": "control"}
		case "capture", "release", "action":
			control := service.Snapshot().Controller
			if control == nil {
				t.Error("native CLI did not acquire the shared controller")
				writeLocalReply(w, index, "Missing controller.")
				return
			}
			arguments["sessionId"] = control.SessionID
			if operation == "action" {
				arguments["requestId"] = "codex-key"
				arguments["controlGeneration"] = control.Generation
				arguments["action"] = map[string]any{"kind": "key", "keys": []string{"Escape"}}
			}
		}
		writeLocalToolCall(w, index, namespace, name, arguments)
	}))
	defer server.Close()
	config := fmt.Sprintf("model = \"gpt-5.5\"\nmodel_provider = \"local\"\n[model_providers.local]\nname = \"Local desktop fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\n", server.URL)
	if err = os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	env["CODEX_API_KEY"] = "fixture-api-key"
	env["OPENAI_BASE_URL"] = server.URL
	observed := map[string]bool{}
	result, err := (adapter{}).RunStream(ctx, agent.TurnInput{Message: "Check desktop tools.", CWD: t.TempDir(), Env: env, Inbound: make(chan agent.Inbound),
		Config: map[string]string{keyModel: "gpt-5.5", keyApproval: "never", keySandbox: "read-only", keyEffort: "low"}, Options: agent.TurnOptions{MCPServers: overlay}}, func(ev agent.Event) {
		if ev.Tool == nil {
			return
		}
		agent.NormalizeSurfaceTool(ev.Tool)
		if ev.Type == agent.EventToolResult && ev.Tool.Action != nil && ev.Tool.Action.Surface != nil {
			a := ev.Tool.Action.Surface
			observed[a.Operation] = true
			if a.Operation == "capture" && a.Capture == nil {
				t.Error("native Codex capture lost artifact metadata")
			}
			if ev.Tool.IsError {
				t.Errorf("native desktop %s failed: %s", a.Operation, ev.Tool.Output)
			}
		}
	})
	if err != nil || result.IsError {
		t.Fatalf("native Codex failed: %v %s", err, result.Text)
	}
	for _, operation := range []string{"status", "open", "capture", "key", "release"} {
		if !observed[operation] {
			t.Errorf("missing native desktop card: %s", operation)
		}
	}
	if !imageReceived.Load() {
		t.Error("MCP screenshot did not reach Codex as image input")
	}
	if approvals.Load() != 1 || provider.inputs.Load() != 1 {
		t.Fatalf("permission/input duplicated or missing: approvals=%d inputs=%d", approvals.Load(), provider.inputs.Load())
	}
	if service.Snapshot().Controller != nil {
		t.Error("native release did not clear controller")
	}
	history, err := (adapter{}).History(agent.HistoryQuery{SessionID: result.SessionID, ChatID: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	historyTools, historyImages := 0, 0
	for _, message := range history {
		for _, part := range message.Parts {
			if part.Tool == nil || part.Tool.Action == nil || part.Tool.Action.Surface == nil {
				continue
			}
			historyTools++
			if part.Tool.Action.Surface.Capture != nil {
				historyImages++
			}
		}
	}
	if historyTools != 5 || historyImages != 1 {
		t.Errorf("native history lost desktop cards: tools=%d images=%d", historyTools, historyImages)
	}
	encodedHistory, _ := json.Marshal(history)
	if strings.Contains(string(encodedHistory), "data:image/png") || strings.Contains(string(encodedHistory), "iVBORw0KGgo") {
		t.Error("native history duplicated image bytes in the feed")
	}
	t.Log("native Codex discovered all tools, requested permission, observed an image, dispatched input and released control")
}

func writeLocalToolCall(w http.ResponseWriter, index int, namespace, name string, arguments map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	args, _ := json.Marshal(arguments)
	id := fmt.Sprintf("fc_desktop_%d", index)
	item := map[string]any{"id": id, "type": "function_call", "call_id": fmt.Sprintf("call_desktop_%d", index), "name": name, "arguments": string(args), "status": "completed"}
	started := map[string]any{"id": id, "type": "function_call", "call_id": item["call_id"], "name": name, "arguments": "", "status": "in_progress"}
	if namespace != "" {
		item["namespace"], started["namespace"] = namespace, namespace
	}
	response := map[string]any{"id": fmt.Sprintf("resp_desktop_%d", index), "object": "response", "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}
	for _, event := range []map[string]any{
		{"type": "response.output_item.added", "output_index": 0, "item": started},
		{"type": "response.function_call_arguments.delta", "item_id": id, "output_index": 0, "delta": string(args)},
		{"type": "response.output_item.done", "output_index": 0, "item": item},
		{"type": "response.completed", "response": response},
	} {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
	}
}

func writeLocalItems(w http.ResponseWriter, index int, item map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	response := map[string]any{"id": fmt.Sprintf("resp_search_%d", index), "object": "response", "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}
	for _, event := range []map[string]any{{"type": "response.output_item.added", "output_index": 0, "item": item}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": response}} {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
	}
}
