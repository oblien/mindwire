package codex

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Same native configuration shape as a working Foundry Codex installation,
// with synthetic endpoint, deployment, and token values.
const nativeFoundryConfig = `model = "my-gpt-deployment"
model_provider = "azure_custom"
model_reasoning_effort = "high"

[model_providers.azure_custom]
name = "My Azure deployment"
base_url = "https://example.services.ai.azure.com/openai/v1"
wire_api = "responses"
experimental_bearer_token = "native-config-secret"
request_max_retries = 4
stream_max_retries = 10
stream_idle_timeout_ms = 300000
`

func TestNativeFoundryConfig(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	if err := os.WriteFile(mcpConfigPath(), []byte(nativeFoundryConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	store := mapStore{}
	m := newAuth(store)
	status := m.Status(context.Background())
	if !status.Configured || status.Method != "azureFoundry" {
		t.Fatalf("native Foundry config was not recognized: %+v", status)
	}
	providers, err := (adapter{}).ListProviders(store, agent.MemoryUser, "")
	if err != nil {
		t.Fatal(err)
	}
	p := providers["azure_custom"]
	if !p.HasKey || len(p.Models) != 1 || p.Models[0] != "my-gpt-deployment" {
		t.Fatalf("native provider metadata: %+v", p)
	}
	models, err := (adapter{}).Models(m.EnvForRun())
	if err != nil || len(models) != 1 || models[0].ID != "my-gpt-deployment" {
		t.Fatalf("native deployment: %+v, %v", models, err)
	}
	encoded, err := json.Marshal(providers)
	if err != nil || strings.Contains(string(encoded), "native-config-secret") {
		t.Fatal("native credential must not appear in provider JSON")
	}
	if len(store) != 0 || len(m.EnvForRun()) != 0 {
		t.Fatal("native credentials must remain in Codex config")
	}
	// Connecting through the UI creates a separate managed provider, preserving
	// the original selected provider, token, retry tuning, and all other config.
	st, err := m.Step(context.Background(), map[string]string{
		ckAzureBaseURL: "https://second.services.ai.azure.com/openai/v1",
		ckAzureAPIKey:  "new-secret", ckAzureModel: "second-deployment",
	})
	if err != nil || st.Status != "complete" {
		t.Fatalf("connect: %+v, %v", st, err)
	}
	data, err := os.ReadFile(mcpConfigPath())
	if err != nil || !strings.HasPrefix(string(data), nativeFoundryConfig) || strings.Contains(string(data), "new-secret") {
		t.Fatal("connecting must preserve existing config and keep new secrets out of it")
	}
	cmd := buildExecCommand(agent.TurnInput{Env: m.EnvForRun()}, materialized{})
	if !strings.Contains(cmd, "model_provider=azure-foundry") || !strings.Contains(cmd, "second-deployment") {
		t.Fatalf("managed connection must override native routing for this run: %s", cmd)
	}
}

func TestNativeProviderProfileAndEnvKey(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("MINDWIRE_TEST_AZURE_KEY", "")
	config := `model = "unused"
model_provider = "unused"
profile = "azure"
[profiles.azure]
model = "profile-deployment"
model_provider = "azure_custom"
[model_providers.azure_custom]
name = "Azure"
base_url = "https://example.services.ai.azure.com/openai/v1"
env_key = "MINDWIRE_TEST_AZURE_KEY"
wire_api = "responses"
`
	if err := os.WriteFile(mcpConfigPath(), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newAuth(mapStore{})
	if m.Status(context.Background()).Configured {
		t.Fatal("an unset credential reference is not configured")
	}
	t.Setenv("MINDWIRE_TEST_AZURE_KEY", "env-secret")
	if !m.Status(context.Background()).Configured {
		t.Fatal("selected profile's env credential was not recognized")
	}
	models, err := (adapter{}).Models(nil)
	if err != nil || len(models) != 1 || models[0].ID != "profile-deployment" {
		t.Fatalf("profile deployment: %+v, %v", models, err)
	}
}

func TestNativeProviderMetadataEditKeepsCredentialsAndTuning(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	if err := os.WriteFile(mcpConfigPath(), []byte(nativeFoundryConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	store := mapStore{}
	err := (adapter{}).SetProvider(store, agent.MemoryUser, "", "azure_custom", agent.CustomProvider{
		BaseURL: "https://updated.services.ai.azure.com/openai/v1",
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mcpConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`base_url = "https://updated.services.ai.azure.com/openai/v1"`,
		`experimental_bearer_token = "native-config-secret"`,
		`name = "My Azure deployment"`,
		`request_max_retries = 4`, `stream_max_retries = 10`, `stream_idle_timeout_ms = 300000`,
	} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("metadata edit dropped native setting %s", strings.SplitN(expected, "=", 2)[0])
		}
	}
	if !newAuth(store).Status(context.Background()).Configured {
		t.Fatal("metadata-only edit broke native authentication")
	}
	if store[agent.ProviderCredKey("azure_custom")] != "" {
		t.Fatal("native credential was copied to the daemon store")
	}
	providers, err := (adapter{}).ListProviders(store, agent.MemoryUser, "")
	if err != nil || providers["azure_custom"].EnvVar != "" {
		t.Fatal("metadata edit invented an env credential reference for a native token")
	}
	if err := (adapter{}).SetProvider(store, agent.MemoryUser, "", "azure_custom", providers["azure_custom"], "", nil); err != nil {
		t.Fatal(err)
	}
	if !newAuth(store).Status(context.Background()).Configured {
		t.Fatal("a second metadata edit broke native authentication")
	}
}
