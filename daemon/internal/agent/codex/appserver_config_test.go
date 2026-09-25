package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestAppServerOptionsSurviveStreamingTransport(t *testing.T) {
	input := agent.TurnInput{
		Message: "inspect", SessionID: "thread", CWD: "/repo",
		Env:     map[string]string{azureProviderMarker: azureProviderID, azureModelMarker: "deployment", "AZURE_OPENAI_API_KEY": "private-test-key"},
		Config:  map[string]string{keyEffort: "high", keyAddDir: "/extra", keyAutoCompact: "120000"},
		Options: agent.TurnOptions{OutputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	server := newAppServer(input, materialized{imagePaths: []string{"/tmp/image.png"}})
	if strings.Contains(server.command, "private-test-key") || server.env[azureProviderMarker] != "" || server.env["AZURE_OPENAI_API_KEY"] != "private-test-key" {
		t.Fatal("provider credentials must remain environment-only")
	}
	for _, params := range []map[string]any{server.startParams(), server.resumeParams()} {
		if params["modelProvider"] != azureProviderID || params["model"] != "deployment" || params["approvalPolicy"] != "never" {
			t.Fatalf("connection changed: %+v", params)
		}
		config := params["config"].(map[string]any)
		if config["model_auto_compact_token_limit"] != 120000 || config["sandbox_workspace_write"].(map[string]any)["writable_roots"].([]string)[0] != "/extra" {
			t.Fatal("workspace/context options lost")
		}
	}
	turn := server.turnParams("thread")
	inputs := turn["input"].([]any)
	if len(inputs) != 2 || inputs[1].(map[string]any)["type"] != "localImage" || inputs[1].(map[string]any)["path"] != "/tmp/image.png" {
		t.Fatal("image input lost")
	}
	if turn["effort"] != "high" || string(turn["outputSchema"].(json.RawMessage)) != `{"type":"object"}` {
		t.Fatal("reasoning effort or output schema lost")
	}
}

func TestAppServerAPIKeyUsesEnvironmentReference(t *testing.T) {
	server := newAppServer(agent.TurnInput{Env: map[string]string{
		"CODEX_API_KEY": "private-test-key", "OPENAI_BASE_URL": "https://api.example.test/v1/",
		"OPENAI_ORGANIZATION": "org-test", "OPENAI_PROJECT": "project-test",
	}}, materialized{})
	data, err := json.Marshal(server.startParams())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-test-key") || strings.Contains(server.command, "private-test-key") {
		t.Fatal("API key leaked out of the child environment")
	}
	provider := server.config["model_providers"].(map[string]any)[server.provider].(map[string]any)
	if provider["env_key"] != "CODEX_API_KEY" || provider["base_url"] != "https://api.example.test/v1" {
		t.Fatal("API key routing changed")
	}
	if provider["env_http_headers"].(map[string]string)["OpenAI-Project"] != "OPENAI_PROJECT" {
		t.Fatal("project header lost")
	}
}

func TestImageOnlyTurnUsesNativeImageInputWithoutEmptyTextBlock(t *testing.T) {
	server := newAppServer(agent.TurnInput{}, materialized{imagePaths: []string{"/tmp/photo.jpg"}})
	inputs := server.turnParams("thread")["input"].([]any)
	if len(inputs) != 1 || inputs[0].(map[string]any)["type"] != "localImage" {
		t.Fatalf("Image-only turn must contain only its native image: %+v", inputs)
	}
}
