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

// TestCodexModelsNativeEmpty: Codex has no scriptable model list and the daemon stores no catalog, so
// the native /models list is EMPTY (a valid 200) and the settings model field degrades to free text.
// The rich OpenAI list is a client concern, sourced from the live models.dev catalog.
func TestCodexModelsNativeEmpty(t *testing.T) {
	if !(adapter{}).Capabilities().Models {
		t.Fatal("codex caps: Models=false, want true")
	}

	got, err := adapter{}.Models(nil)
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Models n=%d, want 0 (native list is empty; the client sources OpenAI from the catalog)", len(got))
	}

	// No local list source → the field is free text, not a select (never hardcode a model list).
	if f := modelField(t); f.Type != agent.FieldText {
		t.Fatalf("model field type = %v, want FieldText (no local catalog to enumerate)", f.Type)
	}
}

// TestCodexModelCatalogProviders: Codex DECLARES its catalog provider scope so the client can source the
// picker from the live models.dev catalog for those providers.
func TestCodexModelCatalogProviders(t *testing.T) {
	var mod agent.ModelCatalogModule = adapter{}
	got := mod.ModelCatalogProviders()
	if len(got) != 1 || got[0] != "openai" {
		t.Fatalf("ModelCatalogProviders = %v, want [openai]", got)
	}
}
