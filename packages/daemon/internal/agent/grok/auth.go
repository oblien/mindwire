package grok

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/nativeauth"
	"github.com/oblien/mindwire/packages/daemon/internal/proc"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
)

const keyAPIKey = "apiKey"

type authModule struct {
	store  agent.CredStore
	native *nativeauth.Auth
}

func newAuth(store agent.CredStore) *authModule {
	native := nativeauth.New(nativeauth.Config{ID: "grok", Name: "Grok Build",
		Logins: []agent.AuthMethod{{ID: "login", Label: "Sign in with Grok", Interactive: true, Scope: agent.ScopeUnified}},
		Environment: func(_ string) map[string]string {
			return map[string]string{"GROK_HOME": configBase(), "XAI_API_KEY": "", "GROK_OAUTH2_REFERRER": "mindwire", "BROWSER": "true"}
		},
		Login: func(flow *agent.AuthFlow, env map[string]string) error {
			return nativeauth.LoginCommand(flow, "grok", []string{"--no-auto-update", "login", "--oauth", "--device-auth"}, env, "auth.x.ai", "accounts.x.ai", "x.ai", "grok.com")
		},
		Logout: func(ctx context.Context, env map[string]string) error {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			cmd := toolchain.CommandContext(ctx, "grok", "--no-auto-update", "logout")
			proc.Group(cmd)
			for k, v := range env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			return cmd.Run()
		},
	}, store)
	return &authModule{store: store, native: native}
}

func (m *authModule) Methods() []agent.AuthMethod {
	return append(m.native.Methods(), agent.AuthMethod{ID: "apiKey", Label: "xAI API key", Scope: agent.ScopeUnified,
		Help:   "Exported only to Grok Build as XAI_API_KEY; it is never written to Grok's config.",
		Fields: []agent.Field{{Key: keyAPIKey, Label: "xAI API key", Type: agent.FieldSecret, Required: true, Placeholder: "xai-…"}}})
}

func (m *authModule) Begin(ctx context.Context, method string) (agent.AuthState, error) {
	if method == "login" {
		return m.native.Begin(ctx, method)
	}
	if method != "apiKey" {
		return agent.AuthState{}, errors.New("unknown auth method: " + method)
	}
	return agent.AuthState{Method: method, Status: "needs_input", Fields: agent.MethodFields(m, method), Message: "Enter your xAI API key."}, nil
}

func (m *authModule) Step(ctx context.Context, input map[string]string) (agent.AuthState, error) {
	if input[agent.AuthFlowIDKey] != "" || input[agent.AuthActionKey] != "" {
		return m.native.Step(ctx, input)
	}
	if m.native.Active() {
		return agent.AuthState{}, errors.New("cancel the current sign-in first")
	}
	key := strings.TrimSpace(input[keyAPIKey])
	if key == "" {
		return agent.AuthState{Method: "apiKey", Status: "error", Message: "no API key provided"}, nil
	}
	if err := m.store.Set(keyAPIKey, key); err != nil {
		return agent.AuthState{}, err
	}
	if err := m.store.Set("authMethod", "apiKey"); err != nil {
		return agent.AuthState{}, err
	}
	return agent.AuthState{Method: "apiKey", Status: "complete"}, nil
}

func (m *authModule) Status(ctx context.Context) agent.AuthStatus {
	if m.store.Get("authMethod") == "login" {
		return m.native.Status(ctx)
	}
	if strings.TrimSpace(m.store.Get(keyAPIKey)) == "" {
		return agent.AuthStatus{Detail: "No xAI API key set."}
	}
	return agent.AuthStatus{Configured: true, Method: "apiKey", Detail: "xAI API key set"}
}

func (m *authModule) EnvForRun() map[string]string {
	if m.store.Get("authMethod") == "login" {
		return m.native.EnvForRun()
	}
	if key := strings.TrimSpace(m.store.Get(keyAPIKey)); key != "" {
		return map[string]string{"XAI_API_KEY": key}
	}
	return map[string]string{}
}

func (m *authModule) Active() bool                     { return m.native.Active() }
func (m *authModule) Logout(ctx context.Context) error { return m.native.Logout(ctx) }
