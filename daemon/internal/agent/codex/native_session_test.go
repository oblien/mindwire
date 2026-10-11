package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestNativeCodexAttachmentSnapshotBarrierAndControls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shared Codex control uses a Unix socket")
	}
	dir, err := os.MkdirTemp("", "mw-native-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	// Keep the Unix socket path within macOS's 104-byte limit.
	if len(dir) > 65 {
		os.RemoveAll(dir)
		dir, err = os.MkdirTemp("/tmp", "mw-native-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
	}
	socket := filepath.Join(dir, "control.sock")
	t.Setenv("MINDWIRE_CODEX_SOCKET", socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cwd := t.TempDir()
	thread := map[string]any{"id": "thread-1", "cwd": cwd, "status": map[string]any{"type": "active"}}
	var writeMu sync.Mutex
	var peer *websocket.Conn
	ready := make(chan struct{})
	calls := make(chan rpcIn, 16)
	write := func(v any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := wsjson.Write(ctx, peer, v); err != nil && ctx.Err() == nil {
			t.Errorf("fixture write: %v", err)
		}
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		peer = conn
		close(ready)
		for {
			var call rpcIn
			if wsjson.Read(ctx, conn, &call) != nil {
				return
			}
			if call.Method == "initialized" {
				continue
			}
			if call.Method == "" {
				calls <- call
				continue
			}
			result := map[string]any{}
			switch call.Method {
			case "initialize":
			case "thread/read":
				result["thread"] = thread
			case "thread/resume":
				var p map[string]json.RawMessage
				_ = json.Unmarshal(call.Params, &p)
				if len(p) != 3 || string(p["excludeTurns"]) != "true" {
					t.Errorf("attachment changed owner settings: %s", call.Params)
				}
				var page struct {
					Limit int `json:"limit"`
				}
				_ = json.Unmarshal(p["initialTurnsPage"], &page)
				if page.Limit != 1 {
					t.Error("attachment loaded more than the recent turn")
				}
				write(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "answer", "delta": "stale"}})
				result = map[string]any{"thread": thread, "initialTurnsPage": map[string]any{"data": []any{map[string]any{"id": "turn-1", "status": "inProgress", "items": []any{map[string]any{"id": "answer", "type": "agentMessage", "text": "Before"}}}}}}
			case "turn/start":
				calls <- call
				var p map[string]any
				_ = json.Unmarshal(call.Params, &p)
				if len(p) != 3 {
					t.Errorf("turn changed native settings: %s", call.Params)
				}
				result["turn"] = map[string]any{"id": "turn-1"}
			case "turn/interrupt":
				calls <- call
				write(map[string]any{"id": call.ID, "error": map[string]any{"code": -32600, "message": "expected active turn id old but found turn-1"}})
				continue
			default:
				t.Errorf("unexpected native request: %s", call.Method)
			}
			write(map[string]any{"id": call.ID, "result": result})
		}
	})}
	go func() { _ = server.Serve(ln) }()
	defer server.Close()
	connection, err := (adapter{}).OpenNativeSession(ctx, agent.NativeSessionQuery{SessionID: "thread-1", CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	<-ready
	initial := connection.Initial()
	if initial.Activity.State != "running" || initial.Activity.TurnID != "turn-1" || len(initial.Events) != 1 || initial.Events[0].Text != "Before" {
		t.Fatalf("initial native state: %+v", initial)
	}
	write(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1"}}})
	write(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "other-thread", "turnId": "other-turn", "itemId": "answer", "delta": "wrong"}})
	write(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "answer", "delta": " after"}})
	select {
	case ev := <-connection.Updates():
		if ev.Reset || ev.Event == nil || ev.Event.Text != " after" {
			t.Fatalf("stale, duplicate or cross-thread event: %+v", ev)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	receipt, err := connection.Action(ctx, agent.NativeSessionAction{RequestID: "phone-input", Kind: "input", Text: "Continue"})
	if err != nil || receipt.TurnID != "turn-1" {
		t.Fatalf("shared input: %+v %v", receipt, err)
	}
	if call := <-calls; call.Method != "turn/start" {
		t.Fatalf("input started competing session: %s", call.Method)
	}
	_, err = connection.Action(ctx, agent.NativeSessionAction{RequestID: "stop-missing", Kind: "interrupt"})
	if err == nil {
		t.Fatal("untargeted interrupt accepted")
	}
	_, err = connection.Action(ctx, agent.NativeSessionAction{RequestID: "stop-stale", Kind: "interrupt", ExpectedTurnID: "old"})
	var native *agent.NativeSessionError
	if !errors.As(err, &native) || native.Code != "native_turn_changed" {
		t.Fatalf("stale stop: %v", err)
	}
	<-calls
	write(map[string]any{"id": "approval-1", "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "command-1", "command": "pwd"}})
	var prompt *agent.Interaction
	select {
	case ev := <-connection.Updates():
		prompt = ev.Event.Interaction
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if prompt == nil {
		t.Fatal("native question missing")
	}
	if len(prompt.Options) == 0 {
		t.Fatal("missing approval choices")
	}
	action := agent.NativeSessionAction{Kind: "response", RequestID: "reply-one", Response: &agent.InteractionResponse{InteractionID: prompt.ID, Decision: prompt.Options[0].ID}}
	receipt, err = connection.Action(ctx, action)
	if err != nil || receipt.Status != "submitted" {
		t.Fatalf("answer: %+v %v", receipt, err)
	}
	<-calls
	action.RequestID = "reply-two"
	if _, err = connection.Action(ctx, action); err == nil {
		t.Fatal("question was answered twice before resolution")
	}
	write(map[string]any{"method": "serverRequest/resolved", "params": map[string]any{"threadId": "thread-1", "requestId": "approval-1"}})
	select {
	case ev := <-connection.Updates():
		if ev.Event.Interaction == nil || ev.Event.Interaction.NeedsResponse {
			t.Fatal("other-client/native resolution was not reflected")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	connection.Close()
	select {
	case call := <-calls:
		t.Fatalf("detach controlled native owner: %s", call.Method)
	default:
	}
}
