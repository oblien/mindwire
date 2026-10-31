package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

func TestRegisteredModelKeepsSubscriptionAndConfig(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	original := "model = \"native-default\"\nmodel_provider = \"openai\"\n"
	if err := os.WriteFile(mcpConfigPath(), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	store := mapStore{ckMethod: "login"}
	if err := (adapter{}).SetProvider(store, agent.MemoryUser, "", "openship-test", agent.CustomProvider{BaseURL: "https://model.example/v1", Models: []string{"custom/model"}}, "fixture-only", nil); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(mcpConfigPath())
	in := agent.TurnInput{Config: map[string]string{keyModel: "openship-test/custom/model"}, Env: newAuth(store).EnvForRun()}
	in.Env["CODEX_ACCESS_TOKEN"] = "native-fixture"
	selected := registeredModel(in)
	if selected.Config[keyModel] != "custom/model" || selected.Env[azureProviderMarker] != "openship-test" || selected.Env["OPENSHIP_TEST_API_KEY"] != "fixture-only" || selected.Env["CODEX_ACCESS_TOKEN"] != "" {
		t.Fatalf("incorrect custom route")
	}
	if in.Config[keyModel] != "openship-test/custom/model" || in.Env[azureProviderMarker] != "openai" || in.Env["CODEX_ACCESS_TOKEN"] != "native-fixture" {
		t.Fatal("changed caller-owned run inputs")
	}
	if !strings.Contains(newAppServer(selected, materialized{}).command, "model_provider=openship-test") || !strings.Contains(buildExecCommand(selected, materialized{}), "model_provider=openship-test") {
		t.Fatal("both transports must select the registered provider")
	}
	unknown := agent.TurnInput{Config: map[string]string{keyModel: "ordinary/model"}}
	if !reflect.DeepEqual(unknown, registeredModel(unknown)) {
		t.Fatal("changed an ordinary model")
	}
	after, _ := os.ReadFile(mcpConfigPath())
	if string(before) != string(after) || store[ckMethod] != "login" || !strings.HasPrefix(string(after), original) {
		t.Fatal("custom selection changed native defaults or login")
	}
}

// Uses the installed CLI against a loopback Responses fixture. No live provider,
// real account, native shell call, login or logout is used.
func TestLocalRegisteredModelStartAndResume(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 for the isolated installed-CLI check")
	}
	t.Setenv("MINDWIRE_ISOLATION", "direct")
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", t.TempDir())
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "exec", true: "app-server"}[streaming], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			auth := []byte(`{"OPENAI_API_KEY":"unused-native-fixture"}`)
			if err := os.WriteFile(filepath.Join(home, "auth.json"), auth, 0600); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
					http.NotFound(w, r)
					return
				}
				if r.Header.Get("Authorization") != "Bearer custom-fixture-key" {
					t.Error("custom endpoint received wrong credentials")
					w.WriteHeader(403)
					return
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if body["model"] != "gpt-5.5" {
					t.Error("incorrect model routed")
				}
				writeLocalReply(w, int(calls.Add(1)), "Custom connection works.")
			}))
			defer server.Close()
			store := mapStore{ckMethod: "login"}
			if err := (adapter{}).SetProvider(store, agent.MemoryUser, "", "openship-local", agent.CustomProvider{BaseURL: server.URL, Models: []string{"gpt-5.5"}}, "custom-fixture-key", nil); err != nil {
				t.Fatal(err)
			}
			config, _ := os.ReadFile(mcpConfigPath())
			in := agent.TurnInput{Message: "Reply only.", CWD: t.TempDir(), Config: map[string]string{keyModel: "openship-local/gpt-5.5", keySandbox: "read-only", keyEffort: "low"}, Env: newAuth(store).EnvForRun()}
			if streaming {
				in.Inbound = make(chan agent.Inbound)
			}
			for i := 0; i < 2; i++ {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				result, err := (adapter{}).RunStream(ctx, in, func(agent.Event) {})
				cancel()
				if err != nil || result.IsError || result.Text != "Custom connection works." {
					t.Fatalf("turn %d: %+v %v", i, result, err)
				}
				if result.SessionID == "" || (in.SessionID != "" && result.SessionID != in.SessionID) {
					t.Fatal("conversation did not resume")
				}
				in.SessionID = result.SessionID
			}
			if calls.Load() != 2 {
				t.Fatalf("wanted two fixture requests, got %d", calls.Load())
			}
			after, _ := os.ReadFile(mcpConfigPath())
			afterAuth, _ := os.ReadFile(filepath.Join(home, "auth.json"))
			if string(after) != string(config) || string(afterAuth) != string(auth) || store[ckMethod] != "login" {
				t.Fatal("native connection was modified")
			}
		})
	}
}
