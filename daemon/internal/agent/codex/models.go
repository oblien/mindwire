package codex

import "github.com/oblien/mindwire/daemon/internal/agent"

// Model listing for Codex. Codex ships no scriptable model list of its own (no list command, no help
// enum), and mindwire no longer stores the models.dev catalog in the daemon — the client owns it and
// fetches it live. So Codex enumerates NOTHING locally: it DECLARES its catalog provider scope (openai)
// via ModelCatalogProviders, and the client sources the model picker from that provider's live catalog
// and enriches every row. Models therefore returns an empty native list (a valid 200), and the settings
// model field degrades to free text (CLI-validated) — nothing is ever hardcoded.
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

// Models returns the models Codex enumerates natively — none, since Codex has no scriptable list and the
// daemon carries no catalog. The OpenAI list is a client concern (see ModelCatalogProviders). An empty
// list is valid, not an error. env is ignored (no per-account list to fetch).
func (adapter) Models(_ map[string]string) ([]agent.ModelInfo, error) {
	return []agent.ModelInfo{}, nil
}

// modelChoices returns the model ids for the settings model select. Codex has no local list source, so
// this is always empty and the field degrades to free text (never hardcode a model list); the rich,
// pickable OpenAI list lives in the client's Models surface, sourced from the live catalog.
func modelChoices() []string { return nil }
