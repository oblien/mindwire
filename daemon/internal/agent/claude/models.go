package claude

import "github.com/oblien/mindwire/daemon/internal/agent"

// Model listing for Claude Code. The `claude` CLI exposes no scriptable model list, so the real source
// of truth is the Anthropic Models API (GET /v1/models) — already fetched, cached (TTL + negative
// backoff), and single-flighted by discover.go for the settings picker. This module promotes that same
// list to a first-class /models route, mapping the private ModelOpt cache to the public
// agent.ModelInfo. No network logic is duplicated; it reuses ensureModels/knownModels verbatim.
//
// The daemon no longer stores the models.dev catalog, so this emits the account list BARE (id, label,
// provider=anthropic); the client enriches each row's context/cost/modality metadata from the live
// catalog by (provider, id). Claude self-enumerates its full list, so it does NOT implement
// ModelCatalogModule: an empty account list (offline / not signed in) stays honestly empty rather than
// the client dumping the whole anthropic catalog.
var _ agent.ModelsModule = adapter{}

// Models returns the models available to the configured account. Unlike the opportunistic refresh a
// turn kicks off in a goroutine, an explicit /models request runs ensureModels SYNCHRONOUSLY (it is
// TTL/backoff-guarded, so a warm cache returns immediately) and then reads the cache. The credential is
// resolved like a real turn's — the daemon-stored key if present, else the HOST-LEVEL key the CLI
// itself uses (see ensureModels/resolveModelEnv) — so a picker isn't empty while `claude auth status`
// says signed in. No credential anywhere, offline, or a transient API failure leaves the cache empty →
// an empty list, which is not an error.
func (adapter) Models(env map[string]string) ([]agent.ModelInfo, error) {
	ensureModels(env)
	opts := knownModels()
	out := make([]agent.ModelInfo, 0, len(opts))
	for _, m := range opts {
		// The account list (from GET /v1/models) is the source of WHICH models and their labels; provider
		// is always anthropic for Claude Code. The client attaches context/cost/modality metadata from the
		// live models.dev catalog by (provider, id) — the daemon carries none.
		out = append(out, agent.ModelInfo{ID: m.ID, Label: m.Label, Provider: "anthropic"})
	}
	return out, nil
}
