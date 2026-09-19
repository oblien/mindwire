package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Opt-in native check: no inference or real credentials, and no writes to the
// user's Codex home. This catches CLI config-path semantics that JSON fixtures
// alone cannot validate (quoted provider keys become literal quote characters).
func TestNativeConnectionTuningLoadedByCodex(t *testing.T) {
	if os.Getenv("MINDWIRE_CODEX_SETTINGS_LIVE") != "1" {
		t.Skip("set MINDWIRE_CODEX_SETTINGS_LIVE=1 to verify the installed Codex")
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	content := "model_provider = \"azure-foundry\"\n[model_providers.azure-foundry]\nname = \"Options fixture\"\nbase_url = \"https://options.invalid/v1\"\nwire_api = \"responses\"\n"
	if err := os.WriteFile(mcpConfigPath(), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	in := agent.TurnInput{Env: map[string]string{azureProviderMarker: azureProviderID}, Config: map[string]string{keyRequestRetries: "4", keyStreamRetries: "10", keyStreamTimeout: "300000"}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := startMetadataRPC(ctx, subscriptionShell+newAppServer(in, materialized{}).command)
	if err != nil {
		t.Fatal("native Codex could not load connection overrides")
	}
	defer client.close()
	var result struct {
		Config struct {
			Providers map[string]struct {
				Name    string `json:"name"`
				Request int    `json:"request_max_retries"`
				Stream  int    `json:"stream_max_retries"`
				Idle    int    `json:"stream_idle_timeout_ms"`
			} `json:"model_providers"`
		} `json:"config"`
	}
	if err := client.call(ctx, "config/read", map[string]any{"includeLayers": false}, &result); err != nil {
		t.Fatal("could not read native configuration")
	}
	got := result.Config.Providers[azureProviderID]
	if got.Name != "Options fixture" || got.Request != 4 || got.Stream != 10 || got.Idle != 300000 {
		t.Fatalf("native connection options were not applied: %+v", got)
	}
}

func TestNativeSettingsExposeOnlyDeclaredDefaultsAndHonorManagedRouting(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	content := `model = "gpt-6-astra"
model_provider = "azure_custom"
model_reasoning_effort = "max"
model_reasoning_summary = "auto"
approvals_reviewer = "user"
profile = "focused"
[profiles.focused]
model_reasoning_summary = "concise"
[model_providers.azure_custom]
name = "Private provider"
experimental_bearer_token = "fixture-secret"
request_max_retries = 4
stream_max_retries = 10
stream_idle_timeout_ms = 300000
[tui.model_availability_nux]
"gpt-6-astra" = 1
`
	if err := os.WriteFile(mcpConfigPath(), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	store := mapStore{}
	values := agent.ReadSettings(adapter{}, store)
	for key, want := range map[string]string{keyModel: "gpt-6-astra", keyEffort: "max", keySummary: "concise", keyReviewer: "user", keyRequestRetries: "4", keyStreamRetries: "10", keyStreamTimeout: "300000"} {
		if values[key] != want {
			t.Errorf("%s = %q; want %q", key, values[key], want)
		}
	}
	encoded, _ := json.Marshal(values)
	if strings.Contains(string(encoded), "fixture-secret") || strings.Contains(string(encoded), "tui") {
		t.Fatal("non-settings metadata or credentials leaked")
	}
	store[keyEffort] = "high"
	if agent.ReadSettings(adapter{}, store)[keyEffort] != "high" {
		t.Fatal("managed override lost")
	}
	store[keyEffort] = ""
	if agent.ReadSettings(adapter{}, store)[keyEffort] != "max" {
		t.Fatal("reset did not restore native default")
	}
	store[ckMethod], store[ckAzureModel] = "azureFoundry", "another-deployment"
	if got := agent.ReadSettings(adapter{}, store); got[keyModel] != "another-deployment" || got[keyRequestRetries] != "" {
		t.Fatalf("native defaults crossed provider: %+v", got)
	}
}

func TestReasoningAndProviderSettingsReachFreshAndResumedTransports(t *testing.T) {
	for _, resume := range []string{"", "existing-thread"} {
		input := agent.TurnInput{SessionID: resume, Env: map[string]string{azureProviderMarker: azureProviderID, azureModelMarker: "deployment"},
			Config: map[string]string{keyModel: "gpt-6-astra", keyEffort: "max", keySummary: "auto", keyReviewer: "user", keyRequestRetries: "4", keyStreamRetries: "10", keyStreamTimeout: "300000"}}
		server := newAppServer(input, materialized{})
		params := server.turnParams("thread")
		if params["model"] != "gpt-6-astra" || params["effort"] != "max" || params["summary"] != "auto" {
			t.Fatalf("turn options: %+v", params)
		}
		for _, thread := range []map[string]any{server.startParams(), server.resumeParams()} {
			if thread["approvalsReviewer"] != "user" {
				t.Fatalf("reviewer lost: %+v", thread)
			}
		}
		for _, command := range []string{server.command, buildExecCommand(input, materialized{})} {
			for _, want := range []string{`model_providers.azure-foundry.request_max_retries=4`, `model_providers.azure-foundry.stream_max_retries=10`, `model_providers.azure-foundry.stream_idle_timeout_ms=300000`} {
				if !strings.Contains(command, want) {
					t.Errorf("tuning missing: %s", want)
				}
			}
			if err := exec.Command("bash", "-n", "-c", command).Run(); err != nil {
				t.Fatalf("invalid shell: %v", err)
			}
		}
		input.Env = map[string]string{azureProviderMarker: "openai"}
		if err := exec.Command("bash", "-n", "-c", newAppServer(input, materialized{}).command).Run(); err != nil {
			t.Fatalf("subscription tuning outside subshell: %v", err)
		}
		input.Env = map[string]string{"CODEX_API_KEY": "fixture-key"}
		server = newAppServer(input, materialized{})
		provider := server.config["model_providers"].(map[string]any)["mindwire-api"].(map[string]any)
		if provider["stream_max_retries"] != uint64(10) || provider["env_key"] != "CODEX_API_KEY" {
			t.Fatalf("API connection tuning: %+v", provider)
		}
	}
}

func TestSettingsValidationAcceptsNewEffortAndRejectsInvalidNumericPatch(t *testing.T) {
	schema := (adapter{}).Settings()
	values, err := agent.NormalizeSettings(schema, map[string]string{"reasoningEffort": "max", "request-max-retries": "4", "model": "gpt-6-astra", "apiKey": "never-a-setting"})
	if err != nil || values[keyEffort] != "max" || values[keyRequestRetries] != "4" || values["apiKey"] != "" {
		t.Fatalf("valid config: %+v %v", values, err)
	}
	for _, value := range []string{"-1", "many", "1.5", "4294967296"} {
		if _, err := agent.NormalizeSettings(schema, map[string]string{"model": "another-model", keyStreamRetries: value}); err == nil {
			t.Errorf("accepted invalid retry count %q", value)
		}
	}
	if _, err := agent.NormalizeSettings(schema, map[string]string{keyStreamTimeout: "0"}); err == nil {
		t.Fatal("accepted zero idle timeout")
	}
}
