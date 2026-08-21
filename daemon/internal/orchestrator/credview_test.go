package orchestrator

import (
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
)

// openStore is a tiny helper: a fresh on-disk store under the test's temp dir.
func openStore(t *testing.T) *session.Store {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}

// A provider connection is shared across agents: a key stored through one agent's view is read back
// through another agent's view, and surfaces (bare) in All() for both — that is the whole point of the
// shared-across-agents change. Non-provider settings stay per-agent-type isolated.
func TestCredViewProvidersAreSharedAcrossAgents(t *testing.T) {
	store := openStore(t)
	oc := newCredView(store, "opencode")
	cx := newCredView(store, "codex")

	// Connect a provider through opencode's view.
	envKey := agent.ProviderEnvKey("google")   // provider:google:envVar
	credKey := agent.ProviderCredKey("google") // provider:google:apiKey
	if err := oc.Set(envKey, "GOOGLE_GENERATIVE_AI_API_KEY"); err != nil {
		t.Fatalf("set envVar: %v", err)
	}
	if err := oc.Set(credKey, "sk-google-123"); err != nil {
		t.Fatalf("set apiKey: %v", err)
	}

	// Codex's view reads the very same connection — shared, not siloed.
	if got := cx.Get(credKey); got != "sk-google-123" {
		t.Errorf("codex Get(apiKey) = %q, want the shared key", got)
	}
	if got := cx.Get(envKey); got != "GOOGLE_GENERATIVE_AI_API_KEY" {
		t.Errorf("codex Get(envVar) = %q, want the shared env var", got)
	}

	// ProviderEnvForRun over EITHER view exports the shared key under its env var — the sole seam a
	// credential enters a run, now identical for every agent.
	for name, view := range map[string]*CredView{"opencode": oc, "codex": cx} {
		env := agent.ProviderEnvForRun(view)
		if env["GOOGLE_GENERATIVE_AI_API_KEY"] != "sk-google-123" {
			t.Errorf("%s ProviderEnvForRun = %v, want the shared key exported", name, env)
		}
	}

	// The physical store key is the shared namespace, NOT either agent's type prefix.
	if store.Get(sharedCredNamespace+credKey) != "sk-google-123" {
		t.Errorf("provider key not stored under the shared namespace")
	}
	if store.Get("opencode:"+credKey) != "" || store.Get("codex:"+credKey) != "" {
		t.Errorf("provider key leaked into a per-agent namespace")
	}
}

// Non-provider settings remain per-agent-type isolated — the shared routing is scoped to the
// provider:* subtree only.
func TestCredViewNonProviderSettingsStayIsolated(t *testing.T) {
	store := openStore(t)
	oc := newCredView(store, "opencode")
	cx := newCredView(store, "codex")

	if err := oc.Set("model", "google/gemini-2.5-pro"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if got := cx.Get("model"); got != "" {
		t.Errorf("codex saw opencode's model %q — settings must stay isolated", got)
	}
	if got := oc.All()["model"]; got != "google/gemini-2.5-pro" {
		t.Errorf("opencode All()[model] = %q, want its own value", got)
	}
	if _, ok := cx.All()["model"]; ok {
		t.Errorf("codex All() surfaced opencode's model — settings must stay isolated")
	}

	// The single-slot auth key literally named "provider" (value = a provider id) must NOT be treated
	// as shared — only the "provider:<id>:" subtree is.
	if err := oc.Set("provider", "anthropic"); err != nil {
		t.Fatalf("set provider slot: %v", err)
	}
	if got := cx.Get("provider"); got != "" {
		t.Errorf("codex saw opencode's single-slot 'provider' %q — must stay isolated", got)
	}
}

// A stale per-agent provider key (written before providers were shared) is ignored by All(): the shared
// namespace is authoritative, so an old connection just re-connects once rather than double-appearing.
func TestCredViewStalePerAgentProviderKeyIgnored(t *testing.T) {
	store := openStore(t)
	// Simulate legacy data: a provider key under the old per-agent prefix.
	if err := store.Set("opencode:"+agent.ProviderEnvKey("legacy"), "LEGACY_API_KEY"); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	oc := newCredView(store, "opencode")
	if _, ok := oc.All()["provider:legacy:envVar"]; ok {
		t.Errorf("All() surfaced a stale per-agent provider key; shared namespace must be authoritative")
	}
}
