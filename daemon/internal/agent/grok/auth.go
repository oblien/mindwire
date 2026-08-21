package grok

import (
	"context"
	"errors"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

const keyAPIKey = "apiKey"

type authModule struct{ store agent.CredStore }

func newAuth(store agent.CredStore) *authModule { return &authModule{store: store} }

func (m *authModule) Methods() []agent.AuthMethod {
	return []agent.AuthMethod{{ID: "apiKey", Label: "xAI API key", Scope: agent.ScopeUnified,
		Help:   "Exported only to Grok Build as XAI_API_KEY; it is never written to Grok's config.",
		Fields: []agent.Field{{Key: keyAPIKey, Label: "xAI API key", Type: agent.FieldSecret, Required: true, Placeholder: "xai-…"}}}}
}

func (m *authModule) Begin(_ context.Context, method string) (agent.AuthState, error) {
	if method != "apiKey" {
		return agent.AuthState{}, errors.New("unknown auth method: " + method)
	}
	return agent.AuthState{Method: method, Status: "needs_input", Fields: agent.MethodFields(m, method), Message: "Enter your xAI API key."}, nil
}

func (m *authModule) Step(_ context.Context, input map[string]string) (agent.AuthState, error) {
	key := strings.TrimSpace(input[keyAPIKey])
	if key == "" {
		return agent.AuthState{Method: "apiKey", Status: "error", Message: "no API key provided"}, nil
	}
	if err := m.store.Set(keyAPIKey, key); err != nil {
		return agent.AuthState{}, err
	}
	return agent.AuthState{Method: "apiKey", Status: "complete"}, nil
}

func (m *authModule) Status(_ context.Context) agent.AuthStatus {
	if strings.TrimSpace(m.store.Get(keyAPIKey)) == "" {
		return agent.AuthStatus{Detail: "No xAI API key set."}
	}
	return agent.AuthStatus{Configured: true, Method: "apiKey", Detail: "xAI API key set"}
}

func (m *authModule) EnvForRun() map[string]string {
	if key := strings.TrimSpace(m.store.Get(keyAPIKey)); key != "" {
		return map[string]string{"XAI_API_KEY": key}
	}
	return map[string]string{}
}
