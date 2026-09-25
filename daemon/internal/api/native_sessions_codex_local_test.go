package api

import (
	"context"
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
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// Real Codex binary, disposable native home, local model: CLI -> Mindwire -> CLI.
// No user chats or paid model requests are involved. Merely listing must never
// call the model, and each continuation must carry the prior native context.
func TestNativeCodexCLIMindwireRoundTrip(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 for the native CLI interoperability check")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatal(err)
	}
	home, cwd := t.TempDir(), t.TempDir()
	cwd, _ = filepath.EvalSymlinks(cwd)
	t.Setenv("CODEX_HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("AZURE_OPENAI_API_KEY", "local-provider-key")
	var mu sync.Mutex
	var requests []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, string(body))
		n := len(requests)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		text := fmt.Sprintf("Native answer %d", n)
		item := map[string]any{"id": fmt.Sprintf("msg_native_%d", n), "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
		streamFixtureEvent(w, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
		streamFixtureEvent(w, map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": item["id"], "delta": text})
		streamFixtureEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		streamFixtureEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{"id": fmt.Sprintf("resp_native_%d", n), "object": "response", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 100, "output_tokens": 10, "total_tokens": 110}}})
	}))
	defer model.Close()
	config := fmt.Sprintf(`model = "gpt-5.5"
model_provider = "azure-foundry"
[model_providers.azure-foundry]
name = "Local native history fixture"
base_url = %q
env_key = "AZURE_OPENAI_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0
`, model.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cli := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "codex", args...)
		cmd.Dir = cwd
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("disposable Codex CLI failed: %v\n%s", err, out)
		}
	}
	cli("exec", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "Started in native CLI")
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Fanout(nil), cwd, "codex")
	defer sup.Wait()
	codex, _ := sup.Resolve("codex")
	for key, value := range map[string]string{"authMethod": "azureFoundry", "provider:azure-foundry:apiKey": "local-provider-key", "provider:azure-foundry:envVar": "AZURE_OPENAI_API_KEY", "provider:azure-foundry:models": "gpt-5.5", "permission-mode": "never", "sandbox": "danger-full-access"} {
		if err := codex.Creds.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	api := New(store, hub, sup, reg)
	defer api.Close()
	api.Register(mux)
	first := putNativeProject(t, mux, "project", cwd)
	if len(first.Chats) != 1 || len(first.Agents) != 1 || len(first.SessionDiscoveryIssues) != 0 {
		t.Fatalf("CLI conversation was not discovered: %+v", first)
	}
	chat := first.Chats[0]
	if chat.SessionID == "" || len(store.Messages(chat.ID)) != 0 {
		t.Fatal("discovery did not keep native-only history")
	}
	mu.Lock()
	calls := len(requests)
	mu.Unlock()
	if calls != 1 {
		t.Fatal("listing caused a model request")
	}
	body, _ := json.Marshal(map[string]any{"chatId": chat.ID, "message": "Continued from Mindwire"})
	response := serve(t, mux, "POST", "/turns", string(body))
	if response.Code != 202 {
		t.Fatalf("Mindwire resume: %d %s", response.Code, response.Body.String())
	}
	var run session.Run
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	defer sup.Cancel(run.ID)
	deadline := time.Now().Add(40 * time.Second)
	for sup.Busy(chat.ID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if sup.Busy(chat.ID) {
		sup.Cancel(run.ID)
		t.Fatal("native resume timed out")
	}
	finished, _ := store.GetRun(run.ID)
	if finished.Status != "done" {
		t.Fatalf("native resume status: %+v", finished)
	}
	if store.Session("codex", chat.ID) != chat.SessionID {
		t.Fatal("Mindwire continuation created a different native session")
	}
	cli("exec", "resume", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", chat.SessionID, "Back in native CLI")
	mu.Lock()
	seen := append([]string(nil), requests...)
	mu.Unlock()
	if len(seen) != 3 || !strings.Contains(seen[1], "Native answer 1") || !strings.Contains(seen[2], "Native answer 2") {
		t.Fatalf("continuations did not share native context (requests=%d)", len(seen))
	}
	response = serve(t, mux, "GET", "/workspace?refresh=true", "")
	var after registry.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if len(after.Chats) != 1 || after.Chats[0].ID != chat.ID || after.Chats[0].SessionID != chat.SessionID {
		t.Fatal("CLI continuation duplicated the app chat")
	}
	response = serve(t, mux, "GET", "/chats/"+chat.ID+"/messages", "")
	var history []agent.Message
	if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 3; n++ {
		count := 0
		for _, message := range history {
			if message.Role == "assistant" && message.Text == fmt.Sprintf("Native answer %d", n) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("native answer %d appeared %d times in unified history", n, count)
		}
	}
}
