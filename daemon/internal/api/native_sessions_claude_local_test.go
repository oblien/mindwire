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
)

func TestNativeClaudeCLIAdapterRoundTrip(t *testing.T) {
	if os.Getenv("CLAUDE_LOCAL") != "1" {
		t.Skip("set CLAUDE_LOCAL=1 for the native Claude interoperability check")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatal(err)
	}
	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	t.Setenv("DISABLE_AUTOUPDATER", "1")
	t.Setenv("ANTHROPIC_API_KEY", "local-native-fixture-key")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "0")
	t.Setenv("CLAUDE_CODE_USE_VERTEX", "0")
	t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "0")
	var mu sync.Mutex
	var requests []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/count_tokens") {
			writeJSON(w, 200, map[string]int{"input_tokens": 10})
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/messages") {
			writeJSON(w, 200, map[string]any{"data": []any{}})
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Stream   bool              `json:"stream"`
			Messages []json.RawMessage `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		last := ""
		if len(req.Messages) > 0 {
			last = string(req.Messages[len(req.Messages)-1])
		}
		main := strings.Contains(last, "Started in native CLI") || strings.Contains(last, "Continued from Mindwire") || strings.Contains(last, "Back in native CLI")
		mu.Lock()
		if main {
			requests = append(requests, string(body))
		}
		n := len(requests)
		mu.Unlock()
		text := fmt.Sprintf("Native answer %d", n)
		message := map[string]any{"id": fmt.Sprintf("msg_native_%d", n), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 100, "output_tokens": 0}}
		if !req.Stream {
			message["content"] = []any{map[string]string{"type": "text", "text": text}}
			message["stop_reason"] = "end_turn"
			writeJSON(w, 200, message)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		streamFixtureEvent(w, map[string]any{"type": "message_start", "message": message})
		streamFixtureEvent(w, map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		streamFixtureEvent(w, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
		streamFixtureEvent(w, map[string]any{"type": "content_block_stop", "index": 0})
		streamFixtureEvent(w, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 10}})
		streamFixtureEvent(w, map[string]any{"type": "message_stop"})
	}))
	defer model.Close()
	t.Setenv("ANTHROPIC_BASE_URL", model.URL)
	cli := func(sessionID, prompt string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		args := []string{"-p", prompt, "--verbose", "--output-format", "stream-json", "--model", "claude-sonnet-4-6", "--dangerously-skip-permissions"}
		if sessionID != "" {
			args = append(args, "--resume", sessionID)
		}
		cmd := exec.CommandContext(ctx, "claude", args...)
		cmd.Dir = cwd
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("disposable Claude CLI failed: %v\n%s", err, out)
		}
	}
	cli("", "Started in native CLI")
	var adapter agent.Adapter
	for _, a := range agent.All() {
		if a.ID() == "claude-code" {
			adapter = a
			break
		}
	}
	if adapter == nil {
		t.Fatal("Claude adapter missing")
	}
	rows, err := adapter.(agent.SessionLister).ListSessions(context.Background(), cwd)
	if err != nil || len(rows) != 1 {
		t.Fatalf("native Claude discovery: count=%d err=%v", len(rows), err)
	}
	sid := rows[0].ID
	mu.Lock()
	calls := len(requests)
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("listing called a model (requests=%d)", calls)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	result, err := adapter.RunStream(ctx, agent.TurnInput{CWD: cwd, SessionID: sid, Message: "Continued from Mindwire", Config: map[string]string{"model": "claude-sonnet-4-6", "permission-mode": "bypassPermissions"}, Env: map[string]string{"ANTHROPIC_API_KEY": "local-native-fixture-key", "ANTHROPIC_BASE_URL": model.URL}}, func(agent.Event) {})
	if err != nil || result.IsError || result.SessionID != sid {
		t.Fatalf("native Claude resume: %+v %v", result, err)
	}
	cli(sid, "Back in native CLI")
	mu.Lock()
	seen := append([]string(nil), requests...)
	mu.Unlock()
	if len(seen) != 3 || !strings.Contains(seen[1], "Native answer 1") || !strings.Contains(seen[2], "Native answer 2") {
		t.Fatalf("Claude native context was not reused (requests=%d)", len(seen))
	}
	rows, err = adapter.(agent.SessionLister).ListSessions(context.Background(), cwd)
	if err != nil || len(rows) != 1 || rows[0].ID != sid {
		t.Fatal("Claude continuation created a duplicate session")
	}
	history, err := adapter.History(agent.HistoryQuery{ChatID: "app-reference", SessionID: sid, CWD: cwd})
	if err != nil {
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
			t.Fatalf("native Claude answer %d appeared %d times", n, count)
		}
	}
}
