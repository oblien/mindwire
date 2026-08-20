package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// newMemoryTestMux wires the real supervisor (all registered adapters, via turn_gate_test's blank
// imports) with claude-code as the default agent and cwd as the daemon working directory.
func newMemoryTestMux(t *testing.T, cwd string) http.Handler {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	sup := orchestrator.New(store, stream.New(), notify.Fanout(nil), cwd, "claude-code")
	mux := http.NewServeMux()
	New(store, stream.New(), sup).Register(mux)
	return mux
}

func serve(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, r))
	return rec
}

// TestMemoryPromptsHTTP exercises the HTTP-specific glue for the memory/prompts routes: ?dir= and
// ?scope= query parsing, {name} path extraction, and the 400/404 status mapping. The file IO itself
// is covered by the adapter/memfile tests; this asserts the wiring the SDK path doesn't touch.
func TestMemoryPromptsHTTP(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	h := newMemoryTestMux(t, proj)
	dir := url.QueryEscape(proj)

	// ?dir= drives project-scope memory: write, then read it back.
	if rec := serve(t, h, "PUT", "/memory?dir="+dir, `{"scope":"project","content":"# hi"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT /memory: %d %s", rec.Code, rec.Body.String())
	}
	rec := serve(t, h, "GET", "/memory?dir="+dir, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /memory: %d %s", rec.Code, rec.Body.String())
	}
	var docs []agent.MemoryDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &docs); err != nil {
		t.Fatalf("decode memory docs: %v", err)
	}
	var found bool
	for _, d := range docs {
		if d.Scope == agent.MemoryProject {
			found = true
			if !d.Exists || d.Content != "# hi" {
				t.Fatalf("project memory = %+v", d)
			}
		}
	}
	if !found {
		t.Fatalf("no project doc in %+v", docs)
	}

	// ?scope= on a named template: write at project, read at project → 200; the default (user) scope
	// has no such template → 404. This proves both {name} extraction and the promptScope default.
	if rec := serve(t, h, "PUT", "/prompts/greet?scope=project&dir="+dir, `{"content":"Say hi"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT /prompts/greet: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, h, "GET", "/prompts/greet?scope=project&dir="+dir, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /prompts/greet?scope=project: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, h, "GET", "/prompts/greet?dir="+dir, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /prompts/greet default scope: %d, want 404", rec.Code)
	}

	// A missing template is 404.
	if rec := serve(t, h, "GET", "/prompts/nope?scope=project&dir="+dir, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing template: %d, want 404", rec.Code)
	}

	// An unknown agent is rejected at the resolve gate (400) before any module runs.
	if rec := serve(t, h, "GET", "/memory?agent=bogus", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /memory?agent=bogus: %d, want 400", rec.Code)
	}
}

// TestModelsHTTP exercises the /models route's capability gate. Claude and Codex both implement
// ModelsModule, so both answer 200 with a JSON array — empty is valid (Claude under no-auth; Codex
// always, since it has no native list and the daemon carries no catalog — the client sources OpenAI from
// the live catalog). An adapter that implements no module (the CLI-free fake) is rejected by the
// handler's type-assertion gate with 400.
func TestModelsHTTP(t *testing.T) {
	agent.Register(resolveFakeAdapter{id: "models-fake"})
	h := newMemoryTestMux(t, t.TempDir())

	rec := serve(t, h, "GET", "/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /models (claude): %d %s", rec.Code, rec.Body.String())
	}
	var models []agent.ModelInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("decode models: %v (body=%s)", err, rec.Body.String())
	}

	// Codex now implements ModelsModule (catalog-sourced) → 200 with a JSON array.
	rec = serve(t, h, "GET", "/models?agent=codex", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /models?agent=codex: %d %s, want 200", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("decode codex models: %v (body=%s)", err, rec.Body.String())
	}

	// An adapter that implements no ModelsModule → the type-assertion gate returns 400.
	if rec := serve(t, h, "GET", "/models?agent=models-fake", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /models?agent=models-fake: %d, want 400", rec.Code)
	}
}

// TestSubagentsHTTP exercises the /subagents route wiring: ?scope=/?dir= parsing, {name} extraction,
// the raw-content-canonical + parsed-meta roundtrip, 404 on a missing definition, and the capability
// gate (Claude implements the module; Codex does not → 400).
func TestSubagentsHTTP(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	h := newMemoryTestMux(t, proj)
	dir := url.QueryEscape(proj)

	body := `{"content":"---\nname: reviewer\ndescription: Reviews code\n---\nBe thorough."}`
	if rec := serve(t, h, "PUT", "/subagents/reviewer?scope=project&dir="+dir, body); rec.Code != http.StatusOK {
		t.Fatalf("PUT /subagents/reviewer: %d %s", rec.Code, rec.Body.String())
	}

	rec := serve(t, h, "GET", "/subagents/reviewer?scope=project&dir="+dir, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /subagents/reviewer: %d %s", rec.Code, rec.Body.String())
	}
	var sub agent.Subagent
	if err := json.Unmarshal(rec.Body.Bytes(), &sub); err != nil {
		t.Fatalf("decode subagent: %v", err)
	}
	if sub.Content == "" || sub.Meta == nil || sub.Meta.Description != "Reviews code" {
		t.Fatalf("subagent read = %+v, want raw content + parsed meta", sub)
	}

	// List omits content (parsed meta kept).
	rec = serve(t, h, "GET", "/subagents?dir="+dir, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /subagents: %d %s", rec.Code, rec.Body.String())
	}
	var list []agent.Subagent
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode subagent list: %v", err)
	}
	if len(list) != 1 || list[0].Content != "" {
		t.Fatalf("list = %+v, want 1 entry with content omitted", list)
	}

	// A missing definition is 404.
	if rec := serve(t, h, "GET", "/subagents/nope?scope=project&dir="+dir, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing subagent: %d, want 404", rec.Code)
	}

	// Codex implements no module → the type-assertion gate returns 400.
	if rec := serve(t, h, "GET", "/subagents?agent=codex", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /subagents?agent=codex: %d, want 400", rec.Code)
	}
}

// TestMCPHTTP exercises the /mcp route wiring: ?scope=/?dir= parsing, {name} extraction, the scope-keyed
// list shape, the PUT echo, DELETE idempotency, 404 on a missing server, and the MCP-specific 400 gates.
// Both registered agents implement MCPServerModule, so the no-module capability gate (identical to the
// /models and /subagents handlers) is unreachable here; the reachable 400 paths are an unsupported scope
// (Codex is user-only) and an unknown agent (the resolve gate).
func TestMCPHTTP(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	h := newMemoryTestMux(t, proj)
	dir := url.QueryEscape(proj)

	// PUT a stdio server at user scope → 200, and the handler echoes the stored server back.
	body := `{"command":"srv","args":["--x"],"env":{"K":"v"}}`
	rec := serve(t, h, "PUT", "/mcp/local?scope=user&dir="+dir, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /mcp/local: %d %s", rec.Code, rec.Body.String())
	}
	var echoed agent.MCPServer
	if err := json.Unmarshal(rec.Body.Bytes(), &echoed); err != nil {
		t.Fatalf("decode PUT echo: %v", err)
	}
	if echoed.Command != "srv" || len(echoed.Args) != 1 || echoed.Env["K"] != "v" {
		t.Fatalf("PUT echo = %+v", echoed)
	}

	// GET /mcp returns a scope→name→server map; the user scope now holds "local".
	rec = serve(t, h, "GET", "/mcp?dir="+dir, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /mcp: %d %s", rec.Code, rec.Body.String())
	}
	var byScope map[agent.MemoryScope]map[string]agent.MCPServer
	if err := json.Unmarshal(rec.Body.Bytes(), &byScope); err != nil {
		t.Fatalf("decode /mcp: %v", err)
	}
	if byScope[agent.MemoryUser]["local"].Command != "srv" {
		t.Fatalf("GET /mcp user scope = %+v, want local server", byScope[agent.MemoryUser])
	}

	// GET one server (default scope = user) → 200; a missing name → 404.
	if rec := serve(t, h, "GET", "/mcp/local?dir="+dir, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /mcp/local: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, h, "GET", "/mcp/nope?dir="+dir, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /mcp/nope: %d, want 404", rec.Code)
	}

	// DELETE removes it (200 {deleted:true}); deleting again is idempotent (still 200).
	if rec := serve(t, h, "DELETE", "/mcp/local?scope=user&dir="+dir, ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /mcp/local: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, h, "DELETE", "/mcp/local?scope=user&dir="+dir, ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /mcp/local (idempotent): %d", rec.Code)
	}
	if rec := serve(t, h, "GET", "/mcp/local?dir="+dir, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after delete: %d, want 404", rec.Code)
	}

	// Codex is user-scope only → a project-scope GET is a 400 (unsupported scope), surfaced by the module.
	if rec := serve(t, h, "GET", "/mcp/x?agent=codex&scope=project&dir="+dir, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /mcp/x?agent=codex&scope=project: %d, want 400", rec.Code)
	}

	// An unknown agent is rejected at the resolve gate (400) before any module runs.
	if rec := serve(t, h, "GET", "/mcp?agent=bogus", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /mcp?agent=bogus: %d, want 400", rec.Code)
	}
}

// TestProvidersHTTP exercises the /providers route wiring against opencode (which materializes into
// opencode.json): the PUT echo (HasKey + derived EnvVar, key never returned), the scope-keyed list, GET
// one, 404 on a missing provider, DELETE idempotency, a metadata-only update preserving the key, and the
// capability gates — Codex also supports the module (user-scope 200) while Claude does not (400), plus
// the unsupported-scope and unknown-agent 400s.
func TestProvidersHTTP(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // opencode.json lives under $XDG_CONFIG_HOME/opencode
	t.Setenv("CODEX_HOME", t.TempDir())      // codex config.toml lives under $CODEX_HOME
	h := newMemoryTestMux(t, t.TempDir())

	// PUT a provider with a write-only key at user scope → 200; the echo carries HasKey + the derived
	// EnvVar but never the key.
	body := `{"name":"My LLM","baseUrl":"https://llm.example/v1","models":["m-large","m-small"],"apiKey":"sk-secret-123"}`
	rec := serve(t, h, "PUT", "/providers/my-llm?agent=opencode&scope=user", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /providers/my-llm?agent=opencode: %d %s", rec.Code, rec.Body.String())
	}
	var echoed agent.CustomProvider
	if err := json.Unmarshal(rec.Body.Bytes(), &echoed); err != nil {
		t.Fatalf("decode PUT echo: %v", err)
	}
	if echoed.ID != "my-llm" || !echoed.HasKey || echoed.EnvVar != "MY_LLM_API_KEY" || echoed.BaseURL != "https://llm.example/v1" {
		t.Fatalf("PUT echo = %+v, want id+hasKey+derived env var", echoed)
	}
	if strings.Contains(rec.Body.String(), "sk-secret-123") {
		t.Fatalf("PUT echo leaked the api key: %s", rec.Body.String())
	}

	// GET /providers returns a scope→id→provider map; the user scope now holds "my-llm".
	rec = serve(t, h, "GET", "/providers?agent=opencode", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /providers?agent=opencode: %d %s", rec.Code, rec.Body.String())
	}
	var byScope map[agent.MemoryScope]map[string]agent.CustomProvider
	if err := json.Unmarshal(rec.Body.Bytes(), &byScope); err != nil {
		t.Fatalf("decode /providers: %v", err)
	}
	if got := byScope[agent.MemoryUser]["my-llm"]; got.BaseURL != "https://llm.example/v1" || len(got.Models) != 2 || !got.HasKey {
		t.Fatalf("GET /providers user scope = %+v, want the registered provider", got)
	}

	// GET one (default scope = user) → 200; a missing id → 404.
	if rec := serve(t, h, "GET", "/providers/my-llm?agent=opencode", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /providers/my-llm: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, h, "GET", "/providers/nope?agent=opencode", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /providers/nope: %d, want 404", rec.Code)
	}

	// A metadata-only update (no apiKey) preserves the stored key.
	upd := `{"baseUrl":"https://llm.example/v2","models":["m-large"]}`
	if rec := serve(t, h, "PUT", "/providers/my-llm?agent=opencode&scope=user", upd); rec.Code != http.StatusOK {
		t.Fatalf("PUT (update) /providers/my-llm: %d %s", rec.Code, rec.Body.String())
	}
	rec = serve(t, h, "GET", "/providers/my-llm?agent=opencode", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &echoed); err != nil {
		t.Fatalf("decode update GET: %v", err)
	}
	if !echoed.HasKey || echoed.BaseURL != "https://llm.example/v2" {
		t.Fatalf("metadata-only update lost the key or URL: %+v", echoed)
	}

	// DELETE removes it (200 {deleted:true}); deleting again is idempotent.
	if rec := serve(t, h, "DELETE", "/providers/my-llm?agent=opencode&scope=user", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /providers/my-llm: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, h, "DELETE", "/providers/my-llm?agent=opencode&scope=user", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /providers/my-llm (idempotent): %d", rec.Code)
	}
	if rec := serve(t, h, "GET", "/providers/my-llm?agent=opencode", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after delete: %d, want 404", rec.Code)
	}

	// Multi-var connect via the `secrets` channel (opencode, no base URL): a Bedrock-style provider with
	// three declared env vars. The echo carries the stored NAMES (sorted) + HasKey and never a value.
	multi := `{"name":"AWS Bedrock","baseUrl":"","models":[],"secrets":{"AWS_ACCESS_KEY_ID":"AKIA-x","AWS_SECRET_ACCESS_KEY":"shhh","AWS_REGION":"us-east-1"}}`
	rec = serve(t, h, "PUT", "/providers/amazon-bedrock?agent=opencode&scope=user", multi)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT (multi-var) /providers/amazon-bedrock: %d %s", rec.Code, rec.Body.String())
	}
	if b := rec.Body.String(); strings.Contains(b, "AKIA-x") || strings.Contains(b, "shhh") {
		t.Fatalf("PUT (multi-var) echo leaked a secret value: %s", b)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &echoed); err != nil {
		t.Fatalf("decode multi-var PUT echo: %v", err)
	}
	if !echoed.HasKey || strings.Join(echoed.EnvVars, ",") != "AWS_ACCESS_KEY_ID,AWS_REGION,AWS_SECRET_ACCESS_KEY" {
		t.Fatalf("multi-var echo = %+v, want hasKey + 3 sorted env var names", echoed)
	}
	if rec := serve(t, h, "DELETE", "/providers/amazon-bedrock?agent=opencode&scope=user", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE (multi-var) /providers/amazon-bedrock: %d", rec.Code)
	}

	// Codex also implements the module (user-scope) → PUT is a 200.
	if rec := serve(t, h, "PUT", "/providers/cx?agent=codex&scope=user", `{"baseUrl":"https://cx.example/v1","models":["c1"]}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT /providers/cx?agent=codex: %d %s", rec.Code, rec.Body.String())
	}

	// opencode is user-scope only → a project-scope single GET is a 400 (unsupported scope), surfaced by
	// the module. (The list route ignores ?scope= — it iterates the module's own ProviderScopes.)
	if rec := serve(t, h, "GET", "/providers/x?agent=opencode&scope=project", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /providers/x?agent=opencode&scope=project: %d, want 400", rec.Code)
	}

	// Claude implements no custom-provider module (it uses the gateway auth lane) → the type-assertion
	// gate returns 400. claude-code is the default agent, so an agent-less request hits it.
	if rec := serve(t, h, "GET", "/providers", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /providers (claude default): %d, want 400", rec.Code)
	}

	// An unknown agent is rejected at the resolve gate (400) before any module runs.
	if rec := serve(t, h, "GET", "/providers?agent=bogus", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /providers?agent=bogus: %d, want 400", rec.Code)
	}
}
