package agent_test

import (
	"context"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/authtest"
)

type nativeFallbackAuth struct {
	store     agent.CredStore
	loggedOut bool
}

func (a *nativeFallbackAuth) Methods() []agent.AuthMethod {
	return []agent.AuthMethod{{ID: "apiKey", Fields: []agent.Field{{Key: "apiKey", Type: agent.FieldSecret}}}}
}
func (a *nativeFallbackAuth) Begin(context.Context, string) (agent.AuthState, error) {
	return agent.AuthState{Status: "pending"}, nil
}
func (a *nativeFallbackAuth) Step(_ context.Context, input map[string]string) (agent.AuthState, error) {
	if input[agent.AuthActionKey] == "cancel" {
		return agent.AuthState{Status: "error"}, nil
	}
	if input["apiKey"] == "" {
		return agent.AuthState{Status: "pending"}, nil
	}
	return agent.AuthState{Status: "complete"}, a.store.Set("apiKey", input["apiKey"])
}
func (a *nativeFallbackAuth) Status(context.Context) agent.AuthStatus {
	return agent.AuthStatus{Configured: true, Method: "configFile"}
}
func (a *nativeFallbackAuth) EnvForRun() map[string]string {
	return map[string]string{"NATIVE_API_KEY": "fixture"}
}
func (a *nativeFallbackAuth) Logout(context.Context) error { a.loggedOut = true; return nil }

func TestExplicitSignOutSurvivesNativeFallbackAndReconnectRequiresCompletion(t *testing.T) {
	store := authtest.NewStore(map[string]string{"apiKey": "old", "model": "selected", "provider:shared:apiKey": "shared-key"})
	native := &nativeFallbackAuth{store: store}
	auth := agent.ManageAuth(native, store)
	ctx := context.Background()
	if err := auth.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if !native.loggedOut || auth.Status(ctx).Configured || len(auth.EnvForRun()) != 0 || store.Get("apiKey") != "" {
		t.Fatal("sign-out was not authoritative")
	}
	if store.Get("model") != "selected" || store.Get("provider:shared:apiKey") != "shared-key" {
		t.Fatal("unrelated settings/provider were deleted")
	}
	auth = agent.ManageAuth(native, store)
	if auth.Status(ctx).Configured {
		t.Fatal("restart silently reconnected native credentials")
	}
	_, _ = auth.Begin(ctx, "login")
	_, _ = auth.Step(ctx, map[string]string{agent.AuthActionKey: "cancel"})
	if auth.Status(ctx).Configured {
		t.Fatal("an unfinished sign-in reconnected")
	}
	state, err := auth.Step(ctx, map[string]string{"apiKey": "new"})
	if err != nil || state.Status != "complete" || !auth.Status(ctx).Configured {
		t.Fatalf("reconnect: %+v %v", state, err)
	}
}
