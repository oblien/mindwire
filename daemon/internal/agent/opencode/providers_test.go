package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestProvidersRoundTrip verifies the opencode.json materialization end-to-end: a Set writes the
// `provider.<id>` block with an `{env:VAR}` placeholder (never the literal secret) while preserving every
// unrelated top-level key byte-for-byte; List/customModels/EnvForRun read it back; and Delete removes only
// that subtree (dropping `provider` entirely when it empties) and clears the stored key.
func TestProvidersRoundTrip(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := providerConfigPath()

	// Seed opencode.json with unrelated top-level keys the writer must preserve.
	seed := `{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4",
  "theme": "system"
}
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir opencode config home: %v", err)
	}
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed opencode.json: %v", err)
	}

	store := mapStore{}
	a := adapter{}
	p := agent.CustomProvider{
		Name:    "My LLM",
		BaseURL: "https://llm.example/v1",
		Models:  []string{"m-large", "m-small"},
	}
	if err := a.SetProvider(store, agent.MemoryUser, "", "my-llm", p, "sk-secret-123", nil); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}

	// The file must not contain the literal secret anywhere.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	if strings.Contains(string(raw), "sk-secret-123") {
		t.Fatalf("opencode.json leaked the api key:\n%s", raw)
	}

	// Sibling top-level keys are preserved; the provider block carries the placeholder + models.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("parse opencode.json: %v", err)
	}
	for _, k := range []string{"$schema", "model", "theme"} {
		if _, ok := top[k]; !ok {
			t.Fatalf("SetProvider dropped sibling top-level key %q; file:\n%s", k, raw)
		}
	}
	var blocks map[string]ocProvider
	if err := json.Unmarshal(top["provider"], &blocks); err != nil {
		t.Fatalf("parse provider subtree: %v", err)
	}
	block := blocks["my-llm"]
	if block.NPM != openaiCompatibleNPM {
		t.Fatalf("npm = %q, want %q", block.NPM, openaiCompatibleNPM)
	}
	if block.Options.BaseURL != "https://llm.example/v1" {
		t.Fatalf("baseURL = %q", block.Options.BaseURL)
	}
	if block.Options.APIKey != "{env:MY_LLM_API_KEY}" {
		t.Fatalf("apiKey placeholder = %q, want {env:MY_LLM_API_KEY}", block.Options.APIKey)
	}
	if _, ok := block.Models["m-large"]; !ok {
		t.Fatalf("models = %v, want m-large present", block.Models)
	}

	// The store holds the secret + env-var name, never the config.
	if store[agent.ProviderCredKey("my-llm")] != "sk-secret-123" {
		t.Fatalf("stored key = %q", store[agent.ProviderCredKey("my-llm")])
	}
	if store[agent.ProviderEnvKey("my-llm")] != "MY_LLM_API_KEY" {
		t.Fatalf("stored env var = %q", store[agent.ProviderEnvKey("my-llm")])
	}

	// List reflects HasKey + the derived env var.
	list, err := a.ListProviders(store, agent.MemoryUser, "")
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	got := list["my-llm"]
	if !got.HasKey || got.EnvVar != "MY_LLM_API_KEY" || len(got.Models) != 2 {
		t.Fatalf("ListProviders my-llm = %+v", got)
	}

	// EnvForRun exports the stored key under its env var (the sole seam creds enter a run).
	env := newAuth(store).EnvForRun()
	if env["MY_LLM_API_KEY"] != "sk-secret-123" {
		t.Fatalf("EnvForRun = %v, want MY_LLM_API_KEY=sk-secret-123", env)
	}

	// customModels surfaces the registered models with Custom=true.
	var found bool
	for _, m := range customModels() {
		if m.ID == "my-llm/m-large" {
			found = m.Custom && m.Provider == "my-llm"
		}
	}
	if !found {
		t.Fatalf("customModels did not surface my-llm/m-large with Custom=true")
	}

	// A metadata-only update (empty apiKey) keeps the stored key.
	if err := a.SetProvider(store, agent.MemoryUser, "", "my-llm", agent.CustomProvider{BaseURL: "https://llm.example/v2"}, "", nil); err != nil {
		t.Fatalf("SetProvider (update): %v", err)
	}
	if store[agent.ProviderCredKey("my-llm")] != "sk-secret-123" {
		t.Fatalf("metadata-only update dropped the stored key")
	}

	// Delete removes the block, drops the now-empty `provider` key, and clears the stored secret.
	if err := a.DeleteProvider(store, agent.MemoryUser, "", "my-llm"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	raw, _ = os.ReadFile(path)
	var afterDelete map[string]json.RawMessage
	if err := json.Unmarshal(raw, &afterDelete); err != nil {
		t.Fatalf("parse after delete: %v", err)
	}
	if _, ok := afterDelete["provider"]; ok {
		t.Fatalf("Delete left a `provider` key behind:\n%s", raw)
	}
	if _, ok := afterDelete["model"]; !ok {
		t.Fatalf("Delete dropped a sibling top-level key:\n%s", raw)
	}
	if store[agent.ProviderCredKey("my-llm")] != "" {
		t.Fatalf("Delete left the stored key behind: %q", store[agent.ProviderCredKey("my-llm")])
	}

	// Deleting again (and from a file with no `provider`) is idempotent.
	if err := a.DeleteProvider(store, agent.MemoryUser, "", "my-llm"); err != nil {
		t.Fatalf("DeleteProvider (idempotent): %v", err)
	}
}

// TestProvidersEnvOnlyConnect verifies the base-URL-free "connect a catalog brand by key" path: no
// opencode.json block is written (the built-in provider already defines the models), the key + derived
// env var land in the store, ListProviders surfaces the connection with HasKey, EnvForRun exports it,
// and Delete clears it. This is the clean provider-connect flow the console drives for opencode.
func TestProvidersEnvOnlyConnect(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := providerConfigPath()

	store := mapStore{}
	a := adapter{}
	// Google-style connect: catalog provides the env var name, no base URL, no model list.
	p := agent.CustomProvider{Name: "Google", EnvVar: "GOOGLE_GENERATIVE_AI_API_KEY"}
	if err := a.SetProvider(store, agent.MemoryUser, "", "google", p, "AIza-secret", nil); err != nil {
		t.Fatalf("SetProvider (env-only): %v", err)
	}

	// No opencode.json block is written — an env-only connect must not create/touch the config file.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), "\"provider\"") {
			t.Fatalf("env-only connect wrote a provider block:\n%s", raw)
		}
	}

	// The store holds the secret + env-var name.
	if store[agent.ProviderCredKey("google")] != "AIza-secret" {
		t.Fatalf("stored key = %q", store[agent.ProviderCredKey("google")])
	}
	if store[agent.ProviderEnvKey("google")] != "GOOGLE_GENERATIVE_AI_API_KEY" {
		t.Fatalf("stored env var = %q", store[agent.ProviderEnvKey("google")])
	}

	// ListProviders surfaces the env-only connection with HasKey (no base URL / models).
	list, err := a.ListProviders(store, agent.MemoryUser, "")
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	got := list["google"]
	if !got.HasKey || got.EnvVar != "GOOGLE_GENERATIVE_AI_API_KEY" || got.BaseURL != "" || len(got.Models) != 0 {
		t.Fatalf("ListProviders google = %+v", got)
	}

	// EnvForRun exports the stored key under its env var (the sole seam creds enter a run).
	if env := newAuth(store).EnvForRun(); env["GOOGLE_GENERATIVE_AI_API_KEY"] != "AIza-secret" {
		t.Fatalf("EnvForRun = %v, want GOOGLE_GENERATIVE_AI_API_KEY=AIza-secret", env)
	}

	// A second env-only connect does NOT overwrite the first (multi-provider; the single-slot auth bug).
	if err := a.SetProvider(store, agent.MemoryUser, "", "openai", agent.CustomProvider{EnvVar: "OPENAI_API_KEY"}, "sk-second", nil); err != nil {
		t.Fatalf("SetProvider (second env-only): %v", err)
	}
	if store[agent.ProviderCredKey("google")] != "AIza-secret" {
		t.Fatalf("second connect clobbered the first provider's key")
	}
	list, _ = a.ListProviders(store, agent.MemoryUser, "")
	if !list["google"].HasKey || !list["openai"].HasKey {
		t.Fatalf("both providers should be connected: %+v", list)
	}

	// A metadata-only update (empty apiKey) keeps the stored key.
	if err := a.SetProvider(store, agent.MemoryUser, "", "google", agent.CustomProvider{EnvVar: "GOOGLE_GENERATIVE_AI_API_KEY"}, "", nil); err != nil {
		t.Fatalf("SetProvider (env-only update): %v", err)
	}
	if store[agent.ProviderCredKey("google")] != "AIza-secret" {
		t.Fatalf("metadata-only update dropped the stored key")
	}

	// Delete clears the stored secret + env var (Disconnect).
	if err := a.DeleteProvider(store, agent.MemoryUser, "", "google"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if store[agent.ProviderCredKey("google")] != "" || store[agent.ProviderEnvKey("google")] != "" {
		t.Fatalf("Delete left env-only creds behind")
	}
	list, _ = a.ListProviders(store, agent.MemoryUser, "")
	if _, ok := list["google"]; ok {
		t.Fatalf("Delete should drop the env-only connection from the list")
	}
}

// TestProvidersScopeGate confirms opencode rejects any non-user scope.
func TestProvidersScopeGate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := adapter{}
	if _, err := a.ListProviders(mapStore{}, agent.MemoryProject, ""); err == nil {
		t.Fatalf("ListProviders at project scope: want error")
	}
	if err := a.SetProvider(mapStore{}, agent.MemoryProject, "", "x", agent.CustomProvider{BaseURL: "https://x/v1"}, "", nil); err == nil {
		t.Fatalf("SetProvider at project scope: want error")
	}
}

// TestProvidersMultiVarConnect verifies the catalog-driven multi-var connect path: a provider whose catalog
// entry declares SEVERAL env vars (e.g. AWS Bedrock) is connected via the `secrets` NAME→VALUE channel.
// Each value lands under its own namespaced cred key, NO opencode.json block is written, ListProviders
// reports all the stored var NAMES with HasKey, EnvForRun exports every var, a blank value leaves a stored
// one intact, and Delete clears them all.
func TestProvidersMultiVarConnect(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := providerConfigPath()

	store := mapStore{}
	a := adapter{}
	// Bedrock-style connect: three declared vars, no base URL, no model list — supplied via `secrets`.
	secrets := map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA-example",
		"AWS_SECRET_ACCESS_KEY": "shhh-secret",
		"AWS_REGION":            "us-east-1",
	}
	if err := a.SetProvider(store, agent.MemoryUser, "", "amazon-bedrock", agent.CustomProvider{Name: "AWS Bedrock"}, "", secrets); err != nil {
		t.Fatalf("SetProvider (multi-var): %v", err)
	}

	// No opencode.json block is written — a multi-var env-only connect must not touch the config file.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), "\"provider\"") {
			t.Fatalf("multi-var connect wrote a provider block:\n%s", raw)
		}
	}

	// Each value is stored under its own namespaced :env:<VAR> key (values, never a config file).
	for name, want := range secrets {
		if got := store[agent.ProviderEnvValueKey("amazon-bedrock", name)]; got != want {
			t.Fatalf("stored %s = %q, want %q", name, got, want)
		}
	}

	// ListProviders surfaces all three var NAMES (sorted), HasKey, no base URL / models.
	list, err := a.ListProviders(store, agent.MemoryUser, "")
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	got := list["amazon-bedrock"]
	if !got.HasKey || got.BaseURL != "" || len(got.Models) != 0 {
		t.Fatalf("ListProviders amazon-bedrock = %+v", got)
	}
	wantNames := []string{"AWS_ACCESS_KEY_ID", "AWS_REGION", "AWS_SECRET_ACCESS_KEY"}
	if strings.Join(got.EnvVars, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("EnvVars = %v, want %v (sorted)", got.EnvVars, wantNames)
	}
	if got.EnvVar != wantNames[0] {
		t.Fatalf("EnvVar (first) = %q, want %q", got.EnvVar, wantNames[0])
	}

	// EnvForRun exports every var (the sole seam creds enter a run).
	env := newAuth(store).EnvForRun()
	for name, want := range secrets {
		if env[name] != want {
			t.Fatalf("EnvForRun[%s] = %q, want %q", name, env[name], want)
		}
	}

	// A blank value leaves the stored one intact ("leave blank to keep"); a non-blank one rotates.
	upd := map[string]string{"AWS_ACCESS_KEY_ID": "", "AWS_REGION": "eu-west-1"}
	if err := a.SetProvider(store, agent.MemoryUser, "", "amazon-bedrock", agent.CustomProvider{}, "", upd); err != nil {
		t.Fatalf("SetProvider (partial update): %v", err)
	}
	if store[agent.ProviderEnvValueKey("amazon-bedrock", "AWS_ACCESS_KEY_ID")] != "AKIA-example" {
		t.Fatalf("blank field clobbered a stored value")
	}
	if store[agent.ProviderEnvValueKey("amazon-bedrock", "AWS_REGION")] != "eu-west-1" {
		t.Fatalf("non-blank field did not rotate the stored value")
	}

	// An invalid env-var name is rejected (the name is a cred-store key segment — a security boundary).
	if err := a.SetProvider(store, agent.MemoryUser, "", "amazon-bedrock", agent.CustomProvider{}, "", map[string]string{"BAD:NAME": "x"}); err == nil {
		t.Fatalf("SetProvider with a colon in the env-var name: want error")
	}

	// Delete clears every :env:<VAR> entry (Disconnect) and drops the connection from the list.
	if err := a.DeleteProvider(store, agent.MemoryUser, "", "amazon-bedrock"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	for name := range secrets {
		if store[agent.ProviderEnvValueKey("amazon-bedrock", name)] != "" {
			t.Fatalf("Delete left %s behind", name)
		}
	}
	if len(agent.StoredProviderEnv(store, "amazon-bedrock")) != 0 {
		t.Fatalf("StoredProviderEnv should be empty after delete")
	}
	list, _ = a.ListProviders(store, agent.MemoryUser, "")
	if _, ok := list["amazon-bedrock"]; ok {
		t.Fatalf("Delete should drop the multi-var connection from the list")
	}
}

// TestProviderEnvCanonicalAlias pins the alias repair. models.dev declares TWO env vars for google and
// the console stores the one the user filled; @ai-sdk/google reads only GOOGLE_GENERATIVE_AI_API_KEY, so
// a key stored under GOOGLE_API_KEY made opencode list Gemini models and then fail the turn with "API
// key is missing". EnvForRun must export the stored value under the canonical name TOO, without a
// re-connect and without dropping what was stored.
func TestProviderEnvCanonicalAlias(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store := mapStore{}
	a := adapter{}

	// Exactly what the console's catalog connect writes: the multi-var scheme, name straight from the feed.
	if err := a.SetProvider(store, agent.MemoryUser, "", "google", agent.CustomProvider{Name: "Google"}, "",
		map[string]string{"GOOGLE_API_KEY": "key-from-console"}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	// Storage stays verbatim — the catalog's name is what round-trips to GET /providers.
	if got := store[agent.ProviderEnvValueKey("google", "GOOGLE_API_KEY")]; got != "key-from-console" {
		t.Fatalf("stored under GOOGLE_API_KEY = %q, want the value verbatim", got)
	}

	env := newAuth(store).EnvForRun()
	if env["GOOGLE_GENERATIVE_AI_API_KEY"] != "key-from-console" {
		t.Errorf("canonical name not exported: GOOGLE_GENERATIVE_AI_API_KEY=%q", env["GOOGLE_GENERATIVE_AI_API_KEY"])
	}
	if env["GOOGLE_API_KEY"] != "key-from-console" {
		t.Errorf("the stored name must still be exported: GOOGLE_API_KEY=%q", env["GOOGLE_API_KEY"])
	}

	// A name the caller chose explicitly always wins — the repair is additive, never a rename.
	if err := a.SetProvider(store, agent.MemoryUser, "", "google", agent.CustomProvider{},
		"", map[string]string{"GOOGLE_GENERATIVE_AI_API_KEY": "explicit"}); err != nil {
		t.Fatalf("SetProvider explicit: %v", err)
	}
	if env := newAuth(store).EnvForRun(); env["GOOGLE_GENERATIVE_AI_API_KEY"] != "explicit" {
		t.Errorf("explicit value must win, got %q", env["GOOGLE_GENERATIVE_AI_API_KEY"])
	}

	// A multi-key brand has no single "the API key" to alias — nothing may be invented for it.
	bedrock := mapStore{}
	if err := a.SetProvider(bedrock, agent.MemoryUser, "", "amazon-bedrock", agent.CustomProvider{}, "",
		map[string]string{"AWS_ACCESS_KEY_ID": "akid", "AWS_SECRET_ACCESS_KEY": "secret"}); err != nil {
		t.Fatalf("SetProvider bedrock: %v", err)
	}
	benv := newAuth(bedrock).EnvForRun()
	if len(benv) != 2 || benv["AWS_ACCESS_KEY_ID"] != "akid" || benv["AWS_SECRET_ACCESS_KEY"] != "secret" {
		t.Errorf("multi-key brand env = %v, want exactly the two stored vars", benv)
	}
}

// TestProviderEnvOnlySingleKeyUsesCanonicalName covers the other env-only shape: no `secrets` map and no
// explicit override, where the name used to be derived as "<ID>_API_KEY" and google therefore broke the
// same way.
func TestProviderEnvOnlySingleKeyUsesCanonicalName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store := mapStore{}
	if err := (adapter{}).SetProvider(store, agent.MemoryUser, "", "google", agent.CustomProvider{}, "sk-single", nil); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if got := store[agent.ProviderEnvKey("google")]; got != "GOOGLE_GENERATIVE_AI_API_KEY" {
		t.Errorf("recorded env var = %q, want GOOGLE_GENERATIVE_AI_API_KEY", got)
	}
	if env := newAuth(store).EnvForRun(); env["GOOGLE_GENERATIVE_AI_API_KEY"] != "sk-single" {
		t.Errorf("EnvForRun = %v, want the key under the canonical name", env)
	}
}
