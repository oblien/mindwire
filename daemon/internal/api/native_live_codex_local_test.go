package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/nativesessions"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// Native PC owner -> real Codex shared server -> HTTP watcher and ordinary
// Mindwire /turns -> same native turn. Everything uses disposable homes and a
// local model fixture; this must never touch the developer's real sessions.
func TestNativeCodexLiveOwnerHTTP(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 for real Codex shared-owner interoperability")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Unix control socket")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	home, err := os.MkdirTemp("/tmp", "mw-live-codex-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	cwd := t.TempDir()
	cwd, _ = filepath.EvalSymlinks(cwd)
	socket := filepath.Join(home, "control.sock")
	t.Setenv("CODEX_HOME", home)
	t.Setenv("MINDWIRE_CODEX_SOCKET", socket)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("AZURE_OPENAI_API_KEY", "local-native-key")
	requests := make(chan string, 8)
	firstComplete := make(chan struct{})
	watching := make(chan struct{})
	var finishOnce sync.Once
	finishFirst := func() { finishOnce.Do(func() { close(firstComplete) }) }
	defer finishFirst()
	var modelMu sync.Mutex
	number := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		modelMu.Lock()
		number++
		n := number
		modelMu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		text := fmt.Sprintf("Working from PC %d", n)
		item := map[string]any{"id": fmt.Sprintf("msg_live_%d", n), "type": "message", "role": "assistant", "phase": "commentary", "status": "in_progress", "content": []any{}}
		streamFixtureEvent(w, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
		streamFixtureEvent(w, map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": item["id"], "delta": text})
		w.(http.Flusher).Flush()
		select {
		case requests <- string(body):
		case <-ctx.Done():
			return
		}
		if n == 1 {
			select {
			case <-watching:
			case <-r.Context().Done():
				return
			case <-ctx.Done():
				return
			}
			streamFixtureEvent(w, map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": item["id"], "delta": " live"})
			text += " live"
			select {
			case <-firstComplete:
			case <-r.Context().Done():
				return
			case <-ctx.Done():
				return
			}
		} else {
			select {
			case <-r.Context().Done():
				return
			case <-ctx.Done():
				return
			}
		}
		item["status"] = "completed"
		item["content"] = []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}
		streamFixtureEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		streamFixtureEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{"id": "response-live-1", "object": "response", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 100, "output_tokens": 12, "total_tokens": 112}}})
	}))
	defer model.Close()
	config := fmt.Sprintf("model = \"gpt-5.5\"\nmodel_provider = \"local\"\n[model_providers.local]\nname = \"Local native owner test\"\nbase_url = %q\nenv_key = \"AZURE_OPENAI_API_KEY\"\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", model.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(home, "native.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	binary := agent.FirstNonEmpty(os.Getenv("CODEX_NATIVE_BINARY"), "codex")
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "unix://"+socket)
	cmd.Dir = cwd
	cmd.Stdout = log
	cmd.Stderr = log
	proc.Group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- cmd.Wait() }()
	defer func() {
		cancel()
		select {
		case <-processDone:
		case <-time.After(5 * time.Second):
			t.Error("isolated native process did not stop")
		}
	}()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	var pc *websocket.Conn
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		pc, _, err = websocket.Dial(ctx, "ws://localhost", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		data, _ := os.ReadFile(filepath.Join(home, "native.log"))
		t.Fatalf("native server: %v\n%s", err, data)
	}
	defer pc.CloseNow()
	pc.SetReadLimit(16 << 20)
	sequence := 0
	rpc := func(method string, params any) map[string]json.RawMessage {
		t.Helper()
		sequence++
		id := sequence
		if err := wsjson.Write(ctx, pc, map[string]any{"id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		for {
			var m struct {
				Method string                     `json:"method"`
				ID     json.RawMessage            `json:"id"`
				Result map[string]json.RawMessage `json:"result"`
				Error  json.RawMessage            `json:"error"`
			}
			if err := wsjson.Read(ctx, pc, &m); err != nil {
				t.Fatal(err)
			}
			if string(m.ID) != fmt.Sprint(id) {
				continue
			}
			if len(m.Error) > 0 {
				t.Fatalf("PC %s: %s", method, m.Error)
			}
			return m.Result
		}
	}
	rpc("initialize", map[string]any{"clientInfo": map[string]any{"name": "pc-native-fixture", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}})
	if err := wsjson.Write(ctx, pc, map[string]any{"method": "initialized"}); err != nil {
		t.Fatal(err)
	}
	threadResult := rpc("thread/start", map[string]any{"cwd": cwd, "model": "gpt-5.5", "approvalPolicy": "never", "sandbox": "danger-full-access"})
	var thread struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(threadResult["thread"], &thread)
	turnResult := rpc("turn/start", map[string]any{"threadId": thread.ID, "input": []any{map[string]any{"type": "text", "text": "Started from the PC terminal"}}})
	var turn struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(turnResult["turn"], &turn)
	select {
	case <-requests:
	case <-ctx.Done():
		t.Fatal("native PC did not start the local model")
	}
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSession("codex", "live-chat", thread.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatCWD("live-chat", cwd); err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Fanout(nil), cwd, "codex")
	api := New(store, hub, sup)
	defer api.Close()
	defer sup.Wait()
	mux := http.NewServeMux()
	api.Register(mux)
	host := httptest.NewServer(Auth("fixture-token", mux))
	defer host.Close()
	watch := func() (nativesessions.Snapshot, io.ReadCloser, *bufio.Scanner) {
		t.Helper()
		watchCtx, stopWatch := context.WithTimeout(ctx, 10*time.Second)
		t.Cleanup(stopWatch)
		req, _ := http.NewRequestWithContext(watchCtx, "GET", host.URL+"/chats/live-chat/native/events?agent=codex", nil)
		req.Header.Set("Authorization", "Bearer fixture-token")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("native watch: %d %s", response.StatusCode, data)
		}
		scan := bufio.NewScanner(response.Body)
		scan.Buffer(make([]byte, 4096), 4<<20)
		for scan.Scan() {
			if !strings.HasPrefix(scan.Text(), "data: ") {
				continue
			}
			var frame nativesessions.Frame
			if err := json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &frame); err != nil {
				t.Fatal(err)
			}
			if frame.Snapshot == nil {
				t.Fatal("missing initial snapshot")
			}
			return *frame.Snapshot, response.Body, scan
		}
		t.Fatalf("missing native snapshot: %v", scan.Err())
		return nativesessions.Snapshot{}, nil, nil
	}
	base, body, scan := watch()
	defer body.Close()
	if base.Activity.Owner != "native" || base.Activity.State != "running" || base.TurnID != turn.ID {
		t.Fatalf("native running state: %+v", base)
	}
	encoded, _ := json.Marshal(base.Parts)
	close(watching)
	// Codex's native bootstrap currently omits an unfinished text block's
	// earlier deltas. New deltas stream immediately; item/completed restores the
	// full block. This fixture tests the real protocol rather than inventing a
	// replay capability the harness does not expose.
	for !strings.Contains(string(encoded), " live") && scan.Scan() {
		if strings.HasPrefix(scan.Text(), "data: ") {
			encoded = append(encoded, []byte(scan.Text())...)
		}
	}
	if !strings.Contains(string(encoded), " live") {
		t.Fatalf("native output was not visible: %s", encoded)
	}
	other, otherBody, _ := watch()
	if other.ConnectionID != base.ConnectionID {
		t.Fatal("second viewer duplicated attachment")
	}
	otherBody.Close()
	body.Close()
	// This owner was launched by the terminal, so there is no daemon run for
	// the ordinary Busy guard. Native history must still be protected.
	for _, mutation := range []struct{ method, path, body string }{
		{"DELETE", "/chats/live-chat", ""},
		{"POST", "/chats/live-chat/fork", `{"newChatId":"unsafe-fork"}`},
	} {
		response := serve(t, mux, mutation.method, mutation.path, mutation.body)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "native_session_owned") {
			t.Fatalf("native owner mutation guard: %d %s", response.Code, response.Body.String())
		}
	}
	// Existing clients sending /turns join the active owner, without requiring a
	// separate Mindwire harness login, model configuration, or managed CLI spawn.
	sent := serve(t, mux, "POST", "/turns?agent=codex", fmt.Sprintf(`{"chatId":"live-chat","cwd":%q,"requestId":"phone-send","message":"Follow up from the phone"}`, cwd))
	if sent.Code != 202 {
		t.Fatalf("send into PC turn: %d %s", sent.Code, sent.Body.String())
	}
	var run session.Run
	_ = json.Unmarshal(sent.Body.Bytes(), &run)
	defer sup.Cancel(run.ID)
	// Wait for the actual native acknowledgement, not just daemon admission.
	replay, acceptedEvents, _, detachReceipt := hub.Subscribe(run.ID)
	defer detachReceipt()
	acknowledged := false
	for _, ev := range replay {
		if ev.Meta["nativeInputAccepted"] == true {
			acknowledged = true
		}
	}
	for !acknowledged {
		select {
		case ev, ok := <-acceptedEvents:
			if !ok {
				finished, _ := store.GetRun(run.ID)
				t.Fatalf("native send ended before acknowledgement: %+v", finished)
			}
			acknowledged = ev.Meta["nativeInputAccepted"] == true
		case <-ctx.Done():
			t.Fatal("native input was not acknowledged")
		}
	}
	finishFirst()
	select {
	case request := <-requests:
		if !strings.Contains(request, "Follow up from the phone") {
			t.Fatal("native continuation lost the phone input")
		}
	case <-ctx.Done():
		t.Fatal("native steering did not continue")
	}
	if store.Session("codex", "live-chat") != thread.ID {
		t.Fatal("phone created a competing native conversation")
	}
	wrong := agent.NativeSessionAction{ConnectionID: base.ConnectionID, RequestID: "wrong-stop", Kind: "interrupt", ExpectedTurnID: "not-this-turn"}
	raw, _ := json.Marshal(wrong)
	rejected := serve(t, mux, "POST", "/chats/live-chat/native/actions?agent=codex", string(raw))
	if rejected.Code != 409 {
		t.Fatalf("stale stop accepted: %d %s", rejected.Code, rejected.Body.String())
	}
	if !sup.Cancel(run.ID) {
		t.Fatal("phone turn was not controllable")
	}
	for sup.Busy("live-chat") && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	finished, _ := store.GetRun(run.ID)
	if finished.Status != "cancelled" {
		t.Fatalf("stop state: %+v", finished)
	}
	// The shared server and original session still belong to the PC.
	rpc("thread/read", map[string]any{"threadId": thread.ID, "includeTurns": false})
	rpc("thread/start", map[string]any{"cwd": cwd, "model": "gpt-5.5", "approvalPolicy": "never", "sandbox": "danger-full-access"})
	select {
	case err := <-processDone:
		t.Fatalf("phone stopped PC server: %v", err)
	default:
	}
}
