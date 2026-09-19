package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/authtest"
)

// Runs as an isolated managed executable, exercising actual stdio framing,
// process cleanup, notifications, and native account persistence.
func TestCodexNativeAuthProcess(t *testing.T) {
	if os.Getenv("MINDWIRE_AUTH_TEST_PROCESS") != "codex" {
		return
	}
	enc := json.NewEncoder(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	scenario := os.Getenv("MINDWIRE_AUTH_TEST_CASE")
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			os.Exit(2)
		}
		if len(req.ID) == 0 {
			continue
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("OPENAI_BASE_URL") != "" {
				os.Exit(3)
			}
		case "account/read":
			var account any
			if _, err := os.Stat(os.Getenv("MINDWIRE_AUTH_TEST_STATE")); err == nil {
				typeName := "chatgpt"
				if scenario == "wrong-account" {
					typeName = "apiKey"
				}
				account = map[string]any{"type": typeName, "planType": "plus"}
			}
			result = map[string]any{"account": account, "requiresOpenaiAuth": true}
		case "account/login/start":
			if req.Params["type"] != "chatgptDeviceCode" {
				os.Exit(4)
			}
			if scenario == "unsupported" {
				_ = enc.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": -32601, "message": "unsupported method"}})
				continue
			}
			result = map[string]any{"type": "chatgptDeviceCode", "loginId": "native-login", "verificationUrl": "https://auth.openai.com/codex/device", "userCode": "ABCD-12345"}
			if scenario != "waiting" {
				_ = os.WriteFile(os.Getenv("MINDWIRE_AUTH_TEST_STATE"), []byte("native account"), 0600)
				// Completion may race the start response. Also ignore a different login.
				for _, id := range []string{"other-login", "native-login"} {
					_ = enc.Encode(map[string]any{"method": "account/login/completed", "params": map[string]any{"loginId": id, "success": true}})
				}
			}
		case "account/login/cancel":
			if req.Params["loginId"] != "native-login" {
				os.Exit(5)
			}
			_ = os.WriteFile(os.Getenv("MINDWIRE_AUTH_TEST_CANCEL"), []byte("canceled"), 0600)
			result = map[string]any{"status": "canceled"}
		case "account/logout":
			_ = os.Remove(os.Getenv("MINDWIRE_AUTH_TEST_STATE"))
		case "model/list":
			if scenario == "unsupported-models" {
				_ = enc.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": -32601, "message": "unsupported method"}})
				continue
			}
			if req.Params["cursor"] == "next" {
				result = map[string]any{"data": []any{
					map[string]any{"id": "second", "model": "another-model", "displayName": "Another model"},
					map[string]any{"id": "hidden", "model": "hidden-model", "hidden": true},
				}}
			} else {
				result = map[string]any{"nextCursor": "next", "data": []any{map[string]any{
					"id": "native-id", "model": "gpt-6-astra", "displayName": "GPT 6 Astra", "isDefault": true,
					"defaultReasoningEffort": "high", "inputModalities": []string{"text", "image"},
					"supportedReasoningEfforts": []any{
						map[string]any{"reasoningEffort": "high", "description": "More reasoning"},
						map[string]any{"reasoningEffort": "max", "description": "Maximum reasoning"},
					},
				}}}
			}
		default:
			os.Exit(6)
		}
		_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
	}
	os.Exit(0)
}

func codexLoginFixture(t *testing.T, scenario string) *authModule {
	t.Helper()
	authtest.NativeCLI(t, "codex", "TestCodexNativeAuthProcess")
	t.Setenv("MINDWIRE_AUTH_TEST_CASE", scenario)
	t.Setenv("OPENAI_API_KEY", "old-shell-key")
	t.Setenv("OPENAI_BASE_URL", "https://old-gateway.invalid")
	m := newAuth(authtest.NewStore(map[string]string{ckMethod: "apiKey", ckAPIKey: "old-stored-key", ckBaseURL: "https://old-gateway.invalid"}))
	t.Cleanup(m.cancelLogin)
	return m
}

func TestChatGPTLogoutClearsNativeAccountAndStoredFields(t *testing.T) {
	m := codexLoginFixture(t, "complete")
	if err := m.store.Set(ckMethod, "login"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("MINDWIRE_AUTH_TEST_STATE"), []byte("native account"), 0600); err != nil {
		t.Fatal(err)
	}
	auth := agent.ManageAuth(m, m.store)
	if err := auth.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(os.Getenv("MINDWIRE_AUTH_TEST_STATE")); !os.IsNotExist(err) {
		t.Fatal("native account was retained")
	}
	if auth.Status(context.Background()).Configured || m.store.Get(ckAPIKey) != "" {
		t.Fatal("agent remains authenticated")
	}
}

func TestChatGPTDeviceLoginCompletesAndSurvivesDaemonRestart(t *testing.T) {
	m := codexLoginFixture(t, "complete")
	st, err := m.Begin(context.Background(), "login")
	if err != nil || st.FlowID == "" {
		t.Fatalf("begin: %+v %v", st, err)
	}
	authtest.Eventually(t, func() bool { return !m.login.Active() })
	if st := m.login.State(); st.Status != "complete" || st.Code != "" {
		t.Fatalf("completion: %+v", st)
	}
	if got := newAuth(m.store).Status(context.Background()); !got.Configured || got.Method != "login" {
		t.Fatalf("native persisted status: %+v", got)
	}
	assertEnv(t, m.EnvForRun(), map[string]string{azureProviderMarker: "openai"})
	if m.store.Get(ckAPIKey) != "old-stored-key" {
		t.Fatal("switching methods must not destroy unrelated credentials")
	}
}

func TestChatGPTPendingLoginIsSharedAndCancellationReachesNativeServer(t *testing.T) {
	m := codexLoginFixture(t, "waiting")
	st, err := m.Begin(context.Background(), "login")
	if err != nil || st.FlowID == "" {
		t.Fatalf("begin: %+v %v", st, err)
	}
	// A cold native process can produce its URL after Begin's bounded wait.
	// Exercise the same flow-scoped polling contract as the mobile client.
	flowID := st.FlowID
	authtest.Eventually(t, func() bool {
		st, err = m.Step(context.Background(), map[string]string{agent.AuthFlowIDKey: flowID})
		return err != nil || st.Status == "error" || (st.URL != "" && st.Code != "")
	})
	if err != nil || st.Status != "pending" || st.URL == "" || st.Code == "" || !m.Active() {
		t.Fatalf("device code: %+v %v", st, err)
	}
	again, _ := m.Begin(context.Background(), "login")
	if again.FlowID != st.FlowID {
		t.Fatal("concurrent opens started another login")
	}
	if m.store.Get(ckMethod) != "apiKey" {
		t.Fatal("pending login changed the active connection")
	}
	_, err = m.Step(context.Background(), map[string]string{agent.AuthFlowIDKey: "old-attempt", agent.AuthActionKey: "cancel"})
	if err == nil || !m.login.Active() {
		t.Fatal("stale cancel affected the current login")
	}
	st, err = m.Step(context.Background(), map[string]string{agent.AuthFlowIDKey: st.FlowID, agent.AuthActionKey: "cancel"})
	if err != nil || st.Status != "error" {
		t.Fatalf("cancel: %+v %v", st, err)
	}
	authtest.Eventually(t, func() bool { _, err := os.Stat(os.Getenv("MINDWIRE_AUTH_TEST_CANCEL")); return err == nil })
	if m.store.Get(ckMethod) != "apiKey" {
		t.Fatal("cancellation changed the prior connection")
	}
}

func TestChatGPTRejectsUnsupportedLoginOrWrongNativeAccount(t *testing.T) {
	for _, scenario := range []string{"unsupported", "wrong-account", "save-failed"} {
		t.Run(scenario, func(t *testing.T) {
			m := codexLoginFixture(t, scenario)
			if scenario == "save-failed" {
				m.store.(*authtest.Store).FailKey = ckMethod
			}
			_, _ = m.Begin(context.Background(), "login")
			authtest.Eventually(t, func() bool { return !m.login.Active() })
			if st := m.login.State(); st.Status != "error" || st.Message == "" {
				t.Fatalf("failure: %+v", st)
			}
			if m.store.Get(ckMethod) != "apiKey" {
				t.Fatal("failed login replaced the prior connection")
			}
		})
	}
}

func TestChatGPTSelectionRoutesBothTransportsToNativeAccount(t *testing.T) {
	input := agent.TurnInput{Message: "hello", Env: map[string]string{azureProviderMarker: "openai"}}
	server := newAppServer(input, materialized{})
	if server.provider != "openai" || !strings.Contains(server.command, subscriptionShell) || apiKeyEnv(server.env) != "" {
		t.Fatalf("app-server routing: %+v", server)
	}
	if command := buildExecCommand(input, materialized{}); !strings.Contains(command, subscriptionShell) || !strings.Contains(command, "model_provider=openai") {
		t.Fatalf("exec routing: %s", command)
	}
}

func TestExistingNativeChatGPTLoginIsRecognizedWithoutDaemonSecrets(t *testing.T) {
	codexLoginFixture(t, "complete")
	if err := os.WriteFile(os.Getenv("MINDWIRE_AUTH_TEST_STATE"), []byte("native account"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	if got := newAuth(authtest.NewStore(nil)).Status(context.Background()); !got.Configured || got.Method != "login" {
		t.Fatalf("existing native login: %+v", got)
	}
}

func TestSubscriptionEnvironmentDoesNotBypassWorkingDirectoryFailure(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "invoked")
	input := agent.TurnInput{Message: "hello", CWD: filepath.Join(root, "missing"), Env: map[string]string{azureProviderMarker: "openai"}}
	commands := []string{
		newAppServer(input, materialized{}).command,
		"cd " + agent.ShellQuote(input.CWD) + " && " + buildExecCommand(input, materialized{}),
	}
	for _, command := range commands {
		script := "codex() { touch " + agent.ShellQuote(marker) + "; }; " + command
		if err := exec.Command("bash", "-c", script).Run(); err == nil {
			t.Fatal("a missing workspace must fail before launching Codex")
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("Codex ran outside the requested working directory")
		}
	}
}
