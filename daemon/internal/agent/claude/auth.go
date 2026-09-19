package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

// Claude owns native subscription credentials and refresh. The daemon stores
// only the selected method and credentials entered for field-based connections.
type authModule struct {
	store     agent.CredStore
	mu        sync.Mutex
	login     *agent.AuthFlow
	loginDone <-chan struct{}
}

func newAuth(store agent.CredStore) *authModule { return &authModule{store: store} }

func (m *authModule) Methods() []agent.AuthMethod {
	methods := []agent.AuthMethod{
		{
			ID: "login", Label: "Continue with Claude", Scope: agent.ScopeUnified, Interactive: true,
			Help: "Use the Claude Code access included with your Claude subscription. Sign in in your browser, then paste the authorization code here if prompted.",
		},
		{
			ID: "apiKey", Label: "API key", Scope: agent.ScopeUnified,
			Help: "Anthropic API key from console.anthropic.com (pay-per-token).",
			Fields: []agent.Field{{
				Key: "apiKey", Label: "Anthropic API key", Type: agent.FieldSecret,
				Required: true, Placeholder: "sk-ant-…",
			}},
		},
		{
			ID: "bearerToken", Label: "Bearer token (gateway)", Scope: agent.ScopeUnified,
			Help: "A bearer token for a custom Anthropic-compatible gateway or proxy — sent as ANTHROPIC_AUTH_TOKEN (Authorization: Bearer). Set the gateway's base URL alongside it.",
			Fields: []agent.Field{
				{
					Key: "bearerToken", Label: "Bearer token", Type: agent.FieldSecret,
					Required: true, Placeholder: "…",
					Help: "Sent as the ANTHROPIC_AUTH_TOKEN bearer credential.",
				},
				{
					Key: "baseUrl", Label: "Base URL", Type: agent.FieldText,
					Placeholder: "https://gateway.internal/anthropic",
					Help:        "Custom endpoint (ANTHROPIC_BASE_URL). Leave blank to use the default Anthropic API.",
				},
			},
		},
	}
	// Custom-scope lane: Claude's third-party cloud backends (Bedrock/Vertex/Foundry). Declared in
	// cloud.go; appended after the unified methods so the options list reads unified-first.
	for _, p := range cloudProviders() {
		methods = append(methods, p.method())
	}
	return methods
}

func (m *authModule) Begin(ctx context.Context, methodID string) (agent.AuthState, error) {
	switch methodID {
	case "apiKey":
		return agent.AuthState{
			Method: "apiKey", Status: "needs_input",
			Fields: agent.MethodFields(m, "apiKey"), Message: "Enter your Anthropic API key.",
		}, nil
	case "bearerToken":
		return agent.AuthState{
			Method: "bearerToken", Status: "needs_input",
			Fields: agent.MethodFields(m, "bearerToken"), Message: "Enter your gateway bearer token (and its base URL).",
		}, nil
	case "login":
		return m.beginLogin(ctx), nil
	default:
		// Custom-scope cloud providers: a field flow keyed by the provider id.
		if p, ok := cloudProviderByID(methodID); ok {
			return agent.AuthState{
				Method: p.id, Status: "needs_input",
				Fields: p.method().Fields, Sections: p.sections,
			}, nil
		}
		return agent.AuthState{}, errors.New("unknown auth method: " + methodID)
	}
}

func (m *authModule) Step(ctx context.Context, input map[string]string) (agent.AuthState, error) {
	if len(input) == 0 || input[agent.AuthFlowIDKey] != "" || input[agent.AuthActionKey] != "" || input[agent.AuthCodeKey] != "" {
		m.mu.Lock()
		flow := m.login
		m.mu.Unlock()
		if flow != nil {
			return flow.Step(input)
		}
		return agent.AuthState{Method: "login", Status: "error", Message: "No sign-in in progress. Start again."}, nil
	}
	m.cancelLogin()
	if key := strings.TrimSpace(input["apiKey"]); key != "" {
		if err := m.store.Set("apiKey", key); err != nil {
			return agent.AuthState{}, err
		}
		if err := m.store.Set("baseUrl", ""); err != nil {
			return agent.AuthState{}, err
		}
		if err := m.store.Set("authMethod", "apiKey"); err != nil {
			return agent.AuthState{}, err
		}
		go ensureModels(m.EnvForRun()) // pull the real model list — async so /auth/step returns at once
		return agent.AuthState{Method: "apiKey", Status: "complete"}, nil
	}
	if tok := strings.TrimSpace(input["bearerToken"]); tok != "" {
		if err := m.store.Set("bearerToken", tok); err != nil {
			return agent.AuthState{}, err
		}
		// baseUrl is optional and non-secret; store the trimmed value (empty clears any prior URL).
		if err := m.store.Set("baseUrl", strings.TrimSpace(input["baseUrl"])); err != nil {
			return agent.AuthState{}, err
		}
		_ = m.store.Set("authMethod", "bearerToken")
		go ensureModels(m.EnvForRun())
		return agent.AuthState{Method: "bearerToken", Status: "complete"}, nil
	}
	// Custom-scope cloud providers: the input carries a provider's declared field keys (disjoint per
	// provider, so the match is unambiguous). Persist each provided value under its cred-store key —
	// secrets included — and record the provider as the active auth method; EnvForRun exports them.
	if p, ok := cloudProviderForInput(input); ok {
		if p.id == "foundry" {
			validated, err := validateFoundryInput(input)
			if err != nil {
				return agent.AuthState{Method: p.id, Status: "error", Message: err.Error(), Fields: p.method().Fields, Sections: p.sections}, nil
			}
			input = validated
		}
		for _, cf := range p.fields {
			// Trim; empty clears any prior value (so a re-setup can drop a field, e.g. switch from
			// static keys to an AWS profile).
			if err := m.store.Set(cf.Key, strings.TrimSpace(input[cf.Key])); err != nil {
				return agent.AuthState{}, err
			}
		}
		if err := m.store.Set("authMethod", p.id); err != nil {
			return agent.AuthState{}, err
		}
		go ensureModels(m.EnvForRun())
		return agent.AuthState{Method: p.id, Status: "complete"}, nil
	}
	// Otherwise poll the in-flight login flow.
	m.mu.Lock()
	lf := m.login
	m.mu.Unlock()
	if lf == nil {
		return agent.AuthState{Method: "login", Status: "error", Message: "no login in progress"}, nil
	}
	return lf.Step(input)
}

// Status reports whether a turn would actually authenticate. It does NOT guess from where
// credentials happen to live — it asks the CLI its own verdict via `claude auth status --json`.
// That single command is the source of truth: it runs the CLI's full credential resolution
// (settings.json env, keychain, apiKeyHelper, OAuth refresh, third-party providers like
// Bedrock/Vertex) and reports the result. It's free and fast (~0.25s), so there's no reason to
// sniff config or spend a real request.
func (m *authModule) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.login != nil && m.login.Active()
}

func (m *authModule) Status(ctx context.Context) agent.AuthStatus {
	st, err := queryAuthStatus(ctx, m.EnvForRun())
	if err != nil {
		return agent.AuthStatus{Configured: false, Detail: "Couldn't check auth (" + err.Error() + ")."}
	}
	if !st.LoggedIn {
		return agent.AuthStatus{Configured: false, Detail: "Not signed in — run login."}
	}
	go ensureModels(m.EnvForRun())
	return agent.AuthStatus{Configured: true, Method: st.methodLabel(), Detail: st.detail()}
}

// ---- live auth verification via the CLI's own answer ------------------------------
//
// `claude auth status --json` prints e.g.:
//
//	{"loggedIn":true,"authMethod":"api_key","apiProvider":"firstParty","apiKeySource":"ANTHROPIC_API_KEY"}
//
// loggedIn is authoritative. It exits 0 and honors the CLI's complete credential resolution, so we
// never re-derive auth ourselves.

const authStatusTimeout = 15 * time.Second // generous: a cold `node` start can be slow

// authStatus mirrors the `claude auth status --json` payload.
type authStatus struct {
	LoggedIn     bool   `json:"loggedIn"`
	AuthMethod   string `json:"authMethod"`   // e.g. "api_key", "oauth"
	APIProvider  string `json:"apiProvider"`  // e.g. "firstParty", "bedrock", "vertex"
	APIKeySource string `json:"apiKeySource"` // e.g. "ANTHROPIC_API_KEY", "apiKeyHelper"
}

// methodLabel is the cosmetic "apiKey" vs "login" tag, taken from the CLI's reported authMethod.
func (s authStatus) methodLabel() string {
	switch strings.ToLower(s.AuthMethod) {
	case "api_key", "apikey":
		return "apiKey"
	case "oauth", "claude_ai", "claudeai", "subscription", "token":
		return "login"
	default:
		return s.AuthMethod
	}
}

// detail is the human line shown next to a verified login, naming a non-default provider if any.
func (s authStatus) detail() string {
	if s.APIProvider != "" && !strings.EqualFold(s.APIProvider, "firstParty") {
		return "Signed in via " + s.APIProvider
	}
	return "Signed in"
}

// authStatusArgs are the flags for the verdict command. This IS the CLI answering about itself.
func authStatusArgs() []string {
	return []string{"auth", "status", "--json"}
}

// queryAuthStatus runs `claude auth status --json` and parses its verdict. It goes through a
// LOGIN shell (`bash -lc`) — exactly like a human typing `claude auth status` in the sandbox
// terminal — so PATH and any profile-exported env (ANTHROPIC_*, apiKeyHelper, etc.) resolve
// identically. The daemon is launched detached with a stripped env, so a bare exec would miss all
// of that and could report "not signed in" while the terminal is clearly logged in. Daemon-stored
// creds are layered on top so an app-provided key also applies.
func queryAuthStatus(ctx context.Context, extraEnv map[string]string) (authStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, authStatusTimeout)
	defer cancel()
	command := subscriptionCommand("claude "+strings.Join(authStatusArgs(), " "), extraEnv)
	cmd := exec.CommandContext(ctx, "bash", "-lc", toolchain.Shell(command))
	proc.Group(cmd) // timeout kills the whole `claude` tree (it forks node), not just bash
	cmd.Env = toolchain.Environment()
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return authStatus{}, errors.New("auth check timed out")
		}
		// A non-zero exit with parseable JSON still carries the verdict (e.g. loggedIn:false).
		if st, perr := parseAuthStatus(stdout.Bytes()); perr == nil {
			return st, nil
		}
		return authStatus{}, errors.New("auth check failed")
	}
	return parseAuthStatus(stdout.Bytes())
}

// parseAuthStatus decodes the `--json` payload.
func parseAuthStatus(out []byte) (authStatus, error) {
	var st authStatus
	if err := json.Unmarshal(bytes.TrimSpace(out), &st); err != nil {
		return authStatus{}, errors.New("unrecognized auth status output")
	}
	return st, nil
}

// EnvForRun builds the process env for a turn. It is the ONLY place credentials enter a run — they
// never travel through TurnInput.Config or the shell command string. An explicit
// method takes precedence over saved credentials from earlier connections.
func (m *authModule) EnvForRun() map[string]string {
	env := map[string]string{}
	// Custom-scope cloud providers own the whole env: when one is the selected method, route the CLI
	// to that backend (enable flag) and export its declared vars — nothing else. First-party endpoint
	// / credential vars do not apply (the provider has its own endpoint + credential chain).
	if p, ok := cloudProviderByID(m.store.Get("authMethod")); ok {
		env[p.enableEnv] = "1"
		for _, cf := range p.fields {
			if cf.env == "" { // form selectors do not become process environment variables
				continue
			}
			if v := strings.TrimSpace(m.store.Get(cf.Key)); v != "" {
				env[cf.env] = v
			}
		}
		if p.id == "foundry" && env["ANTHROPIC_MODEL"] != "" {
			// A three-field setup has one deployed model. Pin aliases and the
			// background model to it so Claude never guesses undeployed Azure IDs.
			for _, name := range []string{"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL"} {
				env[name] = env["ANTHROPIC_MODEL"]
			}
		}
		return env
	}
	method := m.store.Get("authMethod")
	if method == "login" {
		env[subscriptionMarker] = "native"
		// Preserve subscriptions connected with earlier daemon setup-token flows.
		if token := m.store.Get("oauthToken"); token != "" {
			env[subscriptionMarker] = "token"
			env["CLAUDE_CODE_OAUTH_TOKEN"] = token
		}
		return env
	}
	if u := strings.TrimSpace(m.store.Get("baseUrl")); u != "" {
		env["ANTHROPIC_BASE_URL"] = u
	}
	switch {
	case method == "apiKey":
		env["ANTHROPIC_API_KEY"] = m.store.Get("apiKey")
	case method == "bearerToken":
		env["ANTHROPIC_AUTH_TOKEN"] = m.store.Get("bearerToken")
	case m.store.Get("oauthToken") != "": // compatibility for stores without a method tag
		env["CLAUDE_CODE_OAUTH_TOKEN"] = m.store.Get("oauthToken")
	case m.store.Get("bearerToken") != "":
		env["ANTHROPIC_AUTH_TOKEN"] = m.store.Get("bearerToken")
	case m.store.Get("apiKey") != "":
		env["ANTHROPIC_API_KEY"] = m.store.Get("apiKey")
	}
	return env
}
