package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
	_ "github.com/oblien/mindwire/daemon/internal/agent/codex"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// This fixture drives the real Codex binary, supervisor, parsers and authenticated HTTP/SSE API.
// The local model waits for the client to acknowledge partial content before it can finish a block.
// An implementation that buffers until item/turn completion therefore cannot pass these checks.
type codexStreamFixture struct {
	server *httptest.Server
	store  *session.Store
	cwd    string
	mu     sync.Mutex
	gates  map[string]chan struct{}
	calls  int
	errors []string
	finish chan struct{}
	once   sync.Once
}

func newCodexStreamFixture(t *testing.T) *codexStreamFixture {
	t.Helper()
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 to exercise Codex and the HTTP stream using a local model")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatal(err)
	}
	codexDir := t.TempDir()
	t.Setenv("CODEX_HOME", codexDir)
	f := &codexStreamFixture{cwd: t.TempDir(), gates: map[string]chan struct{}{}, finish: make(chan struct{})}
	model := httptest.NewServer(http.HandlerFunc(f.model))
	t.Cleanup(model.Close)
	config := fmt.Sprintf(`model = "gpt-5.5"
model_provider = "azure-foundry"
[model_providers.azure-foundry]
name = "Local Foundry streaming fixture"
base_url = %q
env_key = "AZURE_OPENAI_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0
`, model.URL)
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	// Codex subscribes to process output after spawn; give its watcher time to attach.
	tool := `sleep 0.25
printf 'tool output one\n'
checks=0
while [ ! -f "$1" ]; do
    checks=$((checks + 1))
    if [ "$checks" -gt 240 ]; then
        printf 'tool stream was not observed before completion\n' >&2
        exit 1
    fi
    sleep 0.05
done
printf 'tool output two\n'
`
	if err := os.WriteFile(filepath.Join(f.cwd, "stream_tool.sh"), []byte(tool), 0o600); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	sup := orchestrator.New(f.store, hub, notify.Fanout(nil), f.cwd, "codex")
	codex, ok := sup.Resolve("codex")
	if !ok {
		t.Fatal("Codex adapter not registered")
	}
	for key, value := range map[string]string{
		"authMethod": "azureFoundry", "provider:azure-foundry:apiKey": "local-provider-key",
		"provider:azure-foundry:envVar": "AZURE_OPENAI_API_KEY", "provider:azure-foundry:models": "gpt-5.5",
		"permission-mode": "never", "sandbox": "workspace-write",
	} {
		if err := codex.Creds.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	New(f.store, hub, sup).Register(mux)
	mux.HandleFunc("POST /fixture/ack/{phase}", func(w http.ResponseWriter, r *http.Request) {
		phase := r.PathValue("phase")
		f.release(phase)
		if strings.HasPrefix(phase, "tool-") {
			_ = os.WriteFile(filepath.Join(f.cwd, phase), []byte("observed"), 0o600)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /fixture/status", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"requests": f.calls, "errors": f.errors})
	})
	mux.HandleFunc("POST /fixture/finish", func(w http.ResponseWriter, r *http.Request) {
		f.once.Do(func() { close(f.finish) })
		w.WriteHeader(http.StatusNoContent)
	})
	authed := Auth("local-daemon-token", mux)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Model the separate cloud gateway token: the first SSE open needs a credential refresh,
		// while X-Mindwire-Token continues to authenticate the in-workspace daemon.
		if strings.HasSuffix(r.URL.Path, "/stream") && r.Header.Get("Authorization") != "Bearer gateway-fresh" {
			http.Error(w, "gateway token expired", http.StatusUnauthorized)
			return
		}
		r.Header.Del("Authorization") // The gateway consumes its bearer before forwarding.
		authed.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		for _, chat := range f.store.Chats() {
			if run, ok := f.store.LatestRun(chat.ChatID); ok && run.Status == "running" {
				sup.Cancel(run.ID)
			}
		}
		sup.Wait()
		f.server.Close()
	})
	return f
}

func (f *codexStreamFixture) gate(name string) chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gates[name] == nil {
		f.gates[name] = make(chan struct{})
	}
	return f.gates[name]
}

func (f *codexStreamFixture) release(name string) {
	gate := f.gate(name)
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-gate:
	default:
		close(gate)
	}
}

func (f *codexStreamFixture) wait(r *http.Request, phase string) bool {
	select {
	case <-f.gate(phase):
		return true
	case <-r.Context().Done():
		return false
	case <-time.After(12 * time.Second):
		f.mu.Lock()
		f.errors = append(f.errors, "client did not observe "+phase+" before completion")
		f.mu.Unlock()
		return false
	}
}

func streamFixtureEvent(w http.ResponseWriter, value map[string]any) {
	data, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", value["type"], data)
	w.(http.Flusher).Flush()
}

func (f *codexStreamFixture) message(w http.ResponseWriter, r *http.Request, turn int, phase, prefix, suffix string) (map[string]any, bool) {
	id := fmt.Sprintf("msg_%s_%d", phase, turn)
	item := map[string]any{"id": id, "type": "message", "role": "assistant", "phase": "commentary", "status": "in_progress", "content": []any{}}
	if phase == "final" {
		item["phase"] = "final_answer"
	}
	streamFixtureEvent(w, map[string]any{"type": "response.output_item.added", "output_index": 1, "item": item})
	streamFixtureEvent(w, map[string]any{"type": "response.output_text.delta", "item_id": id, "output_index": 1, "content_index": 0, "delta": prefix})
	if !f.wait(r, fmt.Sprintf("%s-%d", phase, turn)) {
		return nil, false
	}
	streamFixtureEvent(w, map[string]any{"type": "response.output_text.delta", "item_id": id, "output_index": 1, "content_index": 0, "delta": suffix})
	item["status"] = "completed"
	item["content"] = []any{map[string]any{"type": "output_text", "text": prefix + suffix, "annotations": []any{}}}
	streamFixtureEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 1, "item": item})
	return item, true
}

func (f *codexStreamFixture) model(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/responses" {
		http.NotFound(w, r)
		return
	}
	var request map[string]any
	_ = json.NewDecoder(r.Body).Decode(&request)
	if r.Header.Get("Authorization") != "Bearer local-provider-key" {
		http.Error(w, "missing model bearer", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	f.calls++
	index := f.calls
	f.mu.Unlock()
	turn, step := (index-1)/5+1, (index-1)%5
	w.Header().Set("Content-Type", "text/event-stream")
	// Each later operation waits for the preceding file diff to reach the client.
	// Final-history-only file parsing cannot satisfy these gates.
	if phase := map[int]string{2: "create", 3: "edit", 4: "files"}[step]; phase != "" {
		if !f.wait(r, fmt.Sprintf("%s-%d", phase, turn)) {
			return
		}
	}
	var output []any
	if step == 0 {
		id := fmt.Sprintf("reasoning_%d", turn)
		item := map[string]any{"id": id, "type": "reasoning", "summary": []any{}}
		streamFixtureEvent(w, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
		streamFixtureEvent(w, map[string]any{"type": "response.reasoning_summary_part.added", "item_id": id, "output_index": 0, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": ""}})
		streamFixtureEvent(w, map[string]any{"type": "response.reasoning_summary_text.delta", "item_id": id, "output_index": 0, "summary_index": 0, "delta": "Checking "})
		if !f.wait(r, fmt.Sprintf("thinking-%d", turn)) {
			return
		}
		streamFixtureEvent(w, map[string]any{"type": "response.reasoning_summary_text.delta", "item_id": id, "output_index": 0, "summary_index": 0, "delta": "the workspace."})
		item["summary"] = []any{map[string]any{"type": "summary_text", "text": "Checking the workspace."}}
		streamFixtureEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		output = append(output, item)
		message, ok := f.message(w, r, turn, "commentary", "Inspecting ", "files.")
		if !ok {
			return
		}
		output = append(output, message)
	}
	if step < 4 {
		item := map[string]any{"id": fmt.Sprintf("tool_%d", index), "call_id": fmt.Sprintf("call_%d", index)}
		if step == 0 {
			command := "/bin/sh " + agent.ShellQuote(filepath.Join(f.cwd, "stream_tool.sh")) + " " + agent.ShellQuote(filepath.Join(f.cwd, fmt.Sprintf("tool-%d", turn)))
			args, _ := json.Marshal(map[string]any{"cmd": command, "shell": "/bin/sh", "login": false, "max_output_tokens": 1000, "yield_time_ms": 10000})
			item["type"], item["name"], item["arguments"] = "function_call", "exec_command", string(args)
		} else {
			patch := fmt.Sprintf("*** Begin Patch\n*** Add File: result-%d.txt\n+Hello from Codex streaming\n*** End Patch", turn)
			if step == 2 {
				patch = fmt.Sprintf("*** Begin Patch\n*** Update File: result-%d.txt\n@@\n Hello from Codex streaming\n+Updated while streaming\n*** End Patch", turn)
			} else if step == 3 {
				var source strings.Builder
				for line := 0; line < 240; line++ {
					fmt.Fprintf(&source, "+let value%d = %d\n", line, line)
				}
				patch = fmt.Sprintf("*** Begin Patch\n*** Add File: App-%d.swift\n%s*** Add File: notes-%d.md\n+# Streaming app\n+Created with a second file in the same operation.\n*** End Patch", turn, source.String(), turn)
			}
			item["type"], item["name"], item["input"] = "custom_tool_call", "apply_patch", patch
		}
		streamFixtureEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 2, "item": item})
		output = append(output, item)
	} else {
		message, ok := f.message(w, r, turn, "final", "Finished ", fmt.Sprintf("turn %d.", turn))
		if !ok {
			return
		}
		output = append(output, message)
	}
	streamFixtureEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{
		"id": fmt.Sprintf("resp_%d", index), "object": "response", "status": "completed", "output": output,
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120},
	}})
}

func fixtureRequest(t *testing.T, base, method, path string, body any) *http.Response {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	request, err := http.NewRequest(method, base+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer gateway-fresh")
	request.Header.Set("X-Mindwire-Token", "local-daemon-token")
	response, err := (&http.Client{Timeout: 40 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode >= 300 {
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, data)
	}
	return response
}

func TestCodexStreamingHTTP(t *testing.T) {
	f := newCodexStreamFixture(t)
	for turn := 1; turn <= 3; turn++ {
		response := fixtureRequest(t, f.server.URL, "POST", "/turns?agent=codex", map[string]any{"chatId": "stream-chat", "message": fmt.Sprintf("Stream probe %d", turn)})
		var run session.Run
		_ = json.NewDecoder(response.Body).Decode(&run)
		response.Body.Close()
		response = fixtureRequest(t, f.server.URL, "GET", "/runs/"+run.ID+"/stream?agent=codex", nil)
		seen := map[string]bool{}
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 16<<20)
		var events []agent.Event
		for scanner.Scan() {
			if !strings.HasPrefix(scanner.Text(), "data: ") {
				continue
			}
			var event agent.Event
			if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
			phase := ""
			switch {
			case event.Type == agent.EventThinking && event.Text == "Checking ":
				phase = "thinking"
			case event.Type == agent.EventText && event.Text == "Inspecting ":
				phase = "commentary"
			case event.Type == agent.EventText && event.Text == "Finished ":
				phase = "final"
			case event.Type == agent.EventToolUse && event.Tool != nil && strings.Contains(event.Tool.Output, "tool output one") && !strings.Contains(event.Tool.Output, "tool output two"):
				phase = "tool"
			}
			if event.Tool != nil && event.Tool.Action != nil {
				for _, file := range event.Tool.Action.Files {
					switch {
					case strings.Contains(file.Diff, "+Updated while streaming"):
						phase = "edit"
					case strings.Contains(file.Diff, "+Hello from Codex streaming"):
						phase = "create"
					case strings.Contains(file.Diff, "+let value239 = 239"):
						phase = "files"
					}
				}
			}
			if phase != "" && !seen[phase] {
				if current, _ := f.store.GetRun(run.ID); current.Status != "running" {
					t.Fatalf("%s arrived after the turn ended", phase)
				}
				if phase == "files" {
					listing := fixtureRequest(t, f.server.URL, "GET", "/chats", nil)
					var chats []session.ChatSummary
					_ = json.NewDecoder(listing.Body).Decode(&chats)
					listing.Body.Close()
					if len(chats) != 1 || chats[0].LastStatus != "running" || chats[0].LastRunID != run.ID || chats[0].Agent != "codex" {
						t.Fatalf("daemon did not expose the working chat: %+v", chats)
					}
					history := fixtureRequest(t, f.server.URL, "GET", "/chats/stream-chat/messages?agent=codex", nil)
					var messages []agent.Message
					_ = json.NewDecoder(history.Body).Decode(&messages)
					history.Body.Close()
					if len(messages) != turn*2-1 || messages[len(messages)-1].Role != "user" {
						t.Fatalf("active history must keep prior turns and exclude live output: %+v", messages)
					}
				}
				seen[phase] = true
				ack := fixtureRequest(t, f.server.URL, "POST", fmt.Sprintf("/fixture/ack/%s-%d", phase, turn), nil)
				ack.Body.Close()
			}
		}
		response.Body.Close()
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		current, _ := f.store.GetRun(run.ID)
		if len(seen) != 7 || current.Status != "done" {
			data, _ := json.Marshal(events)
			t.Fatalf("turn %d: partial phases=%v run=%+v events=%s", turn, seen, current, data)
		}
		if data, err := os.ReadFile(filepath.Join(f.cwd, fmt.Sprintf("result-%d.txt", turn))); err != nil || string(data) != "Hello from Codex streaming\nUpdated while streaming\n" {
			t.Fatalf("native file edit failed: %q %v", data, err)
		}
		response = fixtureRequest(t, f.server.URL, "GET", "/chats/stream-chat/messages?agent=codex", nil)
		var messages []agent.Message
		_ = json.NewDecoder(response.Body).Decode(&messages)
		response.Body.Close()
		if len(messages) != turn*2 {
			t.Fatalf("history duplicated/lost a turn: %+v", messages)
		}
		var final, diffs int
		for _, part := range messages[len(messages)-1].Parts {
			if part.Type == "text" && part.Text == fmt.Sprintf("Finished turn %d.", turn) {
				final++
			}
			if part.Tool != nil && part.Tool.Action != nil {
				for _, file := range part.Tool.Action.Files {
					if file.Diff != "" {
						diffs++
					}
				}
			}
		}
		if final != 1 || diffs != 4 {
			t.Fatalf("turn %d: final replies=%d diffs=%d, want one reply and four diffs; %+v", turn, final, diffs, messages)
		}
	}
}

// The iOS integration test uses this same fixture through URLSession and its production feed.
// Write a temporary endpoint file, then keep the test alive until the client posts /fixture/finish.
func TestCodexStreamingIOSFixture(t *testing.T) {
	path := os.Getenv("MINDWIRE_STREAM_FIXTURE_FILE")
	if path == "" {
		t.Skip("set MINDWIRE_STREAM_FIXTURE_FILE to serve the iOS integration fixture")
	}
	f := newCodexStreamFixture(t)
	data, _ := json.Marshal(map[string]any{"url": f.server.URL, "cwd": f.cwd})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	t.Logf("stream fixture ready at %s", f.server.URL)
	select {
	case <-f.finish:
	case <-time.After(10 * time.Minute):
		t.Fatal("iOS fixture timed out")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.errors) > 0 {
		t.Fatalf("stream was buffered: %v", f.errors)
	}
}
