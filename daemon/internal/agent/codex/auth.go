package codex

import (
	"context"
	"errors"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// auth.go is Codex's AuthModule. Codex authenticates a headless run entirely from the process
// environment — no config file is ever written (official test `exec_uses_codex_api_key_env_var`),
// which is exactly the security posture mindwire wants: secrets enter a run ONLY through EnvForRun,
// never via TurnInput.Config or the shell string.
//
// Two field-based methods (both non-interactive — the ChatGPT `localhost:1455` OAuth loopback is not
// headless-compatible, so it is deliberately not offered):
//   - "apiKey":      an OpenAI/Codex API key → CODEX_API_KEY (+ OPENAI_API_KEY for pre-Rust builds),
//                    with optional base URL / organization / project.
//   - "accessToken": a ChatGPT/PAT access token → CODEX_ACCESS_TOKEN.
//
// Status is presence-based: because creds are env-only they don't appear in `codex login status`
// (that reflects ~/.codex/auth.json, which mindwire never writes), so sniffing the CLI would wrongly
// report "not signed in". The daemon-stored credential is the source of truth for a mindwire run.

// Cred-store keys.
const (
	ckMethod      = "authMethod"
	ckAPIKey      = "apiKey"
	ckAccessToken = "accessToken"
	ckBaseURL     = "baseUrl"
	ckOrg         = "org"
	ckProject     = "project"
)

type authModule struct{ store agent.CredStore }

func newAuth(store agent.CredStore) *authModule { return &authModule{store: store} }

func (m *authModule) Methods() []agent.AuthMethod {
	return []agent.AuthMethod{
		{
			ID: "apiKey", Label: "API key", Scope: agent.ScopeUnified,
			Help: "An OpenAI/Codex API key (platform.openai.com). Sent to a run as CODEX_API_KEY — env-only, no config file is written.",
			Fields: []agent.Field{
				{Key: ckAPIKey, Label: "API key", Type: agent.FieldSecret, Required: true, Placeholder: "sk-…",
					Help: "Exported as CODEX_API_KEY (and OPENAI_API_KEY for older CLI builds)."},
				{Key: ckBaseURL, Label: "Base URL", Type: agent.FieldText, Placeholder: "https://api.openai.com/v1",
					Help: "Custom endpoint (OPENAI_BASE_URL). Leave blank for the default."},
				{Key: ckOrg, Label: "Organization", Type: agent.FieldText, Placeholder: "org-…",
					Help: "Optional OpenAI organization (OPENAI_ORGANIZATION)."},
				{Key: ckProject, Label: "Project", Type: agent.FieldText, Placeholder: "proj_…",
					Help: "Optional OpenAI project (OPENAI_PROJECT)."},
			},
		},
		{
			ID: "accessToken", Label: "Access token", Scope: agent.ScopeUnified,
			Help: "A ChatGPT/personal access token. Sent to a run as CODEX_ACCESS_TOKEN — env-only.",
			Fields: []agent.Field{
				{Key: ckAccessToken, Label: "Access token", Type: agent.FieldSecret, Required: true, Placeholder: "…",
					Help: "Exported as CODEX_ACCESS_TOKEN."},
			},
		},
	}
}

func (m *authModule) Begin(_ context.Context, methodID string) (agent.AuthState, error) {
	switch methodID {
	case "apiKey":
		return agent.AuthState{Method: "apiKey", Status: "needs_input",
			Fields: agent.MethodFields(m, "apiKey"), Message: "Enter your OpenAI/Codex API key."}, nil
	case "accessToken":
		return agent.AuthState{Method: "accessToken", Status: "needs_input",
			Fields: agent.MethodFields(m, "accessToken"), Message: "Enter your Codex access token."}, nil
	default:
		return agent.AuthState{}, errors.New("unknown auth method: " + methodID)
	}
}

func (m *authModule) Step(_ context.Context, input map[string]string) (agent.AuthState, error) {
	if key := strings.TrimSpace(input[ckAPIKey]); key != "" {
		if err := m.store.Set(ckAPIKey, key); err != nil {
			return agent.AuthState{}, err
		}
		// Optional non-secret companions; a trimmed empty value clears any prior setting.
		for _, k := range []string{ckBaseURL, ckOrg, ckProject} {
			if err := m.store.Set(k, strings.TrimSpace(input[k])); err != nil {
				return agent.AuthState{}, err
			}
		}
		_ = m.store.Set(ckAccessToken, "") // switching methods clears the other credential
		_ = m.store.Set(ckMethod, "apiKey")
		return agent.AuthState{Method: "apiKey", Status: "complete"}, nil
	}
	if tok := strings.TrimSpace(input[ckAccessToken]); tok != "" {
		if err := m.store.Set(ckAccessToken, tok); err != nil {
			return agent.AuthState{}, err
		}
		_ = m.store.Set(ckAPIKey, "")
		_ = m.store.Set(ckMethod, "accessToken")
		return agent.AuthState{Method: "accessToken", Status: "complete"}, nil
	}
	return agent.AuthState{Status: "error", Message: "no credential provided"}, nil
}

// Status reports whether a run would authenticate, from the stored credential (env-only creds don't
// show up in `codex login status`, so presence is authoritative here).
func (m *authModule) Status(_ context.Context) agent.AuthStatus {
	switch {
	case strings.TrimSpace(m.store.Get(ckAPIKey)) != "":
		return agent.AuthStatus{Configured: true, Method: "apiKey", Detail: "API key set"}
	case strings.TrimSpace(m.store.Get(ckAccessToken)) != "":
		return agent.AuthStatus{Configured: true, Method: "accessToken", Detail: "Access token set"}
	default:
		return agent.AuthStatus{Configured: false, Detail: "No credential set — add an API key or access token."}
	}
}

// EnvForRun is the ONLY place credentials enter a run. Endpoint/org/project are orthogonal to the
// credential and exported whenever set; the credential itself is chosen by the stored method, falling
// back to whichever secret is present so a run still authenticates if the method tag was lost.
func (m *authModule) EnvForRun() map[string]string {
	env := map[string]string{}
	if v := strings.TrimSpace(m.store.Get(ckBaseURL)); v != "" {
		env["OPENAI_BASE_URL"] = v
	}
	if v := strings.TrimSpace(m.store.Get(ckOrg)); v != "" {
		env["OPENAI_ORGANIZATION"] = v
	}
	if v := strings.TrimSpace(m.store.Get(ckProject)); v != "" {
		env["OPENAI_PROJECT"] = v
	}

	apiKey := strings.TrimSpace(m.store.Get(ckAPIKey))
	accessToken := strings.TrimSpace(m.store.Get(ckAccessToken))
	method := m.store.Get(ckMethod)
	if method == "" { // presence fallback if the method tag is missing
		switch {
		case apiKey != "":
			method = "apiKey"
		case accessToken != "":
			method = "accessToken"
		}
	}
	switch method {
	case "apiKey":
		if apiKey != "" {
			env["CODEX_API_KEY"] = apiKey  // exec-only, highest precedence
			env["OPENAI_API_KEY"] = apiKey // also set for pre-Rust CLI builds
		}
	case "accessToken":
		if accessToken != "" {
			env["CODEX_ACCESS_TOKEN"] = accessToken
		}
	}
	// Every registered CUSTOM provider's stored key, exported under the env-var name it was registered
	// with (agent.ProviderEnvForRun) so config.toml's `[model_providers.<id>] env_key = "VAR"` resolves
	// at runtime — the config file never carries a literal secret.
	for name, key := range agent.ProviderEnvForRun(m.store) {
		env[name] = key
	}
	return env
}
