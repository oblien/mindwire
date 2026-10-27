package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

func TestNativeOAuthDiscoveryCodeAndScopedLogout(t *testing.T) {
	var callback, logout atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/provider/auth":
			w.Write([]byte(`{"provider-x":[{"type":"api","label":"API key"},{"type":"oauth","label":"Native subscription"}]}`))
		case "/provider/provider-x/oauth/authorize":
			var body struct{ Method int }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Method != 1 {
				t.Error("incorrect native method index")
			}
			w.Write([]byte(`{"url":"https://example.test/login","method":"code","instructions":"Paste the code"}`))
		case "/provider/provider-x/oauth/callback":
			var body struct {
				Method int
				Code   string
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Method != 1 || body.Code != "one-time-code" {
				t.Error("lost native login response")
			}
			callback.Store(true)
			w.Write([]byte(`true`))
		case "/auth/provider-x":
			if r.Method != http.MethodDelete {
				t.Error("logout is not native delete")
			}
			logout.Store(true)
			w.Write([]byte(`true`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	previous := openAuthServer
	openAuthServer = func(context.Context) (*nativeAuthTransport, error) {
		return &nativeAuthTransport{base: srv.URL, close: func() {}}, nil
	}
	defer func() { openAuthServer = previous }()
	store := mapStore{}
	m := newAuth(store)
	methods, err := m.DiscoverMethods(t.Context())
	if err != nil || len(methods) != 2 {
		t.Fatalf("native methods %+v %v", methods, err)
	}
	state, err := m.Begin(t.Context(), methods[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for state.Status == "pending" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		state, err = m.Step(t.Context(), map[string]string{agent.AuthFlowIDKey: state.FlowID})
	}
	if state.Status != "needs_input" {
		t.Fatalf("missing native code handoff: %+v %v", state, err)
	}
	_, err = m.Step(t.Context(), map[string]string{agent.AuthFlowIDKey: state.FlowID, agent.AuthCodeKey: "one-time-code"})
	if err != nil {
		t.Fatal(err)
	}
	<-m.loginDone
	if !callback.Load() || m.login.State().Status != "complete" || store.Get("oauthProvider") != "provider-x" {
		t.Fatal("native sign-in did not commit")
	}
	if err := m.Logout(t.Context()); err != nil || !logout.Load() {
		t.Fatalf("native scoped logout: %v", err)
	}
}

func TestMCPOverlayPreservesConfigAndUsesEnvironmentReference(t *testing.T) {
	env := map[string]string{"OPENCODE_CONFIG_CONTENT": `{"model":"provider/model","mcp":{"existing":{"type":"local","command":["server"]}}}`}
	err := applyMCPOverlay(env, json.RawMessage(`{"mcpServers":{"product":{"url":"http://127.0.0.1:1234/mcp","bearerTokenEnvVar":"PRODUCT_TOKEN"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Model string
		MCP   map[string]ocMCP
	}
	if err = json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &config); err != nil {
		t.Fatal(err)
	}
	if config.Model != "provider/model" || len(config.MCP) != 2 || config.MCP["product"].Headers["Authorization"] != "Bearer {env:PRODUCT_TOKEN}" {
		t.Fatalf("overlay lost configuration: %+v", config)
	}
}
