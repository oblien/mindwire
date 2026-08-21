package opencode

import (
	"context"
	"testing"
)

// mapStore is an in-memory agent.CredStore for exercising the auth module.
type mapStore map[string]string

func (s mapStore) Get(k string) string    { return s[k] }
func (s mapStore) Set(k, v string) error  { s[k] = v; return nil }
func (s mapStore) All() map[string]string { return s }

// A full apiKey setup persists the credential + provider, tags the method complete, and EnvForRun then
// exports the key under the provider's conventional env var (anthropic → ANTHROPIC_API_KEY).
func TestStepAPIKeyAndEnv(t *testing.T) {
	store := mapStore{}
	m := newAuth(store)

	st, err := m.Step(context.Background(), map[string]string{
		ckProvider: "anthropic",
		ckAPIKey:   "sk-abc",
	})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if st.Method != "apiKey" || st.Status != "complete" {
		t.Fatalf("Step state = %+v, want apiKey/complete", st)
	}
	assertEnv(t, m.EnvForRun(), map[string]string{"ANTHROPIC_API_KEY": "sk-abc"})
}

// An explicit env-var override wins over the provider-derived name so a non-standard var is honored.
func TestEnvForRunOverride(t *testing.T) {
	m := newAuth(mapStore{ckProvider: "anthropic", ckAPIKey: "sk-x", ckEnvVar: "MY_KEY"})
	assertEnv(t, m.EnvForRun(), map[string]string{"MY_KEY": "sk-x"})
}

// An unknown provider falls back to the "<PROVIDER>_API_KEY" convention so a run still authenticates.
func TestEnvForRunUnknownProviderFallback(t *testing.T) {
	m := newAuth(mapStore{ckProvider: "acme", ckAPIKey: "sk-y"})
	assertEnv(t, m.EnvForRun(), map[string]string{"ACME_API_KEY": "sk-y"})
}

// With no key set, EnvForRun exports nothing (a run inherits only the ambient environment).
func TestEnvForRunNoKey(t *testing.T) {
	assertEnv(t, newAuth(mapStore{ckProvider: "anthropic"}).EnvForRun(), map[string]string{})
}

// With a key but no provider and no override there is no var name to export under, so nothing leaks.
func TestEnvForRunNoProviderNoOverride(t *testing.T) {
	assertEnv(t, newAuth(mapStore{ckAPIKey: "sk-z"}).EnvForRun(), map[string]string{})
}

// providerEnvVar maps the known providers and falls back for the rest.
func TestProviderEnvVar(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"anthropic":  "ANTHROPIC_API_KEY",
		"openai":     "OPENAI_API_KEY",
		"deepseek":   "DEEPSEEK_API_KEY",
		"openrouter": "OPENROUTER_API_KEY",
		"google":     "GOOGLE_GENERATIVE_AI_API_KEY",
		"gemini":     "GOOGLE_GENERATIVE_AI_API_KEY",
		"groq":       "GROQ_API_KEY",
		"mistral":    "MISTRAL_API_KEY",
		"xai":        "XAI_API_KEY",
		"grok":       "XAI_API_KEY",
		"Anthropic":  "ANTHROPIC_API_KEY", // case-insensitive
		"cohere":     "COHERE_API_KEY",    // unknown → convention
	}
	for provider, want := range cases {
		if got := providerEnvVar(provider); got != want {
			t.Errorf("providerEnvVar(%q) = %q, want %q", provider, got, want)
		}
	}
}

// Status is presence-based (env-only creds don't show in opencode's own auth store).
func TestStatus(t *testing.T) {
	if got := newAuth(mapStore{ckAPIKey: "sk", ckProvider: "anthropic"}).Status(context.Background()); !got.Configured || got.Method != "apiKey" {
		t.Errorf("api key status = %+v, want configured/apiKey", got)
	}
	if got := newAuth(mapStore{}).Status(context.Background()); got.Configured {
		t.Errorf("empty store status = %+v, want not configured", got)
	}
}

// An empty step is an error, not a silent success.
func TestStepNoCredentialErrors(t *testing.T) {
	st, _ := newAuth(mapStore{}).Step(context.Background(), map[string]string{})
	if st.Status != "error" {
		t.Errorf("empty Step status = %q, want error", st.Status)
	}
}

func assertEnv(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("EnvForRun = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("EnvForRun[%q] = %q, want %q", k, got[k], v)
		}
	}
}
