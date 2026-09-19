package codex

import (
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func modelField(t *testing.T) agent.Field {
	t.Helper()
	for _, sec := range (adapter{}).Settings().Sections {
		for _, f := range sec.Fields {
			if f.Key == keyModel {
				return f
			}
		}
	}
	t.Fatal("model field not found in codex settings")
	return agent.Field{}
}

// Native model/list paging retains model-advertised reasoning, including new levels.
func TestCodexModelsNativeProtocol(t *testing.T) {
	codexLoginFixture(t, "models")
	if !(adapter{}).Capabilities().Models {
		t.Fatal("codex caps: Models=false, want true")
	}

	got, err := adapter{}.Models(map[string]string{azureProviderMarker: "openai"})
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(got) != 2 || got[0].ID != "gpt-6-astra" || !got[0].Default || got[1].ID != "another-model" {
		t.Fatalf("native model paging: %+v", got)
	}
	if got[0].DefaultReasoningEffort != "high" || len(got[0].ReasoningEfforts) != 2 || got[0].ReasoningEfforts[1].Value != "max" {
		t.Fatalf("native model reasoning levels: %+v", got[0])
	}

	// No local list source → the field is free text, not a select (never hardcode a model list).
	if f := modelField(t); f.Type != agent.FieldText {
		t.Fatalf("model field type = %v, want FieldText (no local catalog to enumerate)", f.Type)
	}
}

// TestCodexModelCatalogProviders: Codex DECLARES its catalog provider scope so the client can source the
// picker from the live models.dev catalog for those providers.
func TestCodexModelsCustomDeploymentAndOfflineFallback(t *testing.T) {
	codexLoginFixture(t, "models")
	for _, id := range []string{"private-deployment", "gpt-6-astra"} {
		got, err := (adapter{}).Models(map[string]string{azureProviderMarker: azureProviderID, azureModelMarker: id})
		if err != nil || len(got) != 1 || !got[0].Custom || got[0].ID != id {
			t.Fatalf("deployment: %+v, %v", got, err)
		}
		if (len(got[0].ReasoningEfforts) > 0) != (id == "gpt-6-astra") {
			t.Fatalf("must only match exact model metadata: %+v", got)
		}
	}
	t.Setenv("MINDWIRE_AUTH_TEST_CASE", "unsupported-models")
	got, err := (adapter{}).Models(map[string]string{azureProviderMarker: "openai"})
	if err != nil || len(got) != 0 {
		t.Fatalf("manual fallback: %+v %v", got, err)
	}
}
