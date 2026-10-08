package agent

import (
	"context"
	"errors"
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

// ManagedAuth reuses a trusted host connection on first setup and keeps explicit
// sign-out authoritative across clients and daemon restarts. After sign-out,
// only completing a new authentication flow reconnects the harness.
type ManagedAuth struct {
	AuthModule
	store         CredStore
	mu            sync.RWMutex
	changing      atomic.Bool
	autoExisting  bool
	autoAttempted bool
	autoError     string
}

func ManageAuth(auth AuthModule, store CredStore) *ManagedAuth {
	return &ManagedAuth{AuthModule: auth, store: store}
}

// SetAuthSource binds trusted host configuration at startup. Native adapters own
// format/keychain discovery; the shared auth layer owns first-use and sign-out.
func (m *ManagedAuth) SetAuthSource(source *AuthSource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if native, ok := m.AuthModule.(ExistingAuthModule); ok {
		native.SetAuthSource(source)
		m.autoExisting = source != nil && (source.Reuse == "" || source.Reuse == "automatic")
	}
}

func (m *ManagedAuth) Disconnected() bool { return m.store.Get(signedOutKey) == "true" }

func (m *ManagedAuth) Methods() []AuthMethod {
	methods := m.AuthModule.Methods()
	if native, ok := m.AuthModule.(ExistingAuthModule); ok {
		if method := native.ExistingMethod(); method != nil {
			methods = append([]AuthMethod{*method}, methods...)
		}
	}
	if _, native := m.AuthModule.(AuthLogoutModule); native && m.Disconnected() {
		methods = append(methods, AuthMethod{ID: "configFile", Label: "Use workspace configuration", Scope: ScopeUnified,
			Help: "Reconnect using an account already configured in this workspace. You can edit the native config file first."})
	}
	return methods
}

func (m *ManagedAuth) Status(ctx context.Context) AuthStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status(ctx)
}

// PrepareAuth takes the same first-use path as Status, without repeating native
// status commands on subsequent turns. An absent host login remains discoverable
// later, but an existing choice or failed import needs explicit user action.
func (m *ManagedAuth) PrepareAuth(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disconnected() {
		return errors.New("Signed out. Connect an account to continue.")
	}
	if m.autoExisting && !m.autoAttempted && m.store.Get("authMethod") == "" {
		m.status(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.autoError != "" {
		return errors.New(m.autoError)
	}
	return nil
}

// status runs under mu, including credential import and native verification.
func (m *ManagedAuth) status(ctx context.Context) AuthStatus {
	if m.Disconnected() {
		return AuthStatus{SignedOut: true, Detail: "Signed out. Connect an account to continue."}
	}
	status := m.AuthModule.Status(ctx)
	if status.Configured || m.store.Get("authMethod") != "" {
		m.autoAttempted = true // keep the selected account, even if it later expires
		m.autoError = ""
		return status
	}
	if !m.autoExisting || m.autoAttempted || ctx.Err() != nil {
		if !status.Configured && m.autoError != "" {
			status.Detail = m.autoError
		}
		return status
	}
	if active, ok := m.AuthModule.(interface{ Active() bool }); ok && active.Active() {
		return status
	}
	native := m.AuthModule.(ExistingAuthModule)
	if native.ExistingMethod() == nil {
		return status
	}
	// A failure needs an explicit retry/sign-in, not a keychain read on every poll.
	// Missing connections remain discoverable by a later scan. Concurrent status
	// requests share this lock, so only one import can happen for a new profile.
	m.autoAttempted = true
	m.changing.Store(true)
	defer m.changing.Store(false)
	state, err := native.UseExisting(ctx)
	if ctx.Err() != nil {
		m.autoAttempted = false // a detached status reader did not choose a connection
		return status
	}
	if err != nil || state.Status != "complete" {
		m.autoError = "Could not use the saved connection. Retry it in agent settings or choose another sign-in method."
		status.Detail = m.autoError
		return status
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
	m.autoAttempted = true
	m.autoError = ""
	if native, ok := m.AuthModule.(ExistingAuthModule); ok && method == ExistingAuthMethod {
		if !m.Disconnected() && m.AuthModule.Status(ctx).Configured {
			return AuthState{Method: method, Status: "error", Message: "Disconnect the current account before using a different saved connection."}, nil
		}
		state, err := native.UseExisting(ctx)
		return m.finish(ctx, state, err)
	}
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
	m.autoAttempted = true
	m.autoError = ""
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
