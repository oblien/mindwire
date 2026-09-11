package codex

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// mapStore is an in-memory agent.CredStore for exercising the auth module.
type mapStore map[string]string

func (s mapStore) Get(k string) string    { return s[k] }
func (s mapStore) Set(k, v string) error  { s[k] = v; return nil }
func (s mapStore) All() map[string]string { return s }

// A full API-key setup persists the credential + optional companions, tags the method, and EnvForRun
// then exports CODEX_API_KEY (+ OPENAI_API_KEY) alongside the endpoint/org/project vars.
func TestStepAPIKeyAndEnv(t *testing.T) {
	store := mapStore{}
	m := newAuth(store)

	st, err := m.Step(context.Background(), map[string]string{
		ckAPIKey:  "sk-abc",
		ckBaseURL: "https://api.example/v1",
		ckOrg:     "org-1",
		ckProject: "proj-1",
	})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if st.Method != "apiKey" || st.Status != "complete" {
		t.Fatalf("Step state = %+v, want apiKey/complete", st)
	}
	if store[ckMethod] != "apiKey" {
		t.Errorf("authMethod = %q, want apiKey", store[ckMethod])
	}

	env := m.EnvForRun()
	want := map[string]string{
		"CODEX_API_KEY":       "sk-abc",
		"OPENAI_API_KEY":      "sk-abc",
		"OPENAI_BASE_URL":     "https://api.example/v1",
		"OPENAI_ORGANIZATION": "org-1",
		"OPENAI_PROJECT":      "proj-1",
	}
	assertEnv(t, env, want)
}

// An access-token setup exports only CODEX_ACCESS_TOKEN.
func TestStepAccessTokenAndEnv(t *testing.T) {
	store := mapStore{}
	m := newAuth(store)
	if _, err := m.Step(context.Background(), map[string]string{ckAccessToken: "tok-1"}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	assertEnv(t, m.EnvForRun(), map[string]string{"CODEX_ACCESS_TOKEN": "tok-1"})
}

// Switching methods clears the other credential so a stale secret can't leak into a run.
func TestStepSwitchClearsOtherCredential(t *testing.T) {
	store := mapStore{}
	m := newAuth(store)
	if _, err := m.Step(context.Background(), map[string]string{ckAPIKey: "sk-old"}); err != nil {
		t.Fatalf("Step apiKey: %v", err)
	}
	if _, err := m.Step(context.Background(), map[string]string{ckAccessToken: "tok-new"}); err != nil {
		t.Fatalf("Step accessToken: %v", err)
	}
	env := m.EnvForRun()
	if _, leaked := env["CODEX_API_KEY"]; leaked {
		t.Errorf("stale CODEX_API_KEY leaked after switching to access token: %v", env)
	}
	assertEnv(t, env, map[string]string{"CODEX_ACCESS_TOKEN": "tok-new"})
}

// Optional companions left blank are not exported (env stays minimal).
func TestEnvForRunOmitsBlankCompanions(t *testing.T) {
	m := newAuth(mapStore{ckMethod: "apiKey", ckAPIKey: "sk-x"})
	assertEnv(t, m.EnvForRun(), map[string]string{"CODEX_API_KEY": "sk-x", "OPENAI_API_KEY": "sk-x"})
}

// With no method tag, EnvForRun falls back to whichever secret is present so a run still authenticates.
func TestEnvForRunPresenceFallback(t *testing.T) {
	assertEnv(t, newAuth(mapStore{ckAPIKey: "sk-y"}).EnvForRun(),
		map[string]string{"CODEX_API_KEY": "sk-y", "OPENAI_API_KEY": "sk-y"})
	assertEnv(t, newAuth(mapStore{ckAccessToken: "tok-z"}).EnvForRun(),
		map[string]string{"CODEX_ACCESS_TOKEN": "tok-z"})
}

// Status is presence-based (env-only creds don't show in `codex login status`).
func TestStatus(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	if got := newAuth(mapStore{ckAPIKey: "sk"}).Status(context.Background()); !got.Configured || got.Method != "apiKey" {
		t.Errorf("api key status = %+v, want configured/apiKey", got)
	}
	if got := newAuth(mapStore{ckAccessToken: "t"}).Status(context.Background()); !got.Configured || got.Method != "accessToken" {
		t.Errorf("access token status = %+v, want configured/accessToken", got)
	}
	if got := newAuth(mapStore{}).Status(context.Background()); got.Configured {
		t.Errorf("empty store status = %+v, want not configured", got)
	}
}

// An empty step is an error, not a silent success.
func TestStepNoCredentialErrors(t *testing.T) {
	st, _ := newAuth(mapStore{}).Step(context.Background(), map[string]string{})
	if st.Status != "error" {
		t.Errorf("empty Step status = %q, want error", st.Status)
	}
}

func TestAzureFoundryAuthWritesNativeProviderAndSelectsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	store := mapStore{}
	m := newAuth(store)

	st, err := m.Step(context.Background(), map[string]string{
		ckAzureBaseURL: "https://contoso-ai.services.ai.azure.com/openai/v1",
		ckAzureAPIKey:  "azure-secret",
		ckAzureModel:   "my-codex-deployment",
	})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if st.Method != "azureFoundry" || st.Status != "complete" {
		t.Fatalf("state = %+v", st)
	}
	if got := m.Status(context.Background()); !got.Configured || got.Method != "azureFoundry" {
		t.Fatalf("status = %+v", got)
	}

	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	config := string(data)
	for _, want := range []string{
		"[model_providers.azure-foundry]",
		`base_url = "https://contoso-ai.services.ai.azure.com/openai/v1"`,
		`env_key = "AZURE_OPENAI_API_KEY"`,
		`wire_api = "responses"`,
	} {
		if !strings.Contains(config, want) {
			t.Errorf("config missing %q:\n%s", want, config)
		}
	}
	env := m.EnvForRun()
	if env["AZURE_OPENAI_API_KEY"] != "azure-secret" {
		t.Errorf("Azure credential not exported: %v", env)
	}
	if env[azureProviderMarker] != azureProviderID {
		t.Errorf("provider marker missing: %v", env)
	}
	cmd := buildExecCommand(agent.TurnInput{Env: env, Config: map[string]string{}}, materialized{})
	if !strings.Contains(cmd, "model_provider=azure-foundry") {
		t.Errorf("command does not select Azure provider: %s", cmd)
	}
	if !strings.Contains(cmd, "my-codex-deployment") {
		t.Errorf("command does not select Azure model: %s", cmd)
	}
	if strings.Contains(config, "azure-secret") || strings.Contains(cmd, "azure-secret") {
		t.Fatal("credential leaked into config or command")
	}
	models, err := (adapter{}).Models(env)
	if err != nil || len(models) != 1 || models[0].ID != "my-codex-deployment" {
		t.Fatalf("configured deployment missing from model list: %+v, %v", models, err)
	}
}

func TestAzureFoundryValidationPreservesExistingConnection(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	for _, input := range []map[string]string{
		{ckAzureBaseURL: "", ckAzureAPIKey: "", ckAzureModel: ""},
		{ckAzureBaseURL: "https://example.test/anthropic", ckAzureAPIKey: "key", ckAzureModel: "deployment"},
		{ckAzureResource: "example", ckAzureModel: "deployment"},
		{ckAzureResource: "example", ckAzureAPIKey: "key"},
	} {
		store := mapStore{ckMethod: "apiKey", ckAPIKey: "existing"}
		st, err := newAuth(store).Step(context.Background(), input)
		if err != nil || st.Status != "error" || st.Method != "azureFoundry" {
			t.Fatalf("invalid setup result: %+v, %v", st, err)
		}
		if store[ckMethod] != "apiKey" || store[ckAPIKey] != "existing" || len(store) != 2 {
			t.Fatal("invalid setup changed existing credentials")
		}
	}
}

func TestAzureFoundryAlternativesAndSwitch(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	store := mapStore{ckMethod: "apiKey", ckAPIKey: "old-key", ckBaseURL: "https://old.example/v1", ckOrg: "old-org", ckProject: "old-project"}
	m := newAuth(store)
	st, err := m.Step(context.Background(), map[string]string{
		ckAzureResource: "my-resource", ckAzureToken: "entra-token", ckAzureAPIKey: "ignored-key", ckAzureModel: "deployment",
	})
	if err != nil || st.Status != "complete" {
		t.Fatalf("alternative auth: %+v, %v", st, err)
	}
	assertEnv(t, m.EnvForRun(), map[string]string{
		"AZURE_OPENAI_ACCESS_TOKEN": "entra-token", azureProviderMarker: azureProviderID, azureModelMarker: "deployment",
	})
	providers, err := (adapter{}).ListProviders(store, agent.MemoryUser, "")
	if err != nil || providers[azureProviderID].BaseURL != "https://my-resource.openai.azure.com/openai/v1" {
		t.Fatalf("resource endpoint: %+v, %v", providers, err)
	}
}

func TestAzureFoundrySelectionsControlPersistedProvider(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	store := mapStore{}
	m := newAuth(store)
	for _, mode := range []string{"apiKey", "entraToken", "apiKey"} {
		input := map[string]string{
			ckAzureEndpointType: "resource", ckAzureResource: "selected", ckAzureBaseURL: "https://stale.test/anthropic",
			ckAzureAuthType: mode, ckAzureAPIKey: "key", ckAzureToken: "token", ckAzureModel: "deployment",
		}
		st, err := m.Step(context.Background(), input)
		if err != nil || st.Status != "complete" {
			t.Fatalf("selected mode %s: %+v, %v", mode, st, err)
		}
		env := m.EnvForRun()
		if mode == "apiKey" {
			if env["AZURE_OPENAI_API_KEY"] != "key" || env["AZURE_OPENAI_ACCESS_TOKEN"] != "" {
				t.Fatal("token overrode the selected API key")
			}
		} else if env["AZURE_OPENAI_ACCESS_TOKEN"] != "token" || env["AZURE_OPENAI_API_KEY"] != "" {
			t.Fatal("API key overrode the selected token")
		}
		providers, err := (adapter{}).ListProviders(store, agent.MemoryUser, "")
		if err != nil || providers[azureProviderID].BaseURL != "https://selected.openai.azure.com/openai/v1" {
			t.Fatalf("hidden URL overrode resource selection: %v, %v", providers, err)
		}
		before := maps.Clone(store)
		input[ckAzureAPIKey], input[ckAzureToken] = "", ""
		st, err = m.Step(context.Background(), input)
		if err != nil || st.Status != "error" || len(st.Sections) == 0 || !maps.Equal(store, before) {
			t.Fatalf("invalid selection replaced existing connection or lost its form: %+v, %v", st, err)
		}
	}
}

func TestAzureFoundryEnvIgnoresLegacyOpenAISettings(t *testing.T) {
	store := mapStore{
		ckMethod: "azureFoundry", ckBaseURL: "https://old.example/v1", ckOrg: "old-org", ckProject: "old-project",
		agent.ProviderCredKey(azureProviderID): "azure-key",
		agent.ProviderEnvKey(azureProviderID):  "AZURE_OPENAI_API_KEY", pkModels(azureProviderID): "deployment",
	}
	assertEnv(t, newAuth(store).EnvForRun(), map[string]string{
		"AZURE_OPENAI_API_KEY": "azure-key", azureProviderMarker: azureProviderID, azureModelMarker: "deployment",
	})
}

func TestAzureFoundryRequiresHTTPSAndCredential(t *testing.T) {
	m := newAuth(mapStore{})
	st, _ := m.Step(context.Background(), map[string]string{ckAzureBaseURL: "http://example.test", ckAzureAPIKey: "x"})
	if st.Status != "error" {
		t.Fatalf("insecure URL state = %+v", st)
	}
	st, _ = m.Step(context.Background(), map[string]string{ckAzureResource: "valid-resource"})
	if st.Status != "error" {
		t.Fatalf("missing credential state = %+v", st)
	}
}

func TestAzureFoundryEntraUsesBearerEnvReference(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	m := newAuth(mapStore{})
	_, err := m.Step(context.Background(), map[string]string{
		ckAzureBaseURL: "https://contoso.services.ai.azure.com/openai/v1/",
		ckAzureToken:   "entra-token",
		ckAzureModel:   "gpt-5.2-codex",
	})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `env_key = "AZURE_OPENAI_ACCESS_TOKEN"`) {
		t.Fatalf("Entra config does not use bearer env reference:\n%s", data)
	}
}

func assertEnv(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("EnvForRun = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("EnvForRun[%q] = %q, want %q", k, got[k], v)
		}
	}
}
