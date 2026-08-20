package mindwire

import (
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Providers is the custom-LLM-provider sub-API, reachable as Client.Providers. It registers, lists, and
// removes the custom OpenAI-compatible endpoints an agent loads on every run from its own native config
// — opencode's opencode.json `provider.<id>` block and Codex's config.toml `[model_providers.<id>]`
// tables. Each call maps one-for-one to a /providers HTTP route and enforces the same gates: an agent
// without the underlying optional module surfaces as APIError{400}; a missing provider on Get is
// APIError{404}; an invalid id/base URL or unsupported scope is 400. It is scoped to its client's
// default agent and rebinds under WithAgent; override per call with ForAgent.
//
// SECURITY: the API key is NEVER carried on the CustomProvider value — it is passed write-only to Set
// and reported only as HasKey. The harness config references it solely through an env-var placeholder;
// the value is stored in the daemon and enters a run only through the auth env path. dir selects the
// project-scope working directory — empty resolves to the client's cwd (else the process cwd).
type Providers struct{ c *Client }

// module resolves the scoped agent and type-asserts its optional CustomProvidersModule, returning
// APIError{400} when the agent doesn't expose one (mirroring the /providers handler's capability gate).
// It also returns the agent's CredStore, which the module methods need to persist/read the secret.
func (p *Providers) module(op string, opts []ScopedOption) (agent.CustomProvidersModule, agent.CredStore, error) {
	ag, err := p.c.resolve(opts)
	if err != nil {
		return nil, nil, err
	}
	mod, ok := ag.Adapter.(agent.CustomProvidersModule)
	if !ok {
		return nil, nil, &APIError{Message: "agent does not support custom providers", Status: http.StatusBadRequest, Op: op}
	}
	return mod, ag.Creds, nil
}

// dir resolves the project directory a project-scope op targets: an explicit dir wins, else the client's
// cwd (else the process cwd). Mirrors the HTTP dirParam helper.
func (p *Providers) dir(dir string) string {
	return agent.ResolveDir(dir, p.c.core.sup.CWD())
}

// List returns the scoped agent's registered custom providers across every supported scope (opencode /
// Codex: user only), keyed scope→id→provider. A missing config file yields an empty map for that scope,
// not an error; any other failure is APIError{500}, matching GET /providers. An agent without the module
// is APIError{400}. HasKey reports whether a secret is stored; the key is never returned.
func (p *Providers) List(dir string, opts ...ScopedOption) (map[MemoryScope]map[string]CustomProvider, error) {
	mod, store, err := p.module("Providers.List", opts)
	if err != nil {
		return nil, err
	}
	resolved := p.dir(dir)
	out := map[MemoryScope]map[string]CustomProvider{}
	for _, scope := range mod.ProviderScopes() {
		providers, lerr := mod.ListProviders(store, scope, resolved)
		if lerr != nil {
			return nil, &APIError{Message: lerr.Error(), Status: http.StatusInternalServerError, Op: "Providers.List", Cause: lerr}
		}
		if providers == nil {
			providers = map[string]CustomProvider{}
		}
		out[scope] = providers
	}
	return out, nil
}

// Get returns one provider by id at a scope. An empty scope defaults to user. A provider that isn't
// configured maps to APIError{404}; an unsupported scope to APIError{400}, matching GET /providers/{id}.
// The key is never returned (only HasKey).
func (p *Providers) Get(scope MemoryScope, dir, id string, opts ...ScopedOption) (CustomProvider, error) {
	mod, store, err := p.module("Providers.Get", opts)
	if err != nil {
		return CustomProvider{}, err
	}
	providers, lerr := mod.ListProviders(store, orScope(scope), p.dir(dir))
	if lerr != nil {
		return CustomProvider{}, &APIError{Message: lerr.Error(), Status: http.StatusBadRequest, Op: "Providers.Get", Cause: lerr}
	}
	provider, ok := providers[id]
	if !ok {
		return CustomProvider{}, &APIError{Message: "custom provider not found", Status: http.StatusNotFound, Op: "Providers.Get"}
	}
	return provider, nil
}

// Set registers one provider at a scope and returns the stored provider (re-read so HasKey and the
// derived EnvVar reflect the authoritative state). An empty scope defaults to user. The path id wins over
// any id in prov. Two write-only secret channels, both optional: apiKey is a single key (custom endpoints,
// single-key catalog brands); secrets is a NAME→VALUE map for catalog providers whose entry declares
// MULTIPLE env vars (e.g. AWS Bedrock's key/secret/region). Passing neither leaves any previously stored
// secret intact. An invalid id/base URL or unsupported scope is APIError{400}, matching PUT /providers/{id}.
func (p *Providers) Set(scope MemoryScope, dir, id string, prov CustomProvider, apiKey string, secrets map[string]string, opts ...ScopedOption) (CustomProvider, error) {
	mod, store, err := p.module("Providers.Set", opts)
	if err != nil {
		return CustomProvider{}, err
	}
	prov.ID = id
	sc := orScope(scope)
	resolved := p.dir(dir)
	if werr := mod.SetProvider(store, sc, resolved, id, prov, apiKey, secrets); werr != nil {
		return CustomProvider{}, &APIError{Message: werr.Error(), Status: http.StatusBadRequest, Op: "Providers.Set", Cause: werr}
	}
	providers, lerr := mod.ListProviders(store, sc, resolved)
	if lerr != nil {
		return CustomProvider{}, &APIError{Message: lerr.Error(), Status: http.StatusInternalServerError, Op: "Providers.Set", Cause: lerr}
	}
	if stored, ok := providers[id]; ok {
		return stored, nil
	}
	return prov, nil
}

// Delete removes one provider at a scope and clears its stored key. An empty scope defaults to user.
// Deleting an absent provider (or from a missing config) is not an error (idempotent), matching DELETE
// /providers/{id}. An unsupported scope is APIError{400}.
func (p *Providers) Delete(scope MemoryScope, dir, id string, opts ...ScopedOption) error {
	mod, store, err := p.module("Providers.Delete", opts)
	if err != nil {
		return err
	}
	if derr := mod.DeleteProvider(store, orScope(scope), p.dir(dir), id); derr != nil {
		return &APIError{Message: derr.Error(), Status: http.StatusBadRequest, Op: "Providers.Delete", Cause: derr}
	}
	return nil
}
