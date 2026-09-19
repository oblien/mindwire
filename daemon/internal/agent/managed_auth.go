package agent

import (
	"context"
	"sync"
	"sync/atomic"
)

// AuthLogoutModule removes a harness's native subscription session. Field-based
// connections are cleared by ManagedAuth; shared provider connections and model
// settings belong to the workspace and are deliberately preserved.
type AuthLogoutModule interface {
	Logout(context.Context) error
}

const signedOutKey = "authSignedOut"

// ManagedAuth keeps explicit sign-out authoritative across clients and daemon
// restarts, even when a CLI can discover credentials in its native config/env.
// Only completing a new authentication flow reconnects the harness.
type ManagedAuth struct {
	AuthModule
	store    CredStore
	mu       sync.RWMutex
	changing atomic.Bool
}

func ManageAuth(auth AuthModule, store CredStore) *ManagedAuth {
	return &ManagedAuth{AuthModule: auth, store: store}
}

func (m *ManagedAuth) Disconnected() bool { return m.store.Get(signedOutKey) == "true" }

func (m *ManagedAuth) Methods() []AuthMethod {
	methods := m.AuthModule.Methods()
	if _, native := m.AuthModule.(AuthLogoutModule); native && m.Disconnected() {
		methods = append(methods, AuthMethod{ID: "configFile", Label: "Use workspace configuration", Scope: ScopeUnified,
			Help: "Reconnect using an account already configured in this workspace. You can edit the native config file first."})
	}
	return methods
}

func (m *ManagedAuth) Status(ctx context.Context) AuthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Disconnected() {
		return AuthStatus{SignedOut: true, Detail: "Signed out. Connect an account to continue."}
	}
	return m.AuthModule.Status(ctx)
}

func (m *ManagedAuth) EnvForRun() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Disconnected() {
		return map[string]string{}
	}
	return m.AuthModule.EnvForRun()
}

func (m *ManagedAuth) Begin(ctx context.Context, method string) (AuthState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, native := m.AuthModule.(AuthLogoutModule); native && method == "configFile" && m.Disconnected() {
		if !m.AuthModule.Status(ctx).Configured {
			return AuthState{Method: method, Status: "error", Message: "No authenticated account found in the workspace configuration. Sign in or connect a provider."}, nil
		}
		return m.finish(ctx, AuthState{Method: method, Status: "complete"}, nil)
	}
	state, err := m.AuthModule.Begin(ctx, method)
	return m.finish(ctx, state, err)
}

func (m *ManagedAuth) Step(ctx context.Context, input map[string]string) (AuthState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.AuthModule.Step(ctx, input)
	if input[AuthActionKey] == "cancel" {
		return state, err
	}
	return m.finish(ctx, state, err)
}

func (m *ManagedAuth) finish(ctx context.Context, state AuthState, err error) (AuthState, error) {
	if err == nil && state.Status == "complete" && m.Disconnected() && m.AuthModule.Status(ctx).Configured {
		err = m.store.Set(signedOutKey, "")
	}
	return state, err
}

func (m *ManagedAuth) Active() bool {
	if m.changing.Load() {
		return true
	}
	if active, ok := m.AuthModule.(interface{ Active() bool }); ok {
		return active.Active()
	}
	return false
}

func (m *ManagedAuth) Logout(ctx context.Context) error {
	m.changing.Store(true)
	defer m.changing.Store(false)
	m.mu.Lock()
	defer m.mu.Unlock()
	if native, ok := m.AuthModule.(AuthLogoutModule); ok && !m.Disconnected() {
		if err := native.Logout(ctx); err != nil {
			return err
		}
	}
	// Persist this first so a partial credential-store write cannot reconnect
	// through leftover native or shared credentials.
	if err := m.store.Set(signedOutKey, "true"); err != nil {
		return err
	}
	keys := map[string]bool{"authMethod": true, "oauthToken": true}
	for _, method := range m.Methods() {
		for _, field := range method.Fields {
			keys[field.Key] = true
		}
	}
	for key := range keys {
		if err := m.store.Set(key, ""); err != nil {
			return err
		}
	}
	return nil
}
