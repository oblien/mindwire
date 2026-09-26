package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
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
	"github.com/oblien/mindwire/daemon/internal/projectsync"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

type syncNativeWorkspace struct {
	cwd, codex, claude string
	api                *API
	server             *httptest.Server
}

func syncHTTP(t *testing.T, w *syncNativeWorkspace, method, path string, body, out any) int {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := http.NewRequest(method, w.server.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer isolated-sync-fixture")
	r.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, data)
	}
	if out != nil {
		if err = json.Unmarshal(data, out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return response.StatusCode
}

func syncWaitHTTP(t *testing.T, w *syncNativeWorkspace, o projectsync.Operation) projectsync.Operation {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for o.Active() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		syncHTTP(t, w, "GET", "/workspace/sync/operations/"+o.ID, nil, &o)
	}
	if o.Status != "succeeded" {
		t.Fatalf("synchronization: %+v", o)
	}
	return o
}

func syncRelayHTTP(t *testing.T, from, to *syncNativeWorkspace, id string) {
	t.Helper()
	var checkpoint projectsync.Checkpoint
	syncHTTP(t, from, "GET", "/workspace/sync/checkpoints/"+id, nil, &checkpoint)
	for _, parent := range checkpoint.Parents {
		syncRelayHTTP(t, from, to, parent)
	}
	for cursor := 0; ; {
		var page projectsync.ObjectPage
		syncHTTP(t, from, "GET", fmt.Sprintf("/workspace/sync/checkpoints/%s/objects?cursor=%d", id, cursor), nil, &page)
		var missing struct {
			Objects []string `json:"objects"`
		}
		syncHTTP(t, to, "POST", "/workspace/sync/objects/missing", map[string]any{"objects": page.Objects}, &missing)
		for _, object := range missing.Objects {
			var chunk struct {
				Data []byte `json:"data"`
			}
			syncHTTP(t, from, "GET", "/workspace/sync/objects/"+object, nil, &chunk)
			syncHTTP(t, to, "PUT", "/workspace/sync/objects/"+object, chunk, nil)
		}
		if page.Next == 0 {
			break
		}
		cursor = page.Next
	}
	syncHTTP(t, to, "PUT", "/workspace/sync/checkpoints/"+id, checkpoint, nil)
}

func syncTreeDigest(t *testing.T, root string, onlyTranscripts bool) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if onlyTranscripts && !strings.HasSuffix(path, ".jsonl") && !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00", rel)
		h.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Opt-in real CLI tests use isolated native homes and a local deterministic model.
// The HTTP surface is the same one used by the mobile SSH/Oblien transports.
func TestProjectSyncNativeCodexHTTPRoundTrip(t *testing.T) {
	nativeSyncRoundTrip(t, "codex", "CODEX_LOCAL")
}
func TestProjectSyncNativeClaudeHTTPRoundTrip(t *testing.T) {
	nativeSyncRoundTrip(t, "claude-code", "CLAUDE_LOCAL")
}

func nativeSyncRoundTrip(t *testing.T, harness, flag string) {
	if os.Getenv(flag) != "1" {
		t.Skip("set " + flag + "=1 for isolated native project synchronization")
	}
	binary := "codex"
	if harness == "claude-code" {
		binary = "claude"
	}
	if _, err := exec.LookPath(binary); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	t.Setenv("DISABLE_AUTOUPDATER", "1")
	t.Setenv("ANTHROPIC_API_KEY", "local-project-sync-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "0")
	t.Setenv("CLAUDE_CODE_USE_VERTEX", "0")
	t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "0")
	t.Setenv("AZURE_OPENAI_API_KEY", "local-project-sync-key")
	var mu sync.Mutex
	turn, step, cwd := 0, 0, ""
	requests := map[int][]string{}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/count_tokens") {
			writeJSON(w, 200, map[string]int{"input_tokens": 10})
			return
		}
		if r.Method != "POST" || !(strings.HasSuffix(r.URL.Path, "/responses") || strings.HasSuffix(r.URL.Path, "/messages")) {
			writeJSON(w, 200, map[string]any{"data": []any{}})
			return
		}
		body, _ := io.ReadAll(r.Body)
		var request struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(body, &request)
		mu.Lock()
		n, s, dir := turn, step, cwd
		if request.Stream || harness == "codex" {
			requests[n] = append(requests[n], string(body))
			step++
		}
		mu.Unlock()
		text := fmt.Sprintf("Sync native answer %d", n)
		if !request.Stream && harness == "claude-code" {
			writeJSON(w, 200, map[string]any{"id": "auxiliary", "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{map[string]string{"type": "text", "text": "Project sync"}}, "stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 10, "output_tokens": 3}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if harness == "codex" {
			item := map[string]any{"id": fmt.Sprintf("sync_%d_%d", n, s)}
			if s == 0 && n < 3 {
				args, _ := json.Marshal(map[string]any{"cmd": fmt.Sprintf("printf 'shell %d'", n), "shell": "/bin/sh", "login": false, "max_output_tokens": 1000})
				item["type"], item["name"], item["arguments"], item["call_id"] = "function_call", "exec_command", string(args), fmt.Sprintf("shell_%d", n)
			} else if s == 1 && n < 3 {
				item["type"], item["name"], item["input"], item["call_id"] = "custom_tool_call", "apply_patch", fmt.Sprintf("*** Begin Patch\n*** Add File: native-%d.txt\n+Work from native turn %d\n*** End Patch", n, n), fmt.Sprintf("write_%d", n)
			} else {
				item["type"], item["role"], item["phase"], item["status"] = "message", "assistant", "final_answer", "completed"
				item["content"] = []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}
				streamFixtureEvent(w, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
				streamFixtureEvent(w, map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": item["id"], "delta": text})
			}
			streamFixtureEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
			streamFixtureEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{"id": fmt.Sprintf("response_%d_%d", n, s), "object": "response", "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 100, "output_tokens": 10, "total_tokens": 110}}})
			return
		}
		message := map[string]any{"id": fmt.Sprintf("msg_sync_%d_%d", n, s), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 100, "output_tokens": 0}}
		streamFixtureEvent(w, map[string]any{"type": "message_start", "message": message})
		stop := "end_turn"
		if s < 2 && n < 3 {
			name := "Write"
			input := map[string]any{"file_path": filepath.Join(dir, fmt.Sprintf("native-%d.txt", n)), "content": fmt.Sprintf("Work from native turn %d\n", n)}
			if s == 1 {
				name = "Bash"
				input = map[string]any{"command": fmt.Sprintf("printf 'shell %d'", n), "description": "Check the fixture shell"}
			}
			data, _ := json.Marshal(input)
			streamFixtureEvent(w, map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("tool_sync_%d_%d", n, s), "name": name, "input": map[string]any{}}})
			streamFixtureEvent(w, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": string(data)}})
			stop = "tool_use"
		} else {
			streamFixtureEvent(w, map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
			streamFixtureEvent(w, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
		}
		streamFixtureEvent(w, map[string]any{"type": "content_block_stop", "index": 0})
		streamFixtureEvent(w, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 10}})
		streamFixtureEvent(w, map[string]any{"type": "message_stop"})
	}))
	defer model.Close()
	t.Setenv("ANTHROPIC_BASE_URL", model.URL)
	activate := func(w *syncNativeWorkspace) { t.Setenv("CODEX_HOME", w.codex); t.Setenv("CLAUDE_CONFIG_DIR", w.claude) }
	makeWorkspace := func() *syncNativeWorkspace {
		root, _ := filepath.EvalSymlinks(t.TempDir())
		w := &syncNativeWorkspace{cwd: filepath.Join(root, "project"), codex: filepath.Join(root, "codex"), claude: filepath.Join(root, "claude")}
		for _, dir := range []string{w.cwd, w.codex, w.claude} {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		config := fmt.Sprintf("model = \"gpt-5.5\"\nmodel_provider = \"azure-foundry\"\n[model_providers.azure-foundry]\nname = \"Local sync fixture\"\nbase_url = %q\nenv_key = \"AZURE_OPENAI_API_KEY\"\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", model.URL)
		if err := os.WriteFile(filepath.Join(w.codex, "config.toml"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		activate(w)
		st, err := session.Open(filepath.Join(root, "state", "sessions.json"))
		if err != nil {
			t.Fatal(err)
		}
		reg, err := registry.Open(filepath.Join(root, "state", "workspace.db"))
		if err != nil {
			t.Fatal(err)
		}
		hub := stream.New()
		sup := orchestrator.New(st, hub, notify.Fanout(nil), w.cwd, harness)
		a, _ := sup.Resolve(harness)
		settings := map[string]string{"model": "claude-sonnet-4-6", "permission-mode": "bypassPermissions", "authMethod": "apiKey", "apiKey": "local-project-sync-key", "baseUrl": model.URL}
		if harness == "codex" {
			settings = map[string]string{"model": "gpt-5.5", "authMethod": "azureFoundry", "provider:azure-foundry:apiKey": "local-project-sync-key", "provider:azure-foundry:envVar": "AZURE_OPENAI_API_KEY", "provider:azure-foundry:models": "gpt-5.5", "permission-mode": "never", "sandbox": "danger-full-access"}
		}
		for k, v := range settings {
			if err = a.Creds.Set(k, v); err != nil {
				t.Fatal(err)
			}
		}
		w.api = New(st, hub, sup, reg)
		if err = w.api.InitError(); err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		w.api.Register(mux)
		w.server = httptest.NewServer(Auth("isolated-sync-fixture", mux))
		t.Cleanup(func() { w.server.Close(); w.api.Close(); sup.Wait(); reg.Close() })
		return w
	}
	x, y := makeWorkspace(), makeWorkspace()
	setTurn := func(w *syncNativeWorkspace, n int) {
		activate(w)
		mu.Lock()
		turn, step, cwd = n, 0, w.cwd
		mu.Unlock()
	}
	cli := func(w *syncNativeWorkspace, sid string, n int) {
		t.Helper()
		setTurn(w, n)
		prompt := fmt.Sprintf("sync-fixture turn %d", n)
		args := []string{"exec", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", prompt}
		if sid != "" {
			args = []string{"exec", "resume", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", sid, prompt}
		}
		if harness == "claude-code" {
			args = []string{"-p", prompt, "--verbose", "--output-format", "stream-json", "--model", "claude-sonnet-4-6", "--dangerously-skip-permissions"}
			if sid != "" {
				args = append(args, "--resume", sid)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = w.cwd
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native %s: %v\n%s", harness, err, out)
		}
	}
	cli(x, "", 1)
	var original registry.Snapshot
	syncHTTP(t, x, "PUT", "/workspace/projects/x", map[string]any{"record": registry.Project{Record: registry.Record{ID: "x"}, Name: "Native project", Path: x.cwd}}, &original)
	if len(original.Chats) != 1 {
		t.Fatalf("native chat missing: %+v", original)
	}
	chat := original.Chats[0]
	nativeHome := func(w *syncNativeWorkspace) string {
		if harness == "codex" {
			return w.codex
		}
		return w.claude
	}
	beforeFiles, beforeNative := syncTreeDigest(t, x.cwd, false), syncTreeDigest(t, nativeHome(x), true)
	var exported projectsync.Operation
	syncHTTP(t, x, "POST", "/workspace/sync/exports", projectsync.Request{ID: "x-export", ProjectID: "x"}, &exported)
	exported = syncWaitHTTP(t, x, exported)
	activate(y)
	syncRelayHTTP(t, x, y, exported.ResultCheckpointID)
	var imported projectsync.Operation
	syncHTTP(t, y, "POST", "/workspace/sync/imports", projectsync.Request{ID: "y-import", CheckpointID: exported.ResultCheckpointID, Path: y.cwd}, &imported)
	imported = syncWaitHTTP(t, y, imported)
	if beforeFiles != syncTreeDigest(t, x.cwd, false) || beforeNative != syncTreeDigest(t, nativeHome(x), true) {
		t.Fatal("source files or transcripts changed while switching")
	}
	var target registry.Snapshot
	syncHTTP(t, y, "GET", "/workspace?refresh=true", nil, &target)
	if len(target.Chats) != 1 || target.Chats[0].SessionID != chat.SessionID {
		t.Fatalf("native identity lost: %+v", target)
	}
	setTurn(y, 2)
	var run session.Run
	syncHTTP(t, y, "POST", "/turns", map[string]any{"chatId": target.Chats[0].ID, "message": "sync-fixture turn 2"}, &run)
	deadline := time.Now().Add(45 * time.Second)
	for y.api.sup.Busy(run.ChatID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if y.api.sup.Busy(run.ChatID) {
		y.api.sup.Cancel(run.ID)
		t.Fatal("destination native resume timed out")
	}
	finished, _ := y.api.store.GetRun(run.ID)
	if finished.Status != "done" {
		t.Fatalf("destination resume: %+v", finished)
	}
	if err := os.WriteFile(filepath.Join(x.cwd, "independent-X.txt"), []byte("keep X independent work"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(y.cwd, "independent-Y.txt"), []byte("bring back Y work"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeY := syncTreeDigest(t, y.cwd, false)
	syncHTTP(t, y, "POST", "/workspace/sync/exports", projectsync.Request{ID: "y-export", ProjectID: imported.ResultProjectID}, &exported)
	exported = syncWaitHTTP(t, y, exported)
	activate(x)
	syncRelayHTTP(t, y, x, exported.ResultCheckpointID)
	syncHTTP(t, x, "POST", "/workspace/sync/imports", projectsync.Request{ID: "x-import", CheckpointID: exported.ResultCheckpointID}, &imported)
	imported = syncWaitHTTP(t, x, imported)
	// Simulate a lost final HTTP acknowledgement: same ID returns the same commit.
	var replay projectsync.Operation
	syncHTTP(t, x, "POST", "/workspace/sync/imports", projectsync.Request{ID: "x-import", CheckpointID: exported.ResultCheckpointID}, &replay)
	if replay.ResultCheckpointID != imported.ResultCheckpointID {
		t.Fatal("lost acknowledgement duplicated the import")
	}
	for _, name := range []string{"native-1.txt", "native-2.txt", "independent-X.txt", "independent-Y.txt"} {
		if _, err := os.Stat(filepath.Join(x.cwd, name)); err != nil {
			t.Fatalf("lost code %s: %v", name, err)
		}
	}
	if beforeY != syncTreeDigest(t, y.cwd, false) {
		t.Fatal("return synchronization changed Y")
	}
	cli(x, chat.SessionID, 3)
	var history []agent.Message
	syncHTTP(t, x, "GET", "/chats/"+chat.ID+"/messages", nil, &history)
	tools := 0
	for n := 1; n <= 3; n++ {
		count := 0
		for _, m := range history {
			if m.Role == "assistant" && strings.Contains(m.Text, fmt.Sprintf("Sync native answer %d", n)) {
				count++
			}
			if n == 1 {
				for _, part := range m.Parts {
					if part.Type == "tool" {
						tools++
					}
				}
			}
		}
		if count != 1 {
			t.Fatalf("native answer %d appears %d times", n, count)
		}
	}
	if tools < 4 {
		t.Fatalf("native tool history lost: %d tool cards", tools)
	}
	mu.Lock()
	seen2, seen3 := strings.Join(requests[2], "\n"), strings.Join(requests[3], "\n")
	mu.Unlock()
	if !strings.Contains(seen2, "Sync native answer 1") || !strings.Contains(seen3, "Sync native answer 2") {
		t.Fatal("resumed native model requests lost context")
	}
	var final registry.Snapshot
	syncHTTP(t, x, "GET", "/workspace?refresh=true", nil, &final)
	if len(final.Chats) != 1 || final.Chats[0].ID != chat.ID {
		t.Fatal("return synchronization duplicated native chat membership")
	}
}
