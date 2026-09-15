package codex

import "github.com/oblien/mindwire/daemon/internal/agent"

// Codex has no scriptable account model list. Surface explicitly configured
// deployments from the Foundry auth flow or native config; the public OpenAI
// catalog remains a client concern. Never guess an Azure deployment name from it.
var (
	_ agent.ModelsModule       = adapter{}
	_ agent.ModelCatalogModule = adapter{}
)

// codexModelProviders is the catalog provider set Codex runs. Codex targets the OpenAI API (or an
// OpenAI-compatible base URL); kept as a slice so a compatible provider can be added without touching
// call sites. The client sources the model picker for these from the live models.dev catalog.
var codexModelProviders = []string{"openai"}

// ModelCatalogProviders declares the catalog providers whose models Codex can run, so the client can
// populate the picker from the live catalog (the daemon stores none).
func (adapter) ModelCatalogProviders() []string { return codexModelProviders }

// Models reports the configured deployment, if any. Without custom routing the
// native list stays empty and the client can use the public OpenAI catalog.
func (adapter) Models(env map[string]string) ([]agent.ModelInfo, error) {
	if model := env[azureModelMarker]; env[azureProviderMarker] != "" && model != "" {
		return []agent.ModelInfo{{ID: model, Label: model, Provider: "azure", Custom: true}}, nil
	}
	if p, model := nativeProvider(); p.ID != "" && model != "" {
		return []agent.ModelInfo{{ID: model, Label: model, Provider: p.ID, Custom: true}}, nil
	}
	return []agent.ModelInfo{}, nil
}

// The settings picker stays free text: Codex's cached metadata catalog does not enumerate private
// provider deployments. The public OpenAI picker lives in the client's Models surface.
func modelChoices() []string { return nil }
