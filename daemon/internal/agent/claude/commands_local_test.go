package claude

import (
	"context"
	"encoding/json"
	"fmt"
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
)

func TestLocalClaudeCommandsDiscoverAndExecuteNatively(t *testing.T) {
	if os.Getenv("CLAUDE_LOCAL") != "1" {
		t.Skip("set CLAUDE_LOCAL=1 to exercise the installed Claude")
	}
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", t.TempDir())
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	t.Setenv("DISABLE_AUTOUPDATER", "1")
	for _, name := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		t.Setenv(name, "0")
	}
	path := filepath.Join(cwd, ".claude", "commands", "mindwire-check.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ndescription: A local command fixture\nargument-hint: [instructions]\n---\nNative command marker: mindwire-native-command-confirmed. Arguments: $ARGUMENTS\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls, native atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/count_tokens") {
			_ = json.NewEncoder(w).Encode(map[string]int{"input_tokens": 12})
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/messages") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			return
		}
		body, _ := io.ReadAll(r.Body)
		calls.Add(1)
		if strings.Contains(string(body), "mindwire-native-command-confirmed") && strings.Contains(string(body), "selected arguments") {
			native.Add(1)
		}
		var request struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(body, &request)
		message := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 100, "output_tokens": 0}}
		if !request.Stream {
			message["content"] = []any{map[string]string{"type": "text", "text": "Native command complete."}}
			message["stop_reason"] = "end_turn"
			_ = json.NewEncoder(w).Encode(message)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(event map[string]any) {
			data, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
			w.(http.Flusher).Flush()
		}
		emit(map[string]any{"type": "message_start", "message": message})
		emit(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		emit(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "Native command complete."}})
		emit(map[string]any{"type": "content_block_stop", "index": 0})
		emit(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 10}})
		emit(map[string]any{"type": "message_stop"})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	query := agent.CommandQuery{CWD: cwd, Config: map[string]string{"model": "claude-sonnet-4-6", "permission-mode": "bypassPermissions"},
		Env: map[string]string{"ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": server.URL}}
	catalog, err := (adapter{}).Commands(ctx, query)
	if err != nil || catalog.Warning != "" {
		t.Fatalf("discovery: %+v %v", catalog, err)
	}
	found := map[string]bool{}
	for _, command := range catalog.Commands {
		found[command.Name] = true
	}
	if !found["mindwire-check"] || !found["compact"] || !found["permissions"] || !found["context"] || calls.Load() != 0 {
		t.Fatalf("native discovery incomplete or called a model: commands=%v, calls=%d", found, calls.Load())
	}
	input, err := agent.PrepareCommand(ctx, adapter{}, agent.TurnInput{CWD: cwd, Config: query.Config, Env: query.Env, Inbound: make(chan agent.Inbound),
		Options: agent.TurnOptions{Command: &agent.CommandInvocation{Name: "mindwire-check", Arguments: "selected arguments"}}})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := (adapter{}).RunStream(ctx, input, func(agent.Event) {})
	if err != nil || result.IsError || result.Text != "Native command complete." || result.SessionID == "" || native.Load() != 1 {
		t.Fatalf("native dispatch: %+v %v native calls=%d", result, err, native.Load())
	}
	history, err := (adapter{}).History(agent.HistoryQuery{ChatID: "chat", SessionID: result.SessionID, CWD: cwd,
		Recorded: []agent.Message{{ID: "sent", ChatID: "chat", Role: "user", Text: "/mindwire-check selected arguments", CreatedAt: started, Command: input.Options.Command}}})
	if err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, message := range history {
		if message.Role == "user" {
			users = append(users, message.Text)
		}
	}
	if len(users) != 1 || users[0] != "/mindwire-check selected arguments" {
		t.Fatalf("command history duplicated or exposed skill context: %q", users)
	}
	before := calls.Load()
	input.SessionID = result.SessionID
	input.Options.Command = &agent.CommandInvocation{Name: "context"}
	input, err = agent.PrepareCommand(ctx, adapter{}, input)
	if err != nil {
		t.Fatal(err)
	}
	result, err = (adapter{}).RunStream(ctx, input, func(agent.Event) {})
	if err != nil || result.IsError || strings.TrimSpace(result.Text) == "" || calls.Load() != before {
		t.Fatalf("native /context must render without a model turn: %+v %v calls=%d before=%d", result, err, calls.Load(), before)
	}
}
