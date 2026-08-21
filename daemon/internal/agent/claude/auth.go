package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
)

// authModule implements agent.AuthModule for Claude Code. Two options:
//   - "apiKey": a field flow → ANTHROPIC_API_KEY (pay-per-token)
//   - "login":  an interactive flow → runs `claude setup-token`, surfaces the OAuth
//     URL, and captures the long-lived CLAUDE_CODE_OAUTH_TOKEN (subscription).
//
// Bound to a CredStore at construction so it persists creds and holds in-flight login state.
type authModule struct {
	store agent.CredStore
	mu    sync.Mutex
	login *loginFlow
}

func newAuth(store agent.CredStore) *authModule { return &authModule{store: store} }

func (m *authModule) Methods() []agent.AuthMethod {
	methods := []agent.AuthMethod{
		{
			ID: "apiKey", Label: "API key", Scope: agent.ScopeUnified,
			Help: "Anthropic API key from console.anthropic.com (pay-per-token).",
			Fields: []agent.Field{{
				Key: "apiKey", Label: "Anthropic API key", Type: agent.FieldSecret,
				Required: true, Placeholder: "sk-ant-…",
			}},
		},
		{
			ID: "login", Label: "Sign in with Claude", Scope: agent.ScopeUnified, Interactive: true,
			Help: "Use your Claude Pro/Max subscription (runs claude setup-token).",
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
				Fields: p.method().Fields, Message: "Enter your " + p.label + " details.",
			}, nil
		}
		return agent.AuthState{}, errors.New("unknown auth method: " + methodID)
	}
}

func (m *authModule) Step(ctx context.Context, input map[string]string) (agent.AuthState, error) {
	if key := strings.TrimSpace(input["apiKey"]); key != "" {
		if err := m.store.Set("apiKey", key); err != nil {
			return agent.AuthState{}, err
		}
		_ = m.store.Set("authMethod", "apiKey")
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
		for _, cf := range p.fields {
			// Trim; empty clears any prior value (so a re-setup can drop a field, e.g. switch from
			// static keys to an AWS profile).
			if err := m.store.Set(cf.Key, strings.TrimSpace(input[cf.Key])); err != nil {
				return agent.AuthState{}, err
			}
		}
		_ = m.store.Set("authMethod", p.id)
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
	return lf.state(), nil
}

// Status reports whether a turn would actually authenticate. It does NOT guess from where
// credentials happen to live — it asks the CLI its own verdict via `claude auth status --json`.
// That single command is the source of truth: it runs the CLI's full credential resolution
// (settings.json env, keychain, apiKeyHelper, OAuth refresh, third-party providers like
// Bedrock/Vertex) and reports the result. It's free and fast (~0.25s), so there's no reason to
// sniff config or spend a real request.
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
	cmd := exec.CommandContext(ctx, "bash", "-lc", "claude "+strings.Join(authStatusArgs(), " "))
	proc.Group(cmd) // timeout kills the whole `claude` tree (it forks node), not just bash
	cmd.Env = os.Environ()
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
// never travel through TurnInput.Config or the shell command string. baseUrl is orthogonal to the
// credential (a custom gateway/proxy endpoint) and is exported whenever set; the credential itself
// follows a strict precedence so a subscription login wins over a gateway token wins over an API key.
func (m *authModule) EnvForRun() map[string]string {
	env := map[string]string{}
	// Custom-scope cloud providers own the whole env: when one is the selected method, route the CLI
	// to that backend (enable flag) and export its declared vars — nothing else. First-party endpoint
	// / credential vars do not apply (the provider has its own endpoint + credential chain).
	if p, ok := cloudProviderByID(m.store.Get("authMethod")); ok {
		env[p.enableEnv] = "1"
		for _, cf := range p.fields {
			if v := strings.TrimSpace(m.store.Get(cf.Key)); v != "" {
				env[cf.env] = v
			}
		}
		return env
	}
	// Custom endpoint applies regardless of which first-party credential authenticates.
	if u := strings.TrimSpace(m.store.Get("baseUrl")); u != "" {
		env["ANTHROPIC_BASE_URL"] = u
	}
	switch {
	case m.store.Get("oauthToken") != "":
		env["CLAUDE_CODE_OAUTH_TOKEN"] = m.store.Get("oauthToken")
	case m.store.Get("bearerToken") != "":
		env["ANTHROPIC_AUTH_TOKEN"] = m.store.Get("bearerToken")
	case m.store.Get("apiKey") != "":
		env["ANTHROPIC_API_KEY"] = m.store.Get("apiKey")
	}
	return env
}

// ---- interactive login (claude setup-token) -------------------------------

type loginFlow struct {
	mu      sync.Mutex
	url     string
	status  string // "pending" | "complete" | "error"
	message string
}

var (
	urlRe   = regexp.MustCompile(`https?://[^\s'"]+`)
	tokenRe = regexp.MustCompile(`sk-ant-oat[0-9A-Za-z._-]+`)
)

func (m *authModule) beginLogin(ctx context.Context) agent.AuthState {
	m.mu.Lock()
	if m.login != nil && m.login.state().Status == "pending" {
		st := m.login.state()
		m.mu.Unlock()
		return st
	}
	lf := &loginFlow{status: "pending", message: "Starting sign-in…"}
	m.login = lf
	m.mu.Unlock()

	runCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	go func() { defer cancel(); m.runLogin(runCtx, lf) }()

	// Give the command a moment to print its OAuth URL.
	time.Sleep(800 * time.Millisecond)
	return lf.state()
}

func (m *authModule) runLogin(ctx context.Context, lf *loginFlow) {
	pr, pw, err := os.Pipe()
	if err != nil {
		lf.set("error", "", "couldn't start sign-in: "+err.Error())
		return
	}
	defer func() { _ = pr.Close() }() // release the read end (pw is closed by the wait goroutine)
	cmd := exec.CommandContext(ctx, "bash", "-lc", "claude setup-token")
	proc.Group(cmd) // a cancelled/abandoned sign-in kills the whole `claude` tree, not just bash
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		lf.set("error", "", "couldn't start sign-in: "+err.Error())
		return
	}
	go func() { _ = cmd.Wait(); _ = pw.Close() }()

	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if tok := tokenRe.FindString(line); tok != "" {
			_ = m.store.Set("oauthToken", tok)
			_ = m.store.Set("authMethod", "login")
			ensureModels(m.EnvForRun())
			lf.set("complete", lf.getURL(), "Signed in.")
			return
		}
		if u := urlRe.FindString(line); u != "" && lf.getURL() == "" {
			lf.set("pending", u, "Open this URL to authorize, then it completes automatically.")
		}
	}
	if lf.state().Status != "complete" {
		lf.set("error", lf.getURL(), "Sign-in did not complete (a token was not produced).")
	}
}

func (lf *loginFlow) set(status, url, msg string) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	lf.status = status
	if url != "" {
		lf.url = url
	}
	lf.message = msg
}

func (lf *loginFlow) getURL() string {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.url
}

func (lf *loginFlow) state() agent.AuthState {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return agent.AuthState{Method: "login", Status: lf.status, URL: lf.url, Message: lf.message}
}
