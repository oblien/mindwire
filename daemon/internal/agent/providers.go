package agent

import (
	"fmt"
	"strings"
)

// Custom-LLM-provider control plane. Some harnesses can point at a CUSTOM OpenAI-compatible endpoint
// (opencode's opencode.json `provider.<id>` block, Codex's config.toml `[model_providers.<id>]`) so a
// user can run a model mindwire's on-disk catalog has never heard of — a self-hosted gateway, a
// private router, a preview endpoint. This file adds the persistent surface so a client can list, set,
// and delete those providers; the registered models then appear in /models with Custom=true.
//
// Exposed through an OPTIONAL adapter capability (type-asserted like MCPServerModule, NOT bolted onto
// the mandatory Adapter interface): an agent with no custom-provider story simply doesn't implement it
// and the API answers 400. opencode and Codex implement it; Claude does not (its "custom endpoint" is
// the gateway auth lane — ANTHROPIC_BASE_URL/ANTHROPIC_AUTH_TOKEN + bedrock/vertex — not a provider
// registry), so /providers honestly 400s for Claude.
//
// SECURITY — the env-only invariant is preserved end to end. A CustomProvider carries a base URL, model
// ids, and only the NAME of the env var the key is exported as (EnvVar) — never the secret value. The
// key (when supplied) is written to the per-agent CredStore, and the harness config file references it
// solely through the harness placeholder (`{env:VAR}` for opencode, `env_key = "VAR"` for Codex). At
// turn time the adapter's AuthModule.EnvForRun exports the stored key under EnvVar — the sole seam
// credentials enter a run. GET never returns the key; only HasKey reports whether one is stored. As
// with the MCP writers, only the `provider.<id>` subtree of the config file is read or written — every
// sibling key is preserved byte-for-byte.

// CustomProvider is one registered custom-LLM provider, the canonical agent-agnostic shape. Every field
// but BaseURL/Models is omitempty so a round-trip carries only what a provider actually sets. The
// secret is NEVER part of this struct — it is supplied out-of-band on SetProvider and reported only as
// HasKey.
type CustomProvider struct {
	ID      string   `json:"id"`                // provider id, e.g. "my-llm" (URL/config key)
	Name    string   `json:"name,omitempty"`    // human label; falls back to ID
	BaseURL string   `json:"baseUrl"`           // OpenAI-compatible base URL
	Models  []string `json:"models"`            // model ids offered by this endpoint
	EnvVar  string   `json:"envVar,omitempty"`  // primary env var the key is exported/referenced as; derived from ID when blank
	EnvVars []string `json:"envVars,omitempty"` // ALL env-var NAMES a secret is stored for (multi-var providers); NEVER the values
	HasKey  bool     `json:"hasKey"`            // whether a secret is stored (NEVER echoes the key itself)
}

// CustomProvidersModule is an OPTIONAL adapter capability (type-asserted like MCPServerModule): list,
// set, and delete the agent's registered custom LLM providers across canonical scopes. An implementer
// sets Capabilities.CustomProviders=true; the API type-asserts this interface as the authoritative gate
// before serving /providers. A single-provider GET is served by listing the scope and indexing the map,
// so there is no separate read method.
//
// Every method takes the per-agent CredStore because — unlike MCP config — provider registration
// touches a secret: SetProvider stashes the key, DeleteProvider clears it, and ListProviders reads it
// only to report HasKey. Adapters are stateless singletons, so the store is threaded in from the API
// handler (ag.Creds) rather than held on the adapter.
type CustomProvidersModule interface {
	// ProviderScopes lists the scopes this agent's custom-provider config supports (opencode/Codex:
	// user only, matching where each harness reads global config).
	ProviderScopes() []MemoryScope
	// ListProviders returns the providers registered at a scope, keyed by id. dir is the resolved
	// project directory (used for project scope; ignored for user scope). A missing config file yields
	// an empty map — reads are forgiving. HasKey is filled from the store, never the config file.
	ListProviders(store CredStore, scope MemoryScope, dir string) (map[string]CustomProvider, error)
	// SetProvider registers one provider under p.ID at a scope, writing the harness config subtree
	// (base URL + model ids + an {env:VAR} placeholder) and preserving every sibling key. Two write-only
	// secret channels, both optional: apiKey is a single key the adapter exports under the derived/one env
	// var (custom endpoints, single-key catalog brands); secrets is a NAME→VALUE map for providers whose
	// catalog entry declares MULTIPLE env vars (e.g. AWS Bedrock's key/secret/region), each stored under
	// its own namespaced cred key. Passing neither leaves any previously stored secret intact (metadata-only
	// update). Replaces an existing entry of the same id.
	SetProvider(store CredStore, scope MemoryScope, dir, id string, p CustomProvider, apiKey string, secrets map[string]string) error
	// DeleteProvider removes one provider by id at a scope and clears its stored key. Removing an absent
	// provider is not an error (idempotent), so the caller can DELETE without a prior existence check.
	DeleteProvider(store CredStore, scope MemoryScope, dir, id string) error
}

// ProviderCredKey is the CredStore key under which a custom provider's secret is stored, namespaced by
// provider id so multiple providers coexist. The CredView already prefixes the agent type, so the full
// key is "<agentType>:provider:<id>:apiKey". providerEnvKey does the same for the exported env-var name,
// letting EnvForRun re-export a stored key without re-parsing the harness config.
func ProviderCredKey(id string) string { return "provider:" + id + ":apiKey" }

// ProviderEnvKey is the CredStore key holding the env-var NAME a custom provider's key is exported as
// (so EnvForRun knows where to inject it without parsing config). Full key:
// "<agentType>:provider:<id>:envVar". This is the SINGLE-var (legacy) scheme; ProviderEnvValueKey is the
// MULTI-var one.
func ProviderEnvKey(id string) string { return "provider:" + id + ":envVar" }

// ProviderEnvValueKey is the CredStore key holding ONE env var's value for a multi-var provider — the var
// NAME is embedded in the key, so a provider that authenticates through several env vars (e.g. AWS
// Bedrock's key/secret/region) stores one entry per var. Full key:
// "<agentType>:provider:<id>:env:<VARNAME>". The ":env:" infix can't collide with the legacy ":envVar" key
// (no ":env:" substring) nor the single-slot auth keys, and — because provider ids and env-var names
// contain no colon — the first ":env:" after "provider:" always marks the id/name boundary.
func ProviderEnvValueKey(id, name string) string { return "provider:" + id + ":env:" + name }

// StoredProviderEnv returns the NAME→VALUE map of a multi-var provider's stored env vars (the ":env:<VAR>"
// scheme), skipping entries cleared to "" (Set(k,"") stores an empty string rather than deleting the key).
// Empty when the store is nil or the provider has no multi-var entries. The legacy single-var pair
// (:envVar/:apiKey) is NOT included — callers that need it read it separately.
func StoredProviderEnv(store CredStore, id string) map[string]string {
	out := map[string]string{}
	if store == nil {
		return out
	}
	prefix := "provider:" + id + ":env:"
	for k, v := range store.All() {
		name, ok := strings.CutPrefix(k, prefix)
		if !ok || name == "" {
			continue
		}
		if val := strings.TrimSpace(v); val != "" {
			out[name] = val
		}
	}
	return out
}

// ValidateProviderID enforces a conservative id shape safe to use as a JSON/TOML config key AND as the
// stem of a derived env-var name: it must start with a letter or digit and contain only letters,
// digits, '-' and '_'. Rejecting anything looser keeps the materialized config predictable and the
// env-var derivation collision-free.
func ValidateProviderID(id string) error {
	if id == "" {
		return fmt.Errorf("provider id is empty")
	}
	for i, r := range id {
		ok := r == '-' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return fmt.Errorf("provider id %q has an invalid character %q", id, r)
		}
		if i == 0 && (r == '-' || r == '_') {
			return fmt.Errorf("provider id %q must start with a letter or digit", id)
		}
	}
	return nil
}

// ValidateEnvVarName enforces a POSIX-ish env-var name: a non-empty run of letters, digits and
// underscores not starting with a digit. This both matches what the models.dev catalog declares (e.g.
// AWS_ACCESS_KEY_ID, GOOGLE_GENERATIVE_AI_API_KEY) and — critically — guarantees the name carries no ':',
// so it can be embedded in a ProviderEnvValueKey without breaking the ":env:" boundary parse. Names arrive
// from the client through the HTTP API, so this is a security boundary, not just a sanity check.
func ValidateEnvVarName(name string) error {
	if name == "" {
		return fmt.Errorf("env var name is empty")
	}
	for i, r := range name {
		ok := r == '_' ||
			(r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !ok {
			return fmt.Errorf("env var name %q has an invalid character %q", name, r)
		}
		if i == 0 && r >= '0' && r <= '9' {
			return fmt.Errorf("env var name %q must not start with a digit", name)
		}
	}
	return nil
}

// DeriveEnvVar picks the env-var name a custom provider's key is exported under. A non-empty override
// (already the caller's explicit choice) wins verbatim; otherwise it is derived from the id by
// upper-casing and mapping every non-alphanumeric rune to '_', then appending "_API_KEY" (e.g.
// "my-llm" → "MY_LLM_API_KEY"). Callers pass the result to both the config placeholder and the stored
// ProviderEnvKey so EnvForRun and the config file agree.
func DeriveEnvVar(id, override string) string {
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(id)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String() + "_API_KEY"
}

// ProviderEnvForRun collects the env map a custom-provider-capable agent's EnvForRun should merge, unioning
// BOTH storage schemes so every registered provider's credentials reach the run:
//
//   - legacy single-var: the env-var NAME under "provider:<id>:envVar" paired with its value under
//     "provider:<id>:apiKey" — one var per provider (custom endpoints, single-key catalog brands).
//   - multi-var: every "provider:<id>:env:<VARNAME>" entry, the var NAME embedded in the key and the value
//     stored directly (catalog providers whose env[] declares several vars, e.g. AWS Bedrock).
//
// The new scheme wins on a NAME collision (a var stored both ways resolves to the multi-var value), and
// values cleared to "" contribute nothing. This stays the sole credential seam (EnvForRun → process env)
// without any adapter re-parsing its config file.
func ProviderEnvForRun(store CredStore) map[string]string {
	out := map[string]string{}
	if store == nil {
		return out
	}
	for k, v := range store.All() {
		rest, ok := strings.CutPrefix(k, "provider:")
		if !ok {
			continue
		}
		// Multi-var entry: "<id>:env:<VARNAME>" — the value IS v, the name is after ":env:".
		if _, name, isEnv := strings.Cut(rest, ":env:"); isEnv {
			if name != "" {
				if val := strings.TrimSpace(v); val != "" {
					out[name] = val
				}
			}
			continue
		}
		// Legacy single-var: "<id>:envVar" holds the NAME; its value lives under the ":apiKey" sibling.
		id, ok := strings.CutSuffix(rest, ":envVar")
		if !ok {
			continue
		}
		envVar := strings.TrimSpace(v)
		if envVar == "" {
			continue
		}
		if _, seen := out[envVar]; seen {
			continue // a multi-var entry already claimed this name — it wins.
		}
		if key := strings.TrimSpace(store.Get(ProviderCredKey(id))); key != "" {
			out[envVar] = key
		}
	}
	return out
}
