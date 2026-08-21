package opencode

import (
	"context"
	"errors"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// auth.go is opencode's AuthModule. opencode reads provider API keys straight from the process
// environment (ANTHROPIC_API_KEY, OPENAI_API_KEY, …) — the same key its own `opencode auth login`
// ultimately relies on for the "Environment" providers — so mindwire authenticates a run env-only,
// never writing opencode's auth.json. Secrets enter a run ONLY through EnvForRun, matching codex's
// posture. (opencode's `PUT /auth/{provider}` route is a documented, deferred secondary path.)
//
// One field-based method, "apiKey": a provider id + its key. The provider determines which env var the
// key is exported as (provider→var table below); an explicit override field wins when a provider isn't
// in the table or uses a non-standard var name.

// Cred-store keys.
const (
	ckProvider = "provider"
	ckAPIKey   = "apiKey"
	ckEnvVar   = "envVar"
)

type authModule struct{ store agent.CredStore }

func newAuth(store agent.CredStore) *authModule { return &authModule{store: store} }

func (m *authModule) Methods() []agent.AuthMethod {
	return []agent.AuthMethod{{
		ID: "apiKey", Label: "API key", Scope: agent.ScopeUnified,
		Help: "A provider API key opencode uses for the model you select. Exported to a run as the provider's env var (e.g. ANTHROPIC_API_KEY) — env-only, no auth file is written.",
		Fields: []agent.Field{
			{Key: ckProvider, Label: "Provider", Type: agent.FieldText, Required: true, Placeholder: "anthropic",
				Help: "Provider id (anthropic, openai, openrouter, google, groq, deepseek, …). Determines which env var the key is exported as."},
			{Key: ckAPIKey, Label: "API key", Type: agent.FieldSecret, Required: true, Placeholder: "sk-…",
				Help: "Exported to the run as the provider's API-key env var."},
			{Key: ckEnvVar, Label: "Env var (override)", Type: agent.FieldText, Placeholder: "ANTHROPIC_API_KEY",
				Help: "Optional: the exact env var name to export the key as. Leave blank to derive it from the provider."},
		},
	}}
}

func (m *authModule) Begin(_ context.Context, methodID string) (agent.AuthState, error) {
	if methodID != "apiKey" {
		return agent.AuthState{}, errors.New("unknown auth method: " + methodID)
	}
	return agent.AuthState{Method: "apiKey", Status: "needs_input",
		Fields: agent.MethodFields(m, "apiKey"), Message: "Enter your provider and API key."}, nil
}

func (m *authModule) Step(_ context.Context, input map[string]string) (agent.AuthState, error) {
	key := strings.TrimSpace(input[ckAPIKey])
	if key == "" {
		return agent.AuthState{Status: "error", Message: "no API key provided"}, nil
	}
	if err := m.store.Set(ckAPIKey, key); err != nil {
		return agent.AuthState{}, err
	}
	// Provider + optional env-var override; a trimmed-empty value clears any prior setting.
	for _, k := range []string{ckProvider, ckEnvVar} {
		if err := m.store.Set(k, strings.TrimSpace(input[k])); err != nil {
			return agent.AuthState{}, err
		}
	}
	return agent.AuthState{Method: "apiKey", Status: "complete"}, nil
}

// Status reports whether a run would authenticate, from the stored credential (env-only creds don't
// appear in opencode's auth store, so presence is authoritative).
func (m *authModule) Status(_ context.Context) agent.AuthStatus {
	if strings.TrimSpace(m.store.Get(ckAPIKey)) == "" {
		return agent.AuthStatus{Configured: false, Detail: "No API key set — add a provider key."}
	}
	detail := "API key set"
	if p := strings.TrimSpace(m.store.Get(ckProvider)); p != "" {
		detail = "API key set for " + p
	}
	return agent.AuthStatus{Configured: true, Method: "apiKey", Detail: detail}
}

// EnvForRun is the ONLY place credentials enter a run. The primary "apiKey" method's key is exported
// under the override var when set, else the provider's conventional var, else "<PROVIDER>_API_KEY" as a
// last resort. On top of that, every registered CUSTOM provider's stored key is exported under the
// env-var name it was registered with (agent.ProviderEnvForRun), so opencode.json's `{env:VAR}`
// placeholders resolve at runtime — the config file never carries a literal secret.
func (m *authModule) EnvForRun() map[string]string {
	env := map[string]string{}
	if key := strings.TrimSpace(m.store.Get(ckAPIKey)); key != "" {
		name := strings.TrimSpace(m.store.Get(ckEnvVar))
		if name == "" {
			name = providerEnvVar(strings.TrimSpace(m.store.Get(ckProvider)))
		}
		if name != "" {
			env[name] = key
		}
	}
	for name, key := range agent.ProviderEnvForRun(m.store) {
		env[name] = key
	}
	for name, key := range canonicalProviderEnv(m.store) {
		if _, taken := env[name]; !taken {
			env[name] = key
		}
	}
	return env
}

// canonicalProviderEnv is the alias repair for env-only catalog connects. models.dev declares an `env`
// LIST per provider (google: GOOGLE_GENERATIVE_AI_API_KEY, GOOGLE_API_KEY) and opencode uses that list
// to DETECT a configured provider — but the provider's SDK then reads exactly one of those names. Store
// the key under the wrong alias and opencode lists the models and dies at instantiation ("API key is
// missing"), which reads to the user as a broken pipe rather than a naming mismatch.
//
// So for every provider connected through the multi-var scheme, if we have a VERIFIED name that its SDK
// reads (canonicalEnvVar — the table, never the "<PROVIDER>_API_KEY" guess) and no entry is stored under
// it, export the stored key under that name too. Purely additive: it never renames or drops what the
// caller stored, so a run keeps whatever the catalog declared. Done at the read seam, not on connect, so
// a key already sitting on disk under the wrong alias starts working without a re-connect.
//
// Deliberately limited to the multi-var ":env:" scheme, which only ever holds env-only catalog connects.
// A CUSTOM-ENDPOINT provider stores under the legacy single-var keys and its opencode.json block pins
// that exact name in an "{env:VAR}" placeholder — aliasing there would be at best noise and at worst
// would make opencode light up a built-in brand the user never connected.
//
// Only applied when exactly one "*_API_KEY" alias is stored: a multi-key brand (Bedrock's access-key
// pair, Azure) has no single "the API key" to alias, and guessing one would export a secret under a name
// it does not belong to.
func canonicalProviderEnv(store agent.CredStore) map[string]string {
	out := map[string]string{}
	if store == nil {
		return out
	}
	// Group the multi-var entries by provider id: "provider:<id>:env:<VARNAME>" → value.
	byID := map[string]map[string]string{}
	for k, v := range store.All() {
		rest, ok := strings.CutPrefix(k, "provider:")
		if !ok {
			continue
		}
		id, name, isEnv := strings.Cut(rest, ":env:")
		if !isEnv || id == "" || name == "" || strings.TrimSpace(v) == "" {
			continue
		}
		if byID[id] == nil {
			byID[id] = map[string]string{}
		}
		byID[id][name] = strings.TrimSpace(v)
	}
	for id, stored := range byID {
		canon := canonicalEnvVar(id)
		if canon == "" || agent.ValidateEnvVarName(canon) != nil {
			continue
		}
		if _, already := stored[canon]; already {
			continue // stored under the name its SDK reads — nothing to repair.
		}
		var key string
		var keys int
		for name, val := range stored {
			if strings.HasSuffix(name, "_API_KEY") {
				key, keys = val, keys+1
			}
		}
		if keys == 1 {
			out[canon] = key
		}
	}
	return out
}

// providerEnvVar maps an opencode provider id to the API-key env var it reads. Unknown non-empty
// providers fall back to the common "<PROVIDER>_API_KEY" convention; an empty provider yields "".
func providerEnvVar(provider string) string {
	if strings.TrimSpace(provider) == "" {
		return ""
	}
	if name := canonicalEnvVar(provider); name != "" {
		return name
	}
	return strings.ToUpper(provider) + "_API_KEY"
}

// canonicalEnvVar is the VERIFIED half of that mapping: the env var a provider's SDK actually reads,
// for the brands we have checked. It returns "" rather than guessing, which is what makes it safe to
// drive the alias repair in canonicalProviderEnv — the "<PROVIDER>_API_KEY" fallback is a convention,
// not a fact, and for a hyphenated id it isn't even a legal env-var name ("amazon-bedrock").
func canonicalEnvVar(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "google", "gemini":
		return "GOOGLE_GENERATIVE_AI_API_KEY"
	case "groq":
		return "GROQ_API_KEY"
	case "mistral":
		return "MISTRAL_API_KEY"
	case "xai", "grok":
		return "XAI_API_KEY"
	default:
		return ""
	}
}
