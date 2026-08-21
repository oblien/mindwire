package codex

import (
	"os"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestProvidersRoundTrip verifies the config.toml materialization end-to-end: a Set writes the
// `[model_providers.<id>]` table with `env_key = "VAR"` (never the literal secret) while preserving an
// unrelated existing table byte-for-byte; List reads it back (models + HasKey from the store); EnvForRun
// exports the key; and Delete removes only that table and clears the stored secret/models.
func TestProvidersRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := mcpConfigPath()

	// Seed config.toml with an unrelated MCP-server table the writer must preserve.
	seed := "[mcp_servers.local]\ncommand = \"srv\"\nargs = [\"--x\"]\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed config.toml: %v", err)
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

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "sk-secret-123") {
		t.Fatalf("config.toml leaked the api key:\n%s", content)
	}
	// The provider table + env-var reference are present; the pre-existing MCP table is preserved.
	for _, want := range []string{
		"[model_providers.my-llm]",
		`base_url = "https://llm.example/v1"`,
		`env_key = "MY_LLM_API_KEY"`,
		`wire_api = "chat"`,
		"[mcp_servers.local]",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("config.toml missing %q:\n%s", want, content)
		}
	}

	// The store holds the secret, env-var name, and models (config.toml has no model-list field).
	if store[agent.ProviderCredKey("my-llm")] != "sk-secret-123" {
		t.Fatalf("stored key = %q", store[agent.ProviderCredKey("my-llm")])
	}
	if store[agent.ProviderEnvKey("my-llm")] != "MY_LLM_API_KEY" {
		t.Fatalf("stored env var = %q", store[agent.ProviderEnvKey("my-llm")])
	}

	// List reflects models (from store) + HasKey + the parsed base URL/env key.
	list, err := a.ListProviders(store, agent.MemoryUser, "")
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	got := list["my-llm"]
	if !got.HasKey || got.EnvVar != "MY_LLM_API_KEY" || got.BaseURL != "https://llm.example/v1" || len(got.Models) != 2 {
		t.Fatalf("ListProviders my-llm = %+v", got)
	}

	// EnvForRun exports the stored key under its env var.
	env := newAuth(store).EnvForRun()
	if env["MY_LLM_API_KEY"] != "sk-secret-123" {
		t.Fatalf("EnvForRun = %v, want MY_LLM_API_KEY=sk-secret-123", env)
	}

	// A metadata-only update (empty apiKey) keeps the stored key.
	if err := a.SetProvider(store, agent.MemoryUser, "", "my-llm", agent.CustomProvider{BaseURL: "https://llm.example/v2"}, "", nil); err != nil {
		t.Fatalf("SetProvider (update): %v", err)
	}
	if store[agent.ProviderCredKey("my-llm")] != "sk-secret-123" {
		t.Fatalf("metadata-only update dropped the stored key")
	}
	// The update replaced the table's base_url and did not duplicate the section.
	raw, _ = os.ReadFile(path)
	content = string(raw)
	if strings.Count(content, "[model_providers.my-llm]") != 1 {
		t.Fatalf("update duplicated the provider table:\n%s", content)
	}
	if !strings.Contains(content, `base_url = "https://llm.example/v2"`) {
		t.Fatalf("update did not rewrite base_url:\n%s", content)
	}

	// Delete removes the table, preserves the MCP table, and clears the stored secret/models.
	if err := a.DeleteProvider(store, agent.MemoryUser, "", "my-llm"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	raw, _ = os.ReadFile(path)
	content = string(raw)
	if strings.Contains(content, "[model_providers.my-llm]") {
		t.Fatalf("Delete left the provider table behind:\n%s", content)
	}
	if !strings.Contains(content, "[mcp_servers.local]") {
		t.Fatalf("Delete dropped the unrelated MCP table:\n%s", content)
	}
	if store[agent.ProviderCredKey("my-llm")] != "" || store[pkModels("my-llm")] != "" {
		t.Fatalf("Delete left store keys behind: key=%q models=%q", store[agent.ProviderCredKey("my-llm")], store[pkModels("my-llm")])
	}

	// Deleting again is idempotent.
	if err := a.DeleteProvider(store, agent.MemoryUser, "", "my-llm"); err != nil {
		t.Fatalf("DeleteProvider (idempotent): %v", err)
	}
}

// TestProvidersScopeGate confirms codex rejects any non-user scope.
func TestProvidersScopeGate(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	a := adapter{}
	if _, err := a.ListProviders(mapStore{}, agent.MemoryProject, ""); err == nil {
		t.Fatalf("ListProviders at project scope: want error")
	}
	if err := a.SetProvider(mapStore{}, agent.MemoryProject, "", "x", agent.CustomProvider{BaseURL: "https://x/v1"}, "", nil); err == nil {
		t.Fatalf("SetProvider at project scope: want error")
	}
}

// TestProvidersSharedEnvOnlyLifecycle pins the cross-agent half of the provider cycle. Provider creds
// live in one shared namespace that authModule.EnvForRun merges into every Codex run, so a provider
// connected from another agent's page IS live under Codex. ListProviders used to report only the ones
// with a config.toml table, which made those credentials invisible — and therefore un-editable and
// un-deletable — from any Codex-scoped surface. Connect, report, and disconnect must all work here.
func TestProvidersSharedEnvOnlyLifecycle(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	store := mapStore{}
	a := adapter{}

	// A credential-only connect: no base URL, one declared env var.
	if err := a.SetProvider(store, agent.MemoryUser, "", "google", agent.CustomProvider{Name: "Google"}, "",
		map[string]string{"GOOGLE_GENERATIVE_AI_API_KEY": "key-1"}); err != nil {
		t.Fatalf("SetProvider (env-only): %v", err)
	}

	list, err := a.ListProviders(store, agent.MemoryUser, "")
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	got, ok := list["google"]
	if !ok {
		t.Fatalf("env-only provider missing from ListProviders: %+v", list)
	}
	if !got.HasKey || got.BaseURL != "" || len(got.EnvVars) != 1 || got.EnvVars[0] != "GOOGLE_GENERATIVE_AI_API_KEY" {
		t.Errorf("reported provider = %+v, want hasKey, no baseUrl, the one env var NAME", got)
	}

	// It reaches a run, under the name it was stored as — and only the name, never a config file.
	if env := newAuth(store).EnvForRun(); env["GOOGLE_GENERATIVE_AI_API_KEY"] != "key-1" {
		t.Errorf("EnvForRun = %v, want the shared credential exported", env)
	}

	// Disconnect must clear the multi-var subtree, or the key stays live in every run while the console
	// reports it as gone.
	if err := a.DeleteProvider(store, agent.MemoryUser, "", "google"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if list, err = a.ListProviders(store, agent.MemoryUser, ""); err != nil {
		t.Fatalf("ListProviders after delete: %v", err)
	}
	if _, still := list["google"]; still {
		t.Errorf("provider still reported after disconnect: %+v", list)
	}
	if env := newAuth(store).EnvForRun(); env["GOOGLE_GENERATIVE_AI_API_KEY"] != "" {
		t.Errorf("credential still exported after disconnect: %v", env)
	}
}
