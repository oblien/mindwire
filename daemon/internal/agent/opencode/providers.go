package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Custom-LLM-provider control plane for opencode, exposed through the optional
// agent.CustomProvidersModule (type-asserted by the API, never part of the mandatory Adapter). opencode
// reads custom providers from the top-level `provider` object of its user config, opencode.json — each
// entry `{npm, name, options:{baseURL, apiKey}, models:{...}}` — and loads them on every run. Registering
// one here makes its models selectable and lets a turn point at an endpoint the on-disk catalog has never
// heard of.
//
// SURGICAL IO: opencode.json holds unrelated top-level keys ($schema, model, theme, mcp, …). We decode
// the whole file to map[string]json.RawMessage, mutate ONLY the `provider` subtree, and re-marshal, so
// every sibling key is preserved byte-for-byte (only JSON key order / indentation is normalized). We
// NEVER surface any key other than `provider`.
//
// SECURITY: the written block references the key solely through opencode's own `{env:VAR}` placeholder;
// the secret value lives in the CredStore and reaches a run only via authModule.EnvForRun (which merges
// agent.ProviderEnvForRun). GET reports HasKey, never the key. Every custom provider is registered as
// OpenAI-compatible (npm "@ai-sdk/openai-compatible"), matching the OpenAI-compatible base-URL contract.
var _ agent.CustomProvidersModule = adapter{}

const openaiCompatibleNPM = "@ai-sdk/openai-compatible"

// ocProvider is the subset of an opencode.json `provider.<id>` block mindwire reads and writes. Unknown
// fields within a block we own are not preserved (Set replaces the whole entry, mirroring the MCP
// writers); sibling providers and all other top-level keys are preserved.
type ocProvider struct {
	NPM     string             `json:"npm,omitempty"`
	Name    string             `json:"name,omitempty"`
	Options ocOptions          `json:"options,omitempty"`
	Models  map[string]ocModel `json:"models,omitempty"`
}

type ocOptions struct {
	BaseURL string `json:"baseURL,omitempty"`
	APIKey  string `json:"apiKey,omitempty"`
}

type ocModel struct {
	Name string `json:"name,omitempty"`
}

// ProviderScopes: opencode's custom-provider config is user-scope only (opencode.json under the config
// home).
func (adapter) ProviderScopes() []agent.MemoryScope { return []agent.MemoryScope{agent.MemoryUser} }

// providerConfigPath is opencode.json (same file ConfigPath surfaces). Empty when the config home can't
// be resolved.
func providerConfigPath() string {
	base := configDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "opencode.json")
}

// ListProviders returns every registered provider keyed by id, with HasKey resolved from the store
// (never the config file). Two kinds coexist: providers with an opencode.json `provider.<id>` block
// (custom OpenAI-compatible endpoints), and ENV-ONLY connections — a catalog brand (e.g. Google) whose
// key we relay under its env var without writing any block, because opencode's built-in catalog already
// defines the models (see SetProvider). Both are surfaced so the console can show either as connected. A
// missing config file yields the env-only set alone (forgiving). dir is ignored — opencode is user-only.
func (adapter) ListProviders(store agent.CredStore, scope agent.MemoryScope, _ string) (map[string]agent.CustomProvider, error) {
	if scope != agent.MemoryUser {
		return nil, fmt.Errorf("opencode supports custom providers only at user scope")
	}
	path := providerConfigPath()
	if path == "" {
		return nil, fmt.Errorf("cannot resolve opencode config home")
	}
	blocks, err := readProviderBlocks(path)
	if err != nil {
		return nil, err
	}
	out := map[string]agent.CustomProvider{}
	for id, raw := range blocks {
		var b ocProvider
		if err := json.Unmarshal(raw, &b); err != nil {
			continue // skip a malformed sibling rather than fail the whole list
		}
		out[id] = agent.CustomProvider{
			ID:      id,
			Name:    b.Name,
			BaseURL: b.Options.BaseURL,
			Models:  sortedModelIDs(b.Models),
			EnvVar:  envVarFromPlaceholder(b.Options.APIKey),
			HasKey:  store != nil && strings.TrimSpace(store.Get(agent.ProviderCredKey(id))) != "",
		}
	}
	// Union in env-only connections: catalog brands stored with no config block above. These carry no base
	// URL / model list — the catalog owns those — only the env-var NAME(s) + HasKey. A block always wins
	// (custom endpoints are the richer record). Collision-safe with the single-slot auth method, whose keys
	// (`apiKey`/`provider`/`envVar`) lack the `provider:<id>:` shape this matches.
	for id, names := range envOnlyConnections(store) {
		if _, ok := out[id]; ok {
			continue
		}
		first := ""
		if len(names) > 0 {
			first = names[0]
		}
		out[id] = agent.CustomProvider{
			ID:      id,
			EnvVar:  first, // first name keeps the existing single-var pill working
			EnvVars: names,
			// Multi-var entries always carry a value (StoredProviderEnv skips empties); the legacy single
			// var stores its value separately under :apiKey.
			HasKey: len(agent.StoredProviderEnv(store, id)) > 0 ||
				strings.TrimSpace(store.Get(agent.ProviderCredKey(id))) != "",
		}
	}
	return out, nil
}

// envOnlyConnections returns id → sorted env-var NAMES for every credential stored WITHOUT an opencode.json
// block, unioning both storage schemes: the legacy single `provider:<id>:envVar` (one name) and the
// multi-var `provider:<id>:env:<VARNAME>` entries (one name each). Names are deduplicated and sorted for a
// stable display order. Empty when the store is nil. Mirrors the key shapes agent.ProviderEnvForRun consumes.
func envOnlyConnections(store agent.CredStore) map[string][]string {
	out := map[string][]string{}
	if store == nil {
		return out
	}
	sets := map[string]map[string]bool{}
	add := func(id, name string) {
		if sets[id] == nil {
			sets[id] = map[string]bool{}
		}
		sets[id][name] = true
	}
	for k, v := range store.All() {
		rest, ok := strings.CutPrefix(k, "provider:")
		if !ok {
			continue
		}
		// Multi-var: "<id>:env:<VARNAME>" — the value is v, the name is after ":env:". ":envVar" has no
		// ":env:" substring, so a legacy key never matches here and falls through to the branch below.
		if id, name, isEnv := strings.Cut(rest, ":env:"); isEnv {
			if id != "" && name != "" && strings.TrimSpace(v) != "" {
				add(id, name)
			}
			continue
		}
		// Legacy single-var: "<id>:envVar" holds the NAME (value lives under the :apiKey sibling).
		if id, ok := strings.CutSuffix(rest, ":envVar"); ok && id != "" {
			if envVar := strings.TrimSpace(v); envVar != "" {
				add(id, envVar)
			}
		}
	}
	for id, set := range sets {
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		out[id] = names
	}
	return out
}

// SetProvider connects a provider to opencode. Two shapes, chosen by whether a base URL is supplied:
//
//   - ENV-ONLY (no base URL): a catalog brand opencode already knows (e.g. Google, AWS Bedrock). opencode's
//     built-in provider definition supplies the models, so NO opencode.json block is written — we store
//     only the credential(s), which enter a run solely via EnvForRun → agent.ProviderEnvForRun. Two ways
//     to supply them: `secrets` (a NAME→VALUE map, one entry per env var the catalog declares — the
//     multi-var path for Bedrock/Azure/Vertex, and the N=1 case for single-key brands), or the single
//     `apiKey` under a derived/one env var (the legacy single-key callers). This is the clean "paste the
//     key(s) and the provider's models are usable" path.
//   - CUSTOM ENDPOINT (base URL present): an OpenAI-compatible provider the on-disk catalog has never
//     heard of. We write the provider.<id> block (preserving all sibling config) AND store the single key.
//     `secrets` is not used here (a custom endpoint authenticates through one key).
//
// Either way, an empty apiKey / blank secret leaves any previously stored value intact (metadata-only
// update), and the env-var name is recorded so EnvForRun knows where to inject the key.
func (adapter) SetProvider(store agent.CredStore, scope agent.MemoryScope, _ string, id string, p agent.CustomProvider, apiKey string, secrets map[string]string) error {
	if scope != agent.MemoryUser {
		return fmt.Errorf("opencode supports custom providers only at user scope")
	}
	if err := agent.ValidateProviderID(id); err != nil {
		return err
	}
	// Env-only connect: no config file to touch — just persist the credential(s) so ProviderEnvForRun
	// re-exports them at run time.
	if strings.TrimSpace(p.BaseURL) == "" {
		if store == nil {
			return fmt.Errorf("cannot store credentials for provider %q", id)
		}
		// Multi-var path: store one namespaced entry per declared env var. Names come straight from the
		// catalog (authoritative for opencode's built-in providers), so we validate but never derive them.
		// A blank value leaves any previously stored value for that var intact ("leave blank to keep").
		if len(secrets) > 0 {
			for name, val := range secrets {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				if err := agent.ValidateEnvVarName(name); err != nil {
					return err
				}
				if v := strings.TrimSpace(val); v != "" {
					if err := store.Set(agent.ProviderEnvValueKey(id, name), v); err != nil {
						return err
					}
				}
			}
			return nil
		}
		// Single-key fallback (legacy callers / single-key catalog brands): store the key under the
		// derived/one env var and record that name for EnvForRun. With no explicit override, prefer the
		// name the brand's SDK is KNOWN to read over the "<ID>_API_KEY" derivation — for google those
		// differ (GOOGLE_GENERATIVE_AI_API_KEY vs GOOGLE_API_KEY) and only the former authenticates. This
		// branch is env-only by construction, so there is no "{env:VAR}" placeholder to keep in step.
		envVar := strings.TrimSpace(p.EnvVar)
		if envVar == "" {
			envVar = canonicalEnvVar(id)
		}
		envVar = agent.DeriveEnvVar(id, envVar)
		if err := store.Set(agent.ProviderEnvKey(id), envVar); err != nil {
			return err
		}
		if k := strings.TrimSpace(apiKey); k != "" {
			if err := store.Set(agent.ProviderCredKey(id), k); err != nil {
				return err
			}
		}
		return nil
	}
	path := providerConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve opencode config home")
	}
	envVar := agent.DeriveEnvVar(id, p.EnvVar)
	block := ocProvider{
		NPM:     openaiCompatibleNPM,
		Name:    strings.TrimSpace(p.Name),
		Options: ocOptions{BaseURL: strings.TrimSpace(p.BaseURL), APIKey: "{env:" + envVar + "}"},
		Models:  map[string]ocModel{},
	}
	for _, m := range p.Models {
		if m = strings.TrimSpace(m); m != "" {
			block.Models[m] = ocModel{}
		}
	}
	raw, err := json.Marshal(block)
	if err != nil {
		return err
	}
	if err := mutateProviderBlocks(path, func(blocks map[string]json.RawMessage) error {
		blocks[id] = raw
		return nil
	}); err != nil {
		return err
	}
	// Persist the env-var name unconditionally (so EnvForRun knows where to inject), and the key only
	// when supplied.
	if store != nil {
		if err := store.Set(agent.ProviderEnvKey(id), envVar); err != nil {
			return err
		}
		if k := strings.TrimSpace(apiKey); k != "" {
			if err := store.Set(agent.ProviderCredKey(id), k); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteProvider removes provider.<id> from opencode.json and clears its stored key + env-var. Removing
// an absent provider (or a missing file) is not an error — idempotent.
func (adapter) DeleteProvider(store agent.CredStore, scope agent.MemoryScope, _ string, id string) error {
	if scope != agent.MemoryUser {
		return fmt.Errorf("opencode supports custom providers only at user scope")
	}
	path := providerConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve opencode config home")
	}
	if err := mutateProviderBlocks(path, func(blocks map[string]json.RawMessage) error {
		delete(blocks, id)
		return nil
	}); err != nil {
		return err
	}
	if store != nil {
		_ = store.Set(agent.ProviderCredKey(id), "")
		_ = store.Set(agent.ProviderEnvKey(id), "")
		// Clear every multi-var entry provider:<id>:env:<VAR>. Snapshot the keys first so we don't mutate
		// while ranging the store's map. Set("") stores empty (StoredProviderEnv/ProviderEnvForRun skip it).
		prefix := "provider:" + id + ":env:"
		var stale []string
		for k := range store.All() {
			if strings.HasPrefix(k, prefix) {
				stale = append(stale, k)
			}
		}
		for _, k := range stale {
			_ = store.Set(k, "")
		}
	}
	return nil
}

// customModels surfaces the registered custom providers' models as agent.ModelInfo with Custom=true, for
// Models() to append. Metadata is sparse (a custom endpoint isn't in the on-disk catalog) — id, label,
// provider, and the Custom flag only. Best-effort: a resolution/parse failure yields nothing.
func customModels() []agent.ModelInfo {
	path := providerConfigPath()
	if path == "" {
		return nil
	}
	blocks, err := readProviderBlocks(path)
	if err != nil {
		return nil
	}
	var out []agent.ModelInfo
	ids := make([]string, 0, len(blocks))
	for id := range blocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, provider := range ids {
		var b ocProvider
		if err := json.Unmarshal(blocks[provider], &b); err != nil {
			continue
		}
		for _, m := range sortedModelIDs(b.Models) {
			full := provider + "/" + m
			out = append(out, agent.ModelInfo{ID: full, Label: full, Provider: provider, Custom: true})
		}
	}
	return out
}

// ---- subtree-preserving opencode.json IO -----------------------------------

// readTopLevel decodes opencode.json into its top-level object. A missing/empty file yields an empty
// map (forgiving). opencode config is JSONC ("both JSON and JSONC … formats"), so a strict parse is
// tried first and, only if it fails, retried after stripping JSONC comments + trailing commas — a
// well-formed file never touches the sanitizer, and a genuinely broken one surfaces its ORIGINAL
// (pre-sanitize) parse error, which points at the real offending byte.
//
// NOTE: the matching writer (mutateSubtree) re-marshals the whole object, so comments the user placed
// in opencode.json are NOT preserved across a Set/Delete — reads tolerate JSONC, writes normalize to
// strict JSON (indentation/key order already normalized). opencode re-reads the normalized file fine.
func readTopLevel(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	top := map[string]json.RawMessage{}
	if strings.TrimSpace(string(data)) == "" {
		return top, nil
	}
	if err := json.Unmarshal(data, &top); err != nil {
		clean := map[string]json.RawMessage{}
		if err2 := json.Unmarshal(stripJSONC(data), &clean); err2 == nil {
			return clean, nil
		}
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return top, nil
}

// readSubtree returns one top-level object subtree of opencode.json keyed by id (raw), or an empty map
// when absent. Shared by the custom-provider (`provider`) and persistent-MCP (`mcp`) readers — the only
// two top-level keys mindwire owns.
func readSubtree(path, key string) (map[string]json.RawMessage, error) {
	top, err := readTopLevel(path)
	if err != nil {
		return nil, err
	}
	blocks := map[string]json.RawMessage{}
	if raw, ok := top[key]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return map[string]json.RawMessage{}, nil // tolerate a malformed subtree on read
		}
	}
	return blocks, nil
}

// mutateSubtree applies fn to one top-level object subtree (`key`) and writes the file back, leaving
// every other top-level key byte-for-byte intact. Written 0600 (siblings may hold secrets). An empty
// subtree after mutation drops the key entirely rather than writing `"key":{}`. Shared by the
// custom-provider (`provider`) and persistent-MCP (`mcp`) writers.
func mutateSubtree(path, key string, fn func(blocks map[string]json.RawMessage) error) error {
	top, err := readTopLevel(path)
	if err != nil {
		return err
	}
	blocks := map[string]json.RawMessage{}
	if raw, ok := top[key]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &blocks); err != nil {
			blocks = map[string]json.RawMessage{} // replace a malformed subtree
		}
	}
	if err := fn(blocks); err != nil {
		return err
	}
	if len(blocks) == 0 {
		delete(top, key)
	} else {
		subtree, err := json.Marshal(blocks)
		if err != nil {
			return err
		}
		top[key] = subtree
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare config dir: %w", err)
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// readProviderBlocks / mutateProviderBlocks are the `provider`-subtree specializations used throughout
// providers.go.
func readProviderBlocks(path string) (map[string]json.RawMessage, error) {
	return readSubtree(path, "provider")
}

func mutateProviderBlocks(path string, fn func(blocks map[string]json.RawMessage) error) error {
	return mutateSubtree(path, "provider", fn)
}

// ---- helpers ---------------------------------------------------------------

// sortedModelIDs returns the model ids of a provider block, sorted for a deterministic round-trip.
func sortedModelIDs(m map[string]ocModel) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// envVarFromPlaceholder extracts VAR from an opencode `{env:VAR}` apiKey placeholder, returning "" when
// the value isn't a placeholder (e.g. a hand-written literal — which mindwire never emits).
func envVarFromPlaceholder(s string) string {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "{env:"); ok {
		if v, ok := strings.CutSuffix(rest, "}"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
