package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Custom-LLM-provider control plane for Codex, exposed through the optional agent.CustomProvidersModule
// (type-asserted by the API, never part of the mandatory Adapter). Codex reads custom OpenAI-compatible
// providers from `$CODEX_HOME/config.toml` `[model_providers.<id>]` tables — user scope only — and a run
// selects one via `model_provider = "<id>"`. Registering one here writes that table so a turn can point
// at a private endpoint.
//
// The reader/writer REUSE the minimal zero-dependency TOML scanner and section-preserving remove/append
// discipline from mcp.go: only the `[model_providers.<id>]` table mindwire manages is ever rewritten;
// every other table and key in config.toml is left byte-for-byte intact.
//
// SECURITY: the written table carries `env_key = "VAR"` — the NAME of the env var Codex reads the key
// from — never the secret. The value lives in the CredStore and reaches a run only via
// authModule.EnvForRun (which merges agent.ProviderEnvForRun). Codex's TOML has no per-provider model
// list, so the registered model ids live in the CredStore too (provider:<id>:models), echoed back by
// GET /providers for round-trip fidelity (Codex's /models stays the OpenAI catalog — a custom provider's
// model is chosen by the user, not enumerable from config alone). `wire_api = "chat"` selects the
// OpenAI-compatible Chat Completions wire.
var _ agent.CustomProvidersModule = adapter{}

const providerWireAPI = "chat"

// pkModels is the CredStore key holding a custom provider's model ids (newline-joined), since
// config.toml's [model_providers.*] has no model-list field.
func pkModels(id string) string { return "provider:" + id + ":models" }

// ProviderScopes: Codex's custom-provider config is user-scope only.
func (adapter) ProviderScopes() []agent.MemoryScope { return []agent.MemoryScope{agent.MemoryUser} }

// ListProviders parses every `[model_providers.*]` table from config.toml, filling Models/HasKey from
// the store. A missing file yields an empty map (forgiving). dir is ignored — Codex is user-only.
func (adapter) ListProviders(store agent.CredStore, scope agent.MemoryScope, _ string) (map[string]agent.CustomProvider, error) {
	if scope != agent.MemoryUser {
		return nil, fmt.Errorf("codex supports custom providers only at user scope")
	}
	path := mcpConfigPath()
	if path == "" {
		return nil, fmt.Errorf("cannot resolve codex home")
	}
	data, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	out := map[string]agent.CustomProvider{}
	for id, p := range parseModelProviders(data) {
		p.Models = storedModels(store, id)
		p.EnvVars = sortedNames(agent.StoredProviderEnv(store, id))
		p.HasKey = (store != nil && strings.TrimSpace(store.Get(agent.ProviderCredKey(id))) != "") || len(p.EnvVars) > 0
		out[id] = p
	}
	// Provider credentials live in a CROSS-AGENT namespace, and authModule.EnvForRun merges every one of
	// them into a Codex run. So a provider connected from another agent's page is already live here —
	// reporting only the ones with a config.toml table made those credentials invisible under Codex, and
	// therefore impossible to inspect or delete from a Codex-scoped surface. Report them (no base URL and
	// no models: Codex cannot ROUTE to such a provider without a table, but it does carry the key, and the
	// console has to be able to finish the connect → edit → disconnect cycle from wherever the user is).
	for id, names := range envOnlyConnections(store) {
		if _, hasTable := out[id]; hasTable {
			continue
		}
		out[id] = agent.CustomProvider{ID: id, EnvVars: names, HasKey: true}
	}
	return out, nil
}

// envOnlyConnections finds provider credentials stored WITHOUT a config.toml table, under either cred
// scheme, and returns id → the env-var NAMES (sorted, never the values). Mirrors opencode's function of
// the same name — both read the one shared "provider:*" subtree.
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
		// Multi-var: "<id>:env:<VARNAME>" — the name is after ":env:", the value is v.
		if id, name, isEnv := strings.Cut(rest, ":env:"); isEnv {
			if id != "" && name != "" && strings.TrimSpace(v) != "" {
				add(id, name)
			}
			continue
		}
		// Legacy single-var: "<id>:envVar" holds the NAME; the value is under the ":apiKey" sibling.
		if id, ok := strings.CutSuffix(rest, ":envVar"); ok && id != "" {
			if name := strings.TrimSpace(v); name != "" && strings.TrimSpace(store.Get(agent.ProviderCredKey(id))) != "" {
				add(id, name)
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

// sortedNames returns a name→value map's keys, sorted. The values are never returned.
func sortedNames(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SetProvider connects a provider to Codex. With a base URL it writes the `[model_providers.<id>]` table
// (preserving sibling config), stores the key when supplied, and records the env-var name + model ids —
// that table's `env_key` is what makes the provider routable. Without one it is a credential-only write
// into the shared provider namespace (see the ENV-ONLY branch). An empty apiKey / blank secret leaves any
// previously stored value intact (metadata-only update).
func (adapter) SetProvider(store agent.CredStore, scope agent.MemoryScope, _ string, id string, p agent.CustomProvider, apiKey string, secrets map[string]string) error {
	if scope != agent.MemoryUser {
		return fmt.Errorf("codex supports custom providers only at user scope")
	}
	if err := agent.ValidateProviderID(id); err != nil {
		return err
	}
	// ENV-ONLY: no base URL means there is no `[model_providers.<id>]` table to write — this is a
	// credential-only connect/update in the shared provider namespace (see ListProviders). Codex cannot
	// route to such a provider on its own, but it does export the key, and this is what lets the console
	// re-enter or repair a stored credential without first switching to another agent.
	if strings.TrimSpace(p.BaseURL) == "" {
		if store == nil {
			return fmt.Errorf("cannot store credentials for provider %q", id)
		}
		if len(secrets) > 0 {
			for name, val := range secrets {
				if name = strings.TrimSpace(name); name == "" {
					continue
				}
				if err := agent.ValidateEnvVarName(name); err != nil {
					return err
				}
				// A blank value leaves any stored value for that var intact ("leave blank to keep").
				if v := strings.TrimSpace(val); v != "" {
					if err := store.Set(agent.ProviderEnvValueKey(id, name), v); err != nil {
						return err
					}
				}
			}
			return nil
		}
		envVar := agent.DeriveEnvVar(id, p.EnvVar)
		if err := store.Set(agent.ProviderEnvKey(id), envVar); err != nil {
			return err
		}
		if k := strings.TrimSpace(apiKey); k != "" {
			return store.Set(agent.ProviderCredKey(id), k)
		}
		return nil
	}
	path := mcpConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve codex home")
	}
	envVar := agent.DeriveEnvVar(id, p.EnvVar)
	existing, err := readConfig(path)
	if err != nil {
		return err
	}
	body := strings.TrimRight(strings.Join(removeProvider(splitLines(existing), id), "\n"), "\n")
	section := strings.TrimLeft(buildProviderSection(id, p, envVar), "\n")
	var out string
	if strings.TrimSpace(body) == "" {
		out = section
	} else {
		out = body + "\n\n" + section
	}
	out = strings.TrimRight(out, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare codex home: %w", err)
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return err
	}
	if store != nil {
		if err := store.Set(agent.ProviderEnvKey(id), envVar); err != nil {
			return err
		}
		if err := store.Set(pkModels(id), strings.Join(cleanModels(p.Models), "\n")); err != nil {
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

// DeleteProvider removes the `[model_providers.<id>]` table and clears the stored key/env-var/models.
// Removing an absent provider (or a missing file) is not an error — idempotent.
func (adapter) DeleteProvider(store agent.CredStore, scope agent.MemoryScope, _ string, id string) error {
	if scope != agent.MemoryUser {
		return fmt.Errorf("codex supports custom providers only at user scope")
	}
	path := mcpConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve codex home")
	}
	existing, err := readConfig(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(existing) != "" {
		out := strings.TrimRight(strings.Join(removeProvider(splitLines(existing), id), "\n"), "\n")
		if out != "" {
			out += "\n"
		}
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			return err
		}
	}
	if store != nil {
		_ = store.Set(agent.ProviderCredKey(id), "")
		_ = store.Set(agent.ProviderEnvKey(id), "")
		_ = store.Set(pkModels(id), "")
		// And the multi-var subtree: without this sweep a provider connected with `secrets` survives
		// Disconnect in the shared namespace and keeps being exported into every run — a credential the
		// console reports as gone while offering no way to remove it.
		for name := range agent.StoredProviderEnv(store, id) {
			_ = store.Set(agent.ProviderEnvValueKey(id, name), "")
		}
	}
	return nil
}

// buildProviderSection renders a `[model_providers.<id>]` table (name/base_url/env_key/wire_api). Order
// of top-level tables is TOML-insignificant, so this is appended after the preserved body.
func buildProviderSection(id string, p agent.CustomProvider, envVar string) string {
	var b strings.Builder
	b.WriteString("\n[model_providers." + tomlKey(id) + "]\n")
	if name := strings.TrimSpace(p.Name); name != "" {
		b.WriteString("name = " + tomlString(name) + "\n")
	}
	b.WriteString("base_url = " + tomlString(strings.TrimSpace(p.BaseURL)) + "\n")
	b.WriteString("env_key = " + tomlString(envVar) + "\n")
	b.WriteString("wire_api = " + tomlString(providerWireAPI) + "\n")
	return b.String()
}

// removeProvider drops `[model_providers.<id>]` and any `[model_providers.<id>.*]` sub-tables (plus a
// single blank separator preceding each), preserving all other lines verbatim — mirrors removeServer.
func removeProvider(lines []string, id string) []string {
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			segs, ok := parseTableHeader(t)
			if ok && len(segs) >= 2 && segs[0] == "model_providers" && segs[1] == id {
				if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				skip = true
				continue
			}
			skip = false
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return out
}

// parseModelProviders scans config.toml and returns the providers under `[model_providers.<id>]` tables
// (name/base_url/env_key). Models/HasKey are NOT filled here (they come from the store). Best-effort,
// zero-dependency — understands the flat `key = value` lines the emitter writes.
func parseModelProviders(content string) map[string]agent.CustomProvider {
	out := map[string]agent.CustomProvider{}
	var cur string
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "[") {
			cur = ""
			segs, ok := parseTableHeader(t)
			if !ok || len(segs) < 2 || segs[0] != "model_providers" || len(segs) > 2 {
				continue // ignore other tables and any [model_providers.<id>.sub] tables
			}
			cur = segs[1]
			if _, exists := out[cur]; !exists {
				out[cur] = agent.CustomProvider{ID: cur}
			}
			continue
		}
		if cur == "" {
			continue
		}
		key, val, ok := splitKV(t)
		if !ok {
			continue
		}
		p := out[cur]
		switch key {
		case "name":
			p.Name = parseTOMLValue(val)
		case "base_url":
			p.BaseURL = parseTOMLValue(val)
		case "env_key":
			p.EnvVar = parseTOMLValue(val)
		}
		out[cur] = p
	}
	return out
}

// storedModels returns a provider's model ids from the store (newline-joined under pkModels), or nil.
func storedModels(store agent.CredStore, id string) []string {
	if store == nil {
		return nil
	}
	return cleanModels(strings.Split(store.Get(pkModels(id)), "\n"))
}

// cleanModels trims and drops empty entries, preserving order.
func cleanModels(in []string) []string {
	var out []string
	for _, m := range in {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}
