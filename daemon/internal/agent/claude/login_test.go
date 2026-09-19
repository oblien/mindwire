package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/authtest"
)

func TestClaudeNativeAuthProcess(t *testing.T) {
	if os.Getenv("MINDWIRE_AUTH_TEST_PROCESS") != "claude" {
		return
	}
	args := strings.Join(os.Args, " ")
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CODE_USE_FOUNDRY") != "0" {
		os.Exit(2)
	}
	if strings.Contains(args, "auth login --claudeai") {
		fmt.Fprintln(os.Stdout, "If the browser didn't open, visit: https://claude.com/cai/oauth/authorize?state=fixture")
		// Deliberately no newline: the native CLI waits after exactly this prompt.
		fmt.Fprint(os.Stdout, codePrompt+" ")
		input, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(input) != "fixture-code" {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Getenv("MINDWIRE_AUTH_TEST_STATE"), []byte("native account"), 0600); err != nil {
			os.Exit(4)
		}
		fmt.Fprintln(os.Stdout, "Successfully logged in.")
		os.Exit(0)
	}
	if strings.Contains(args, "auth status --json") {
		_, err := os.Stat(os.Getenv("MINDWIRE_AUTH_TEST_STATE"))
		method := "oauth"
		if os.Getenv("MINDWIRE_AUTH_TEST_CASE") == "wrong-account" {
			method = "api_key"
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"loggedIn": err == nil, "authMethod": method, "apiProvider": "firstParty"})
		os.Exit(0)
	}
	if strings.Contains(args, "auth logout") {
		_ = os.Remove(os.Getenv("MINDWIRE_AUTH_TEST_STATE"))
		os.Exit(0)
	}
	os.Exit(5)
}

func claudeLoginFixture(t *testing.T) *authModule {
	t.Helper()
	authtest.NativeCLI(t, "claude", "TestClaudeNativeAuthProcess")
	t.Setenv("ANTHROPIC_API_KEY", "old-shell-key")
	t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "1")
	m := newAuth(authtest.NewStore(map[string]string{"authMethod": "apiKey", "apiKey": "old-stored-key", "oauthToken": "old-setup-token"}))
	t.Cleanup(m.cancelLogin)
	return m
}

func TestClaudeLogoutClearsNativeAccountAndStoredFields(t *testing.T) {
	m := claudeLoginFixture(t)
	if err := m.store.Set("authMethod", "login"); err != nil {
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
	if auth.Status(context.Background()).Configured || m.store.Get("oauthToken") != "" || m.store.Get("apiKey") != "" {
		t.Fatal("agent remains authenticated")
	}
}

func TestClaudeSubscriptionBrowserCodeCompletesThroughNativeLogin(t *testing.T) {
	m := claudeLoginFixture(t)
	if first := m.Methods()[0]; first.ID != "login" || !first.Interactive || first.Label != "Continue with Claude" {
		t.Fatalf("subscription must be first: %+v", first)
	}
	st, err := m.Begin(context.Background(), "login")
	if err != nil || st.FlowID == "" {
		t.Fatalf("begin: %+v %v", st, err)
	}
	flowID := st.FlowID
	authtest.Eventually(t, func() bool {
		st, err = m.Step(context.Background(), map[string]string{agent.AuthFlowIDKey: flowID})
		return err != nil || st.Status == "error" || st.Status == "needs_input"
	})
	if err != nil || st.Status != "needs_input" || st.URL == "" || !m.Active() {
		t.Fatalf("browser handoff: %+v %v", st, err)
	}
	if fields := m.login.State().Fields; len(fields) != 1 || fields[0].Key != agent.AuthCodeKey {
		t.Fatalf("code field: %+v", fields)
	}
	if m.store.Get("authMethod") != "apiKey" {
		t.Fatal("pending login replaced the current connection")
	}
	st, err = m.Step(context.Background(), map[string]string{agent.AuthFlowIDKey: st.FlowID, agent.AuthCodeKey: "fixture-code"})
	if err != nil || st.Status != "pending" {
		t.Fatalf("submit: %+v %v", st, err)
	}
	authtest.Eventually(t, func() bool { return !m.login.Active() })
	if st := m.login.State(); st.Status != "complete" {
		t.Fatalf("completion: %+v", st)
	}
	if m.store.Get("oauthToken") != "" || m.store.Get("authMethod") != "login" {
		t.Fatal("native login did not replace the legacy setup-token selection")
	}
	if status := newAuth(m.store).Status(context.Background()); !status.Configured || status.Method != "login" {
		t.Fatalf("native persisted status: %+v", status)
	}
	if !maps.Equal(m.EnvForRun(), map[string]string{subscriptionMarker: "native"}) {
		t.Fatalf("old credentials leaked: %+v", m.EnvForRun())
	}
}

func TestClaudeCodeCancellationCannotCompleteOrOverwritePriorConnection(t *testing.T) {
	m := claudeLoginFixture(t)
	st, _ := m.Begin(context.Background(), "login")
	again, _ := m.Begin(context.Background(), "login")
	if st.FlowID != again.FlowID {
		t.Fatal("another open started a duplicate native login")
	}
	st, err := m.Step(context.Background(), map[string]string{agent.AuthFlowIDKey: st.FlowID, agent.AuthActionKey: "cancel"})
	if err != nil || st.Status != "error" || st.URL != "" {
		t.Fatalf("cancel: %+v %v", st, err)
	}
	if _, err := m.Step(context.Background(), map[string]string{agent.AuthFlowIDKey: st.FlowID, agent.AuthCodeKey: "fixture-code"}); err == nil {
		t.Fatal("canceled login accepted a code")
	}
	if m.store.Get("authMethod") != "apiKey" || m.store.Get("oauthToken") != "old-setup-token" {
		t.Fatal("cancellation changed stored credentials")
	}
}

func TestClaudeRejectsCompletionForAnotherAuthenticationMethod(t *testing.T) {
	m := claudeLoginFixture(t)
	t.Setenv("MINDWIRE_AUTH_TEST_CASE", "wrong-account")
	st, _ := m.Begin(context.Background(), "login")
	authtest.Eventually(t, func() bool { return m.login.State().Status == "needs_input" })
	_, _ = m.Step(context.Background(), map[string]string{agent.AuthFlowIDKey: st.FlowID, agent.AuthCodeKey: "fixture-code"})
	authtest.Eventually(t, func() bool { return !m.login.Active() })
	if m.login.State().Status != "error" || m.store.Get("authMethod") != "apiKey" {
		t.Fatal("an existing API key must not masquerade as a successful subscription login")
	}
}

func TestClaudeExplicitMethodWinsOverPreviouslyStoredCredentials(t *testing.T) {
	store := mapStore{"authMethod": "apiKey", "apiKey": "api", "oauthToken": "oauth", "bearerToken": "gateway"}
	m := newAuth(store)
	if !maps.Equal(m.EnvForRun(), map[string]string{"ANTHROPIC_API_KEY": "api"}) {
		t.Fatalf("API selection: %+v", m.EnvForRun())
	}
	store["authMethod"] = "bearerToken"
	if !maps.Equal(m.EnvForRun(), map[string]string{"ANTHROPIC_AUTH_TOKEN": "gateway"}) {
		t.Fatalf("gateway selection: %+v", m.EnvForRun())
	}
	store["authMethod"], store["baseUrl"] = "login", "https://old-gateway.invalid"
	if !maps.Equal(m.EnvForRun(), map[string]string{subscriptionMarker: "token", "CLAUDE_CODE_OAUTH_TOKEN": "oauth"}) {
		t.Fatalf("legacy subscription: %+v", m.EnvForRun())
	}
	store["oauthToken"] = ""
	if !maps.Equal(resolveModelEnv(m.EnvForRun()), map[string]string{subscriptionMarker: "native"}) {
		t.Fatal("model discovery resurrected old host credentials")
	}
}

func TestClaudeSubscriptionSettingsPreserveHooksAndOverrideOldProvider(t *testing.T) {
	options := agent.TurnOptions{ClaudeSettings: json.RawMessage(`{"hooks":{"Stop":[]},"env":{"APP_VALUE":"kept","ANTHROPIC_API_KEY":"old","CLAUDE_CODE_USE_VERTEX":"1"}}`)}
	got, err := subscriptionTurnSettings(options, map[string]string{subscriptionMarker: "native"})
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(got.ClaudeSettings, &settings); err != nil {
		t.Fatal(err)
	}
	env := settings["env"].(map[string]any)
	if env["APP_VALUE"] != "kept" || env["ANTHROPIC_API_KEY"] != "" || env["CLAUDE_CODE_USE_VERTEX"] != "0" || env["CLAUDE_CODE_OAUTH_TOKEN"] != "" || settings["hooks"] == nil {
		t.Fatalf("subscription override lost user settings or kept old auth: %+v", settings)
	}
}
