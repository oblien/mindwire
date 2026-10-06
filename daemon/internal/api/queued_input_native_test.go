package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// Real native executables and local model fixtures, through the production HTTP
// API, runner, receipts and history readers. No paid requests or user sessions.
func TestNativeQueuedInput(t *testing.T) {
	phoneFixture := os.Getenv("MINDWIRE_IPHONE_ATTACHMENT_FIXTURE")
	if os.Getenv("MINDWIRE_NATIVE_INPUT_TEST") != "1" && phoneFixture == "" {
		t.Skip("set MINDWIRE_NATIVE_INPUT_TEST=1 for isolated native CLI input checks")
	}
	for _, harness := range []string{"codex", "claude-code"} {
		if phoneFixture != "" && harness != "codex" {
			continue
		}
		t.Run(harness, func(t *testing.T) {
			binary := harness
			if harness == "claude-code" {
				binary = "claude"
			}
			if _, err := exec.LookPath(binary); err != nil {
				t.Fatal(err)
			}
			cwd, _ := filepath.EvalSymlinks(t.TempDir())
			nativeConfig := t.TempDir()
			t.Setenv("CODEX_HOME", nativeConfig)
			t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
			t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
			t.Setenv("DISABLE_AUTOUPDATER", "1")
			t.Setenv("CLAUDE_CODE_USE_BEDROCK", "0")
			t.Setenv("CLAUDE_CODE_USE_VERTEX", "0")
			t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "0")
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var mu sync.Mutex
			var requests []string
			var filePath string
			fileToolIssued := false
			const fileContents = "MW_UPLOADED_FILE_BYTES: native file read succeeded"
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "count_tokens") {
					writeJSON(w, 200, map[string]int{"input_tokens": 10})
					return
				}
				if r.Method != "POST" {
					writeJSON(w, 200, map[string]any{"data": []any{}})
					return
				}
				body, _ := io.ReadAll(r.Body)
				var req struct {
					Stream   bool              `json:"stream"`
					Messages []json.RawMessage `json:"messages"`
				}
				_ = json.Unmarshal(body, &req)
				main := strings.Contains(string(body), "MW_INPUT_INITIAL") || strings.Contains(string(body), "MW_INPUT_FOLLOW") || strings.Contains(string(body), "MW_INPUT_BLOCK")
				mu.Lock()
				if main {
					requests = append(requests, string(body))
				}
				n := len(requests)
				readFile := main && filePath != "" && strings.Contains(string(body), filePath) && !fileToolIssued
				if readFile {
					fileToolIssued = true
				}
				command := "cat " + agent.ShellQuote(filePath)
				mu.Unlock()
				if phoneFixture != "" && strings.Contains(lastNativeUser(body), "MW_INPUT_BLOCK") {
					<-r.Context().Done()
					return
				}
				if main && n == 1 {
					once.Do(func() { close(started) })
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				text := fmt.Sprintf("Native queued answer %d", n)
				w.Header().Set("Content-Type", "text/event-stream")
				if harness == "codex" {
					item := map[string]any{"id": fmt.Sprintf("msg_queue_%d", n), "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
					if readFile {
						args, _ := json.Marshal(map[string]any{"cmd": command, "max_output_tokens": 1000})
						item = map[string]any{"id": "fc_attachment", "type": "function_call", "call_id": "call_attachment", "name": "exec_command", "arguments": string(args), "status": "completed"}
					}
					streamFixtureEvent(w, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
					if !readFile {
						streamFixtureEvent(w, map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": item["id"], "delta": text})
					}
					streamFixtureEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
					streamFixtureEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{"id": fmt.Sprintf("resp_queue_%d", n), "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 100, "output_tokens": 10, "total_tokens": 110}}})
				} else {
					message := map[string]any{"id": fmt.Sprintf("msg_queue_%d", n), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 100, "output_tokens": 0}}
					if readFile {
						tool := map[string]any{"type": "tool_use", "id": "tool_attachment", "name": "Bash", "input": map[string]string{"command": command}}
						if !req.Stream {
							message["content"] = []any{tool}
							message["stop_reason"] = "tool_use"
							writeJSON(w, 200, message)
							return
						}
						streamFixtureEvent(w, map[string]any{"type": "message_start", "message": message})
						tool["input"] = map[string]any{}
						streamFixtureEvent(w, map[string]any{"type": "content_block_start", "index": 0, "content_block": tool})
						args, _ := json.Marshal(map[string]string{"command": command})
						streamFixtureEvent(w, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": string(args)}})
						streamFixtureEvent(w, map[string]any{"type": "content_block_stop", "index": 0})
						streamFixtureEvent(w, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": 10}})
						streamFixtureEvent(w, map[string]any{"type": "message_stop"})
						return
					}
					if !req.Stream {
						message["content"] = []any{map[string]string{"type": "text", "text": text}}
						message["stop_reason"] = "end_turn"
						writeJSON(w, 200, message)
						return
					}
					streamFixtureEvent(w, map[string]any{"type": "message_start", "message": message})
					streamFixtureEvent(w, map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
					streamFixtureEvent(w, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
					streamFixtureEvent(w, map[string]any{"type": "content_block_stop", "index": 0})
					streamFixtureEvent(w, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 10}})
					streamFixtureEvent(w, map[string]any{"type": "message_stop"})
				}
			}))
			defer model.Close()
			t.Setenv("ANTHROPIC_API_KEY", "local-queued-input-key")
			t.Setenv("ANTHROPIC_BASE_URL", model.URL)
			t.Setenv("AZURE_OPENAI_API_KEY", "local-queued-input-key")
			config := fmt.Sprintf("model = \"gpt-5.5\"\nmodel_provider = \"azure-foundry\"\n[model_providers.azure-foundry]\nname = \"Native queued input fixture\"\nbase_url = %q\nenv_key = \"AZURE_OPENAI_API_KEY\"\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", model.URL)
			if err := os.WriteFile(filepath.Join(nativeConfig, "config.toml"), []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			hub := stream.New()
			sup := orchestrator.New(store, hub, notify.Fanout(nil), cwd, harness)
			ag, _ := sup.Resolve(harness)
			settings := map[string]string{"model": "claude-sonnet-4-6", "permission-mode": "bypassPermissions"}
			if harness == "codex" {
				settings = map[string]string{"authMethod": "azureFoundry", "provider:azure-foundry:apiKey": "local-queued-input-key", "provider:azure-foundry:envVar": "AZURE_OPENAI_API_KEY", "provider:azure-foundry:models": "gpt-5.5", "permission-mode": "never", "sandbox": "danger-full-access"}
			}
			for key, value := range settings {
				if err := ag.Creds.Set(key, value); err != nil {
					t.Fatal(err)
				}
			}
			reg, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer reg.Close()
			a := New(store, hub, sup, reg)
			if a.InitError() != nil {
				t.Fatal(a.InitError())
			}
			defer a.Close()
			mux := http.NewServeMux()
			a.Register(mux)
			if phoneFixture != "" {
				servePhoneAttachmentFixture(t, phoneFixture, a, mux, cwd, started, release,
					func(path string) { mu.Lock(); filePath = path; mu.Unlock() },
					func() string { mu.Lock(); defer mu.Unlock(); return strings.Join(requests, "\n") }, fileContents)
				return
			}
			var first session.Run
			post := func(id, text string, attachments []agent.Attachment) session.Run {
				t.Helper()
				body, _ := json.Marshal(turnReq{RequestID: id, ChatID: "chat", Message: text, Cwd: cwd, QueueIfRunning: true, Options: agent.TurnOptions{Attachments: attachments}})
				res := serve(t, mux, "POST", "/turns", string(body))
				var run session.Run
				if res.Code != 202 || json.Unmarshal(res.Body.Bytes(), &run) != nil {
					t.Fatalf("input admission: %d %s", res.Code, res.Body.String())
				}
				return run
			}
			first = post("initial", "MW_INPUT_INITIAL", nil)
			defer func() { sup.Cancel(first.ID); sup.Wait() }()
			select {
			case <-started:
			case <-time.After(40 * time.Second):
				t.Fatal("native CLI did not reach the local model")
			}
			var pixel bytes.Buffer
			if err := png.Encode(&pixel, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
				t.Fatal(err)
			}
			one := post("follow-one", "MW_INPUT_FOLLOW_ONE", nil)
			upload := func(name, mime string, data []byte) agent.Attachment {
				t.Helper()
				hash := sha256.Sum256(data)
				digest := hex.EncodeToString(hash[:])
				id := digest[:32]
				body, _ := json.Marshal(artifact.UploadChunk{Name: name, Mime: mime, Bytes: len(data), SHA256: digest, Data: data})
				response := serve(t, mux, "PUT", "/chats/chat/attachments/"+id, string(body))
				var state artifact.UploadState
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &state) != nil || !state.Complete {
					t.Fatalf("upload: %d %s", response.Code, response.Body.String())
				}
				return agent.Attachment{ArtifactID: id, Name: name, Mime: mime}
			}
			atts := []agent.Attachment{upload("pixel.png", "image/png", pixel.Bytes()), upload("requirements.txt", "text/plain", []byte(fileContents))}
			_, uploadedPath, err := a.surfaces.Artifacts().Reference("chat", atts[1].ArtifactID)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			filePath = uploadedPath
			mu.Unlock()
			two := post("follow-two", "MW_INPUT_FOLLOW_TWO", atts)
			if one.ID != first.ID || two.ID != first.ID || len(two.Inputs) != 2 {
				t.Fatal("native follow-up spawned another run")
			}
			post("follow-two", "MW_INPUT_FOLLOW_TWO", atts) // lost HTTP acknowledgement retry
			close(release)
			replay, events, done, cancel := sup.SubscribeRun(first.ID, 0)
			defer cancel()
			all := append([]agent.Event(nil), replay...)
			if !done {
				timer := time.NewTimer(45 * time.Second)
				defer timer.Stop()
			reading:
				for {
					select {
					case event, ok := <-events:
						if !ok {
							break reading
						}
						all = append(all, event)
					case <-timer.C:
						t.Fatal("native queue did not finish")
					}
				}
			}
			sup.Wait()
			run, _ := store.GetRun(first.ID)
			if run.Status != "done" || len(run.Inputs) != 2 || run.Inputs[0].Status != "accepted" || run.Inputs[1].Status != "accepted" {
				t.Fatalf("native delivery: %+v", run)
			}
			results, accepted := 0, 0
			for _, event := range all {
				if event.Type == agent.EventResult {
					results++
				}
				if event.Input != nil && event.Input.Status == "accepted" {
					accepted++
				}
			}
			if results != 1 || accepted != 2 {
				t.Fatalf("stream terminal/input counts: %d/%d", results, accepted)
			}
			mu.Lock()
			sent := strings.Join(requests, "\n")
			n := len(requests)
			mu.Unlock()
			if n < 2 || !strings.Contains(sent, "MW_INPUT_FOLLOW_ONE") || !strings.Contains(sent, "MW_INPUT_FOLLOW_TWO") {
				t.Fatalf("native model did not receive follow-ups: requests=%d", n)
			}
			imageKey := "input_image"
			if harness == "claude-code" {
				imageKey = "base64"
			}
			if !strings.Contains(sent, fileContents) {
				t.Fatal("the native harness did not read the uploaded file")
			}
			if data, err := os.ReadFile(uploadedPath); err != nil || string(data) != fileContents {
				t.Fatal("attachment was removed after the turn", err)
			}
			if !strings.Contains(sent, imageKey) {
				t.Fatal("image input was dropped")
			}
			historyResponse := serve(t, mux, "GET", "/chats/chat/messages", "")
			var history []agent.Message
			if historyResponse.Code != 200 || json.Unmarshal(historyResponse.Body.Bytes(), &history) != nil {
				t.Fatalf("history: %d %s", historyResponse.Code, historyResponse.Body.String())
			}
			if dir := os.Getenv("MINDWIRE_INPUT_CAPTURE_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				capture, _ := json.MarshalIndent(map[string]any{"events": all, "messages": history, "run": run, "requests": requests, "recorded": store.Messages("chat")}, "", "  ")
				if err := os.WriteFile(filepath.Join(dir, harness+"-queued-input.json"), capture, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, text := range []string{"MW_INPUT_INITIAL", "MW_INPUT_FOLLOW_ONE", "MW_INPUT_FOLLOW_TWO"} {
				count := 0
				for _, message := range history {
					if message.Role == "user" {
						count += strings.Count(message.Text, text)
					}
				}
				if count != 1 {
					t.Fatalf("native history %s occurs %d times", text, count)
				}
			}
			t.Logf("native queue: %d model requests, %d messages, one final result", n, len(history))
			fork := serve(t, mux, "POST", "/chats/chat/fork", `{"newChatId":"attachment-fork"}`)
			if fork.Code != http.StatusCreated && fork.Code != http.StatusOK {
				t.Fatalf("fork: %d %s", fork.Code, fork.Body.String())
			}
			deleted := serve(t, mux, "DELETE", "/chats/chat", "")
			if deleted.Code != http.StatusOK {
				t.Fatalf("delete source: %d %s", deleted.Code, deleted.Body.String())
			}
			forkHistory := serve(t, mux, "GET", "/chats/attachment-fork/messages", "")
			var inherited []agent.Message
			if forkHistory.Code != 200 || json.Unmarshal(forkHistory.Body.Bytes(), &inherited) != nil {
				t.Fatalf("fork history: %d %s", forkHistory.Code, forkHistory.Body.String())
			}
			files := 0
			for _, message := range inherited {
				if message.Role != "user" {
					continue
				}
				if strings.Contains(message.Text, "\n\nAttached files:") {
					t.Fatal("native file plumbing leaked into the fork's message")
				}
				for _, attachment := range message.Attachments {
					if attachment.ArtifactID != atts[1].ArtifactID {
						continue
					}
					files++
					content, err := a.surfaces.Artifacts().Get(attachment.ArtifactID)
					if err != nil || string(content.Data) != fileContents {
						t.Fatal("source deletion broke the native fork's attachment", err)
					}
				}
			}
			if files != 1 {
				t.Fatalf("fork contains %d copies of the attached file", files)
			}
		})
	}
}
