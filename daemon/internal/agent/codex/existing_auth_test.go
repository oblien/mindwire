package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

const existingNativeConfig = `model_provider = "openai"
model = "root-model"
profile = "work"
approval_policy = "never"
sandbox_mode = "danger-full-access"
[profiles.work]
model_provider = "azure-work"
model = "my-deployment"
model_reasoning_effort = "high"
[model_providers.azure-work]
name = "Work Azure"
base_url = "https://fixture.openai.azure.com/openai/v1"
wire_api = "responses"
experimental_bearer_token = "fixture-access"
stream_idle_timeout_ms = 120000
[model_providers.unselected]
experimental_bearer_token = "must-not-copy-another-key"
[mcp_servers.unscoped]
command = "must-not-copy-command"
[features]
shell_tool = true
`

func sourceFixture(t *testing.T) (*authModule, string, string) {
	t.Helper()
	host, private := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", private)
	m := newAuth(mapStore{"authSignedOut": "true", "model": "old-model"})
	m.SetAuthSource(&agent.AuthSource{Home: host, Environment: map[string]string{"CODEX_HOME": host},
		Keychain: func(context.Context, string, string, bool) ([]byte, bool, error) { return nil, false, nil }})
	return m, host, private
}

func TestExistingNativeProviderUsesNativeSelectionAndPreservesScope(t *testing.T) {
	m, host, private := sourceFixture(t)
	path := filepath.Join(host, "config.toml")
	if err := os.WriteFile(path, []byte(existingNativeConfig), 0600); err != nil {
		t.Fatal(err)
	}
	// Destination-owned tools, policies and profiles must survive the connection change.
	local := `profile = "private"
[profiles.private]
model = "old"
model_provider = "old-provider"
sandbox_mode = "read-only"
[mcp_servers.scoped]
url = "http://localhost/scoped"
`
	if err := os.WriteFile(filepath.Join(private, "config.toml"), []byte(local), 0600); err != nil {
		t.Fatal(err)
	}
	auth := agent.ManageAuth(m, m.store)
	method := auth.Methods()[0]
	if method.ID != agent.ExistingAuthMethod || method.Existing.Name != "Work Azure" || method.Existing.Model != "my-deployment" {
		t.Fatalf("wrong method metadata: %+v", method)
	}
	encoded, _ := json.Marshal(method)
	if strings.Contains(string(encoded), "fixture-access") || strings.Contains(string(encoded), "azure.com") {
		t.Fatal("connection discovery exposed credentials or endpoint")
	}
	state, err := auth.Begin(context.Background(), method.ID)
	if err != nil || state.Status != "complete" || !auth.Status(context.Background()).Configured {
		t.Fatalf("native import: %+v %v", state, err)
	}
	data, _ := os.ReadFile(filepath.Join(private, "config.toml"))
	provider, model := nativeSelection(string(data))
	if provider != "azure-work" || model != "my-deployment" || m.store.Get(keyModel) != model {
		t.Fatal("native model routing was lost")
	}
	for _, forbidden := range []string{"must-not-copy", "shell_tool", "danger-full-access"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("source tools/policies crossed profiles")
		}
	}
	if !strings.Contains(string(data), "http://localhost/scoped") || !strings.Contains(string(data), `sandbox_mode = "read-only"`) {
		t.Fatal("destination policy or tools were overwritten")
	}
	info, _ := os.Stat(filepath.Join(private, "config.toml"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("native credential file is not private")
	}
	if state, _ := auth.Begin(context.Background(), method.ID); state.Status != "error" {
		t.Fatal("silently replaced an active account")
	}
	if err := auth.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if auth.Status(context.Background()).Configured {
		t.Fatal("source silently reconnected after logout")
	}
	if state, _ := auth.Begin(context.Background(), method.ID); state.Status != "complete" {
		t.Fatal("explicit reconnect failed")
	}
	original, _ := os.ReadFile(path)
	if string(original) != existingNativeConfig {
		t.Fatal("source was modified")
	}
}

func TestExistingNativeProviderResolvesOnlyItsNamedEnvironment(t *testing.T) {
	source := &agent.AuthSource{Environment: map[string]string{"WORK_KEY": "fixture-key", "WORK_ACCOUNT": "fixture-account", "UNRELATED_SECRET": "must-not-copy"}}
	config := strings.Replace(existingNativeConfig, `experimental_bearer_token = "fixture-access"`, `env_key = "WORK_KEY"`, 1) + `
[model_providers.azure-work.env_http_headers]
"X-Account" = "WORK_ACCOUNT"
[model_providers.azure-work.http_headers]
"X-Product" = "Codex"
[model_providers.azure-work.query_params]
"api-version" = "fixture"
`
	private := sourceProviderConfig(config, source)
	for _, expected := range []string{`experimental_bearer_token = "fixture-key"`, `X-Account = "fixture-account"`, `X-Product = "Codex"`, `api-version = "fixture"`} {
		if !strings.Contains(private, expected) {
			t.Fatalf("missing connection field %s", expected)
		}
	}
	if strings.Contains(private, "must-not-copy") || strings.Contains(private, "env_key") {
		t.Fatal("source environment/config leaked")
	}
	delete(source.Environment, "WORK_KEY")
	if sourceProviderConfig(config, source) != "" {
		t.Fatal("offered a provider with a missing credential")
	}
}

func TestExistingCodexAccountAndKeychainAreDiscoveryOnly(t *testing.T) {
	m, host, _ := sourceFixture(t)
	raw := []byte(`{"tokens":{"access_token":"fixture-access","refresh_token":"fixture-refresh","account_id":"fixture-account","unrelated":"must-not-copy"},"hooks":"must-not-copy"}`)
	account := sourceAccount(raw)
	if account == nil || strings.Contains(string(account.data), "must-not-copy") {
		t.Fatal("account allowlist failed")
	}
	var revealed bool
	m.source.Keychain = func(_ context.Context, service, account string, reveal bool) ([]byte, bool, error) {
		if service != "Codex Auth" || !strings.HasPrefix(account, "cli|") || len(account) != 20 {
			t.Fatal("wrong native keychain namespace")
		}
		revealed = reveal
		return raw, true, nil
	}
	if method := m.ExistingMethod(); method == nil || revealed {
		t.Fatal("discovery read the keychain secret")
	}
	if _, err := m.sourceConnection(context.Background(), true); err != nil || !revealed {
		t.Fatal("explicit reuse did not read native keychain")
	}
	if _, err := os.Stat(filepath.Join(host, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("discovery wrote source credentials")
	}
}
