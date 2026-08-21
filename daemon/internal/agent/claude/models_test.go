package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// resetModelCache clears the package-global discover cache so a test starts cold and restores it after.
// It also stubs hostCredEnv to return nothing: model tests must never shell out to a login shell (which
// on a developer machine would inject the real host ANTHROPIC_API_KEY and turn "no-auth" into a live
// fetch). Tests exercising the host-fallback path override hostCredEnv themselves after calling this.
func resetModelCache(t *testing.T) {
	t.Helper()
	origHost := hostCredEnv
	hostCredEnv = func() map[string]string { return nil }
	t.Cleanup(func() {
		hostCredEnv = origHost
		modelMu.Lock()
		modelOpts, modelAt, modelTriedAt = nil, time.Time{}, time.Time{}
		modelMu.Unlock()
	})
}

// TestResolveModelEnv covers the credential-resolution gate that feeds ensureModels: daemon creds win,
// a cloud backend owns the env untouched, and only a credential-less daemon falls back to the host
// login-shell key — the fix for "Models empty while `claude auth status` says signed in." Network-free.
func TestResolveModelEnv(t *testing.T) {
	orig := hostCredEnv
	t.Cleanup(func() { hostCredEnv = orig })
	hostCredEnv = func() map[string]string { return map[string]string{"ANTHROPIC_API_KEY": "sk-host"} }

	// 1. Daemon holds no credential → the host key is grafted in (the reported case).
	if got := resolveModelEnv(map[string]string{}); got["ANTHROPIC_API_KEY"] != "sk-host" {
		t.Fatalf("host fallback: ANTHROPIC_API_KEY=%q, want sk-host", got["ANTHROPIC_API_KEY"])
	}
	// 2. A daemon-configured key wins; the host is never merged over it.
	if got := resolveModelEnv(map[string]string{"ANTHROPIC_API_KEY": "sk-daemon"}); got["ANTHROPIC_API_KEY"] != "sk-daemon" {
		t.Fatalf("daemon wins: ANTHROPIC_API_KEY=%q, want sk-daemon", got["ANTHROPIC_API_KEY"])
	}
	// 3. A cloud backend owns the env → no first-party key is grafted on.
	if got := resolveModelEnv(map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"}); got["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("cloud backend: unexpected ANTHROPIC_API_KEY=%q grafted", got["ANTHROPIC_API_KEY"])
	}

	// 4. No daemon cred and no host cred → nothing added (honest empty).
	hostCredEnv = func() map[string]string { return nil }
	if got := resolveModelEnv(map[string]string{}); len(got) != 0 {
		t.Fatalf("no creds: got %v, want empty", got)
	}
}

// TestEnsureModelsHeaders proves the fetch reproduces the CLI's request shape when the host provides a
// gateway config: the credential (x-api-key), the workspace header (from ANTHROPIC_WORKSPACE_ID), and any
// ANTHROPIC_CUSTOM_HEADERS all reach the endpoint, and ANTHROPIC_BASE_URL redirects the call. Without the
// workspace header a real gateway returns 400 — so asserting it is what guards the reported bug. The env
// is passed inline (as a daemon-configured cred would be), so no login shell is consulted.
func TestEnsureModelsHeaders(t *testing.T) {
	resetModelCache(t)
	// Start cold: a prior test may have left the cache fresh or inside the backoff window, which would
	// short-circuit ensureModels before it ever issues the request.
	modelMu.Lock()
	modelOpts, modelAt, modelTriedAt = nil, time.Time{}, time.Time{}
	modelMu.Unlock()

	var gotKey, gotWorkspace, gotVersion, gotCustom, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotWorkspace = r.Header.Get("anthropic-workspace-id")
		gotVersion = r.Header.Get("anthropic-version")
		gotCustom = r.Header.Get("X-Gateway-Tenant")
		gotPath = r.URL.Path
		// The workspace header is mandatory on this gateway — refuse without it, exactly like the real one.
		if gotWorkspace == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5","display_name":"Opus 5"}]}`))
	}))
	t.Cleanup(srv.Close)

	ensureModels(map[string]string{
		"ANTHROPIC_API_KEY":        "sk-gateway",
		"ANTHROPIC_BASE_URL":       srv.URL,
		"ANTHROPIC_WORKSPACE_ID":   "ws-123",
		"ANTHROPIC_CUSTOM_HEADERS": "X-Gateway-Tenant: acme",
	})

	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotKey != "sk-gateway" {
		t.Fatalf("x-api-key = %q, want sk-gateway", gotKey)
	}
	if gotWorkspace != "ws-123" {
		t.Fatalf("anthropic-workspace-id = %q, want ws-123", gotWorkspace)
	}
	if gotVersion != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want 2023-06-01", gotVersion)
	}
	if gotCustom != "acme" {
		t.Fatalf("X-Gateway-Tenant = %q, want acme (from ANTHROPIC_CUSTOM_HEADERS)", gotCustom)
	}
	if got := knownModels(); len(got) != 1 || got[0].ID != "claude-opus-5" {
		t.Fatalf("cache = %+v, want one claude-opus-5 row", got)
	}
}

// TestApplyCustomHeaders covers the ANTHROPIC_CUSTOM_HEADERS parser: multiple headers, the literal `\n`
// escape form Claude Code accepts, values containing a colon, and skipping of blank/nameless lines.
func TestApplyCustomHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	applyCustomHeaders(req, `X-One: a\nX-Two:  b:c \n\n: skipme \nnocolon`)
	if got := req.Header.Get("X-One"); got != "a" {
		t.Fatalf("X-One = %q, want a", got)
	}
	if got := req.Header.Get("X-Two"); got != "b:c" {
		t.Fatalf("X-Two = %q, want b:c (only the first colon splits)", got)
	}
	if len(req.Header) != 2 {
		t.Fatalf("header count = %d, want 2 (blank/nameless/colonless lines skipped): %v", len(req.Header), req.Header)
	}
}

// TestClaudeModelsModule asserts the adapter maps discover.go's private ModelOpt cache to the public
// agent.ModelInfo, keeps provider=anthropic, preserves the account list's order, and advertises the
// capability. The cache is seeded directly (marked fresh) so ensureModels short-circuits without ever
// touching the network. The daemon no longer stores the models.dev catalog, so the row is BARE
// (id/label/provider) — the client enriches context/cost/modalities from the live catalog.
func TestClaudeModelsModule(t *testing.T) {
	resetModelCache(t)
	a := adapter{}
	if !a.Capabilities().Models {
		t.Fatal("claude caps: Models=false, want true")
	}

	modelMu.Lock()
	modelOpts = []ModelOpt{{ID: "claude-opus-5", Label: "Opus 5"}, {ID: "claude-sonnet-5", Label: "Sonnet 5"}}
	modelAt = time.Now()      // fresh → ensureModels is a no-op regardless of env
	modelTriedAt = time.Now() // backoff window → also a no-op
	modelMu.Unlock()

	got, err := a.Models(nil) // nil env: no credential → ensureModels never hits the network
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	// The account list is the source of WHICH models and their order; provider is always anthropic; the
	// account display_name wins the label even when the catalog enriched the rest of the row.
	if len(got) != 2 {
		t.Fatalf("Models n=%d, want 2", len(got))
	}
	for i, want := range []struct{ id, label string }{
		{"claude-opus-5", "Opus 5"}, {"claude-sonnet-5", "Sonnet 5"},
	} {
		if got[i].ID != want.id || got[i].Label != want.label || got[i].Provider != "anthropic" {
			t.Fatalf("Models[%d] = {id:%q label:%q provider:%q}, want {id:%q label:%q provider:anthropic}",
				i, got[i].ID, got[i].Label, got[i].Provider, want.id, want.label)
		}
	}
}

// TestClaudeModelsBare asserts the daemon emits the account row WITHOUT catalog metadata (the client
// enriches): only id/label/provider are populated, with no context/cost attached daemon-side.
func TestClaudeModelsBare(t *testing.T) {
	resetModelCache(t)
	modelMu.Lock()
	modelOpts = []ModelOpt{{ID: "claude-opus-5", Label: "Account Opus 5"}}
	modelAt, modelTriedAt = time.Now(), time.Now()
	modelMu.Unlock()

	got, err := adapter{}.Models(nil)
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Models n=%d, want 1", len(got))
	}
	m := got[0]
	if m.ID != "claude-opus-5" || m.Label != "Account Opus 5" || m.Provider != "anthropic" {
		t.Fatalf("row = {id:%q label:%q provider:%q}, want {claude-opus-5, Account Opus 5, anthropic}", m.ID, m.Label, m.Provider)
	}
	// No catalog in the daemon → no enrichment: context/cost stay zero-valued.
	if m.Context != 0 || m.MaxOut != 0 || m.Cost != nil {
		t.Fatalf("unexpected daemon-side enrichment: %+v", m)
	}
}

// TestClaudeModelsNoAuth verifies that with no credential and a cold cache the list is empty — a valid
// result, not an error. ensureModels returns at the no-credential branch before any HTTP request.
func TestClaudeModelsNoAuth(t *testing.T) {
	resetModelCache(t)
	modelMu.Lock()
	modelOpts, modelAt, modelTriedAt = nil, time.Time{}, time.Time{}
	modelMu.Unlock()

	got, err := adapter{}.Models(map[string]string{}) // no creds → no network
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Models = %v, want empty under no-auth", got)
	}
}
