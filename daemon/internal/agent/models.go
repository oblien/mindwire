package agent

// Model listing. Some agents can enumerate the models available to the configured account (Claude
// fetches them from the Anthropic Models API); others treat the model as free text. This is exposed
// through an OPTIONAL adapter capability (type-asserted like MemoryModule, NOT bolted onto the
// mandatory Adapter interface), so an agent with no scriptable model list simply doesn't implement it
// and the API answers 400. No secret flows out here — only model ids and labels.

// ModelInfo is one model an agent can run: its API id plus a human label, plus provider-aware metadata
// when the source knows it. The daemon populates only what an adapter enumerates natively (id, label,
// provider); the richer fields (context, cost, modalities, capability flags) are filled CLIENT-SIDE from
// the live models.dev catalog the SDK owns. It is the public, agent-agnostic shape of a model choice,
// served by GET /models. Every field beyond ID/Label is omitempty so the wire stays byte-compatible: an
// adapter that emits exactly {id,label,provider} is normal, and the client enriches by (provider, id).
type ModelInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`

	// Provider is the harness/catalog provider id that runs this model (e.g. "anthropic", "openai").
	// It makes provider-binding explicit: claude-code runs anthropic, codex runs openai-compatible,
	// opencode is provider-agnostic, so the catalog is agent-scoped rather than a flat global list.
	Provider string `json:"provider,omitempty"`

	// Context/MaxOut are the model's context-window and max-output token limits (catalog limit.context
	// / limit.output). Modalities list the accepted input and produced output kinds ("text","image",…).
	Context int      `json:"contextWindow,omitempty"`
	MaxOut  int      `json:"maxOutput,omitempty"`
	Input   []string `json:"inputModalities,omitempty"`
	Output  []string `json:"outputModalities,omitempty"`

	// Reasoning/ToolCall/Attachment are capability flags from the catalog.
	Reasoning  bool `json:"reasoning,omitempty"`
	ToolCall   bool `json:"toolCall,omitempty"`
	Attachment bool `json:"attachment,omitempty"`

	// Cost is per-million-token pricing when the catalog knows it.
	Cost *ModelCost `json:"cost,omitempty"`

	// Custom marks a model sourced from a client-registered custom provider (see CustomProvidersModule)
	// rather than the built-in catalog. Such a model's metadata is typically sparse (a custom endpoint
	// isn't in the on-disk catalog), so only ID/Provider/Custom are populated — surfaced honestly.
	Custom bool `json:"custom,omitempty"`
}

// ModelCost is per-million-token pricing for a model, in USD. Every field is omitempty so a model with
// unknown pricing carries no cost block at all.
type ModelCost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
}

// ModelsModule is an OPTIONAL adapter capability (type-asserted like Titler): enumerate the models
// available to the configured account. An adapter that implements it sets Capabilities.Models=true;
// the API type-asserts this interface as the authoritative gate before serving /models.
type ModelsModule interface {
	// Models returns the models the agent enumerates NATIVELY — Claude fetches them from the Anthropic
	// account API, opencode shells `opencode models`. env carries the run credentials (from
	// AuthModule.EnvForRun) so an adapter that fetches its list from a provider API authenticates as the
	// configured account. mindwire no longer stores the models.dev catalog in the daemon (the client owns
	// it and fetches it live), so an adapter returns only what it truly knows locally — no catalog
	// enrichment, no catalog-sourced list. An EMPTY list (no credentials, offline, no scriptable list) is
	// not an error: it degrades to "unknown", and the client fills the picker from the live catalog for
	// the providers named by ModelCatalogModule.
	Models(env map[string]string) ([]ModelInfo, error)
}

// ModelCatalogModule is an OPTIONAL adapter capability: declare which models.dev catalog providers this
// agent's models come from. Because the daemon no longer ships the models.dev catalog, an agent that
// cannot self-enumerate a full list (Codex has no scriptable model command) instead names its provider
// scope here, and the client — which owns the live catalog — sources the model picker from those
// providers and enriches every model by (provider, id). The sentinel "*" means "all providers" (a
// provider-agnostic agent like opencode, which uses models.dev as its own registry). An adapter that
// self-enumerates its complete list (Claude's account API) need not implement this: its returned models
// already carry Provider, so the client enriches them, and an empty account list stays honestly empty
// rather than dumping the whole catalog. Surfaced as `modelProviders` on GET /agent.
type ModelCatalogModule interface {
	ModelCatalogProviders() []string
}
