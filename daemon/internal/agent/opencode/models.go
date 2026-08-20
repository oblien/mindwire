package opencode

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Model listing for opencode. `opencode models` prints one `provider/model` id per line (verified
// against opencode 1.14.24), which is exactly the selector opencode's prompt body wants — so the CLI is
// the source of truth and this module surfaces it verbatim.
//
// CRITICAL: the output is NOT a static offline dump — it is env-sensitive. opencode lists a provider's
// models only when that provider is authenticated, and an "Environment" provider counts as authenticated
// the moment its API-key env var is present in the process environment (verified: bare `opencode models`
// omits google entirely, while `GOOGLE_API_KEY=… opencode models` adds all 38 google rows). This is the
// mechanism behind "connect a provider → its models become usable": the /models route hands us the run
// env (agent.AuthModule.EnvForRun, which merges every connected provider's key via ProviderEnvForRun) and
// we enumerate with THAT env overlaid on the daemon's ambient environment. Enumerating with a bare shell
// (as this module used to) silently hides every connected provider's models.
//
// mindwire no longer stores the models.dev catalog in the daemon, so this module emits only the BARE ids
// opencode enumerates (id + provider); the client owns the live catalog and fills each row's
// context/cost/modality metadata by (provider, id). When `opencode models` prints nothing (opencode not
// installed), opencode declares the "*" provider scope so the client browses the whole live catalog
// instead of collapsing to free text.
var (
	_ agent.ModelsModule       = adapter{}
	_ agent.ModelCatalogModule = adapter{}
)

// modelListTTL mirrors agent.HelpCacheTTL: one `opencode models` exec per (provider-set) window, so
// opening the settings UI or hitting /models repeatedly shells out at most once per window.
const modelListTTL = 10 * time.Minute

type modelListEntry struct {
	val string
	at  time.Time
}

// modelListCache memoizes `opencode models` output keyed by the set of provider env-var NAMES present in
// the run env — the thing that changes WHICH providers opencode enumerates. The key values themselves
// don't affect the list, so keying by names keeps the cache stable across key rotation while still
// re-shelling the moment a provider is connected or disconnected. Concurrency-safe; a failed exec keeps
// the last good value for that key (a transient hiccup never blanks the picker).
var (
	modelListMu    sync.Mutex
	modelListCache = map[string]modelListEntry{}
)

// listModels runs `opencode models` with env overlaid on the daemon's ambient environment and returns
// the raw output, memoized per provider-set. nil/empty env → the bare ambient list (the default for
// callers with no run credentials, e.g. the settings schema field).
func listModels(env map[string]string) string {
	key := envKey(env)
	modelListMu.Lock()
	defer modelListMu.Unlock()
	prev := modelListCache[key]
	if prev.val != "" && time.Since(prev.at) < modelListTTL {
		return prev.val
	}
	cmd := exec.Command("opencode", "models")
	cmd.Env = mergeEnv(env)
	if out, err := cmd.CombinedOutput(); err == nil && len(out) > 0 {
		e := modelListEntry{val: string(out), at: time.Now()}
		modelListCache[key] = e
		return e.val
	}
	return prev.val // keep last good on failure
}

// envKey is the cache key: the sorted list of env-var NAMES in env (their presence, not their values, is
// what changes which providers opencode enumerates). Empty env → "" (the bare ambient list).
func envKey(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, "\n")
}

// mergeEnv overlays env onto the daemon's ambient os.Environ so opencode sees both the daemon's own
// environment (PATH, ambient keys) and the connected-provider keys. nil/empty env → nil, so the child
// inherits os.Environ() unchanged.
func mergeEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	base := os.Environ()
	over := make(map[string]bool, len(env))
	for k := range env {
		over[k] = true
	}
	out := make([]string, 0, len(base)+len(env))
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i >= 0 && over[kv[:i]] {
			continue // dropped: about to be overridden
		}
		out = append(out, kv)
	}
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// modelChoices returns the discovered provider/model ids for the given run env (empty when opencode isn't
// installed or printed nothing — the settings field then degrades to free text; never hardcode a model
// list). Pass nil for the bare ambient list.
func modelChoices(env map[string]string) []string {
	var out []string
	for _, line := range strings.Split(listModels(env), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "/") {
			out = append(out, line)
		}
	}
	return out
}

// ModelCatalogProviders returns the "*" sentinel: opencode is provider-agnostic (it uses models.dev as
// its own provider registry), so when it enumerates nothing the client sources the picker from the whole
// live catalog rather than degrading to free text.
func (adapter) ModelCatalogProviders() []string { return []string{"*"} }

// Models surfaces the `provider/model` ids `opencode models` prints — BARE (id + provider only) —
// enumerated with the run env so a connected provider's models are listed (see the env note above). The
// id stays the full `provider/model` selector opencode's prompt body wants; the client enriches label and
// metadata from the live models.dev catalog by (provider, id). A build that enumerates nothing yields
// only any registered custom-provider models, and the "*" scope tells the client to browse the full
// catalog.
func (adapter) Models(env map[string]string) ([]agent.ModelInfo, error) {
	ids := modelChoices(env)
	out := make([]agent.ModelInfo, 0, len(ids))
	for _, id := range ids {
		mi := agent.ModelInfo{ID: id, Label: id}
		if provider, _, ok := strings.Cut(id, "/"); ok {
			mi.Provider = provider
		}
		out = append(out, mi)
	}
	// Append models from any registered custom provider (opencode.json `provider.<id>`), flagged
	// Custom=true. These come from providers.go, not `opencode models`, so a build with no custom
	// provider adds nothing.
	out = append(out, customModels()...)
	return out, nil
}
