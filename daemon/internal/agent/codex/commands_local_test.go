package codex

import (
	"context"
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

// The official CLI discovers a real repository skill and receives its native skill
// input. Inference is replaced by a localhost fixture; no user's sessions are read.
func TestLocalCodexCommandDiscoveryAndSkillDispatch(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 to exercise the installed Codex")
	}
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", t.TempDir())
	path := filepath.Join(cwd, ".agents", "skills", "mindwire-check", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: mindwire-check\ndescription: A local command fixture\n---\nNative command marker: mindwire-native-skill-confirmed.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		calls.Add(1)
		for _, want := range []string{"mindwire-native-skill-confirmed", "selected arguments"} {
			if !strings.Contains(string(body), want) {
				t.Errorf("native skill dispatch omitted %q", want)
			}
		}
		writeLocalReply(w, 1, "Native skill complete.")
	}))
	defer server.Close()
	config := fmt.Sprintf("model = \"gpt-5.5\"\nmodel_provider = \"local\"\n[model_providers.local]\nname = \"Local commands fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", server.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	query := agent.CommandQuery{CWD: cwd, Env: map[string]string{"CODEX_API_KEY": "fixture-key", "OPENAI_BASE_URL": server.URL}}
	catalog, err := (adapter{}).Commands(ctx, query)
	if err != nil || catalog.Warning != "" {
		t.Fatalf("discovery: %+v, %v", catalog, err)
	}
	found := false
	for _, command := range catalog.Commands {
		if command.Name == "mindwire-check" {
			found = command.Kind == "prompt" && command.SkillPath == path
		}
	}
	if !found || calls.Load() != 0 {
		t.Fatalf("skill missing or discovery called the model: found=%t calls=%d", found, calls.Load())
	}
	input, err := agent.PrepareCommand(ctx, adapter{}, agent.TurnInput{CWD: cwd, Env: query.Env,
		Config:  map[string]string{keyModel: "gpt-5.5", keyEffort: "low"},
		Options: agent.TurnOptions{Command: &agent.CommandInvocation{Name: "mindwire-check", Arguments: "selected arguments"}}})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := (adapter{}).RunStream(ctx, input, func(agent.Event) {})
	if err != nil || result.IsError || result.Text != "Native skill complete." || result.SessionID == "" {
		t.Fatalf("dispatch: %+v %v", result, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("native command was submitted %d times", calls.Load())
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
	query.CWD = t.TempDir()
	other, err := (adapter{}).Commands(ctx, query)
	if err != nil || other.Warning != "" {
		t.Fatalf("other project: %+v %v", other, err)
	}
	for _, command := range other.Commands {
		if command.Name == "mindwire-check" {
			t.Fatal("repository skill leaked to another project")
		}
	}
}
