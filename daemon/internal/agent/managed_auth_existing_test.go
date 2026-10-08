package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/authtest"
)

type existingAuthFixture struct {
	store     agent.CredStore
	source    *agent.AuthSource
	available bool
	active    bool
	imports   atomic.Int32
	checks    atomic.Int32
	verify    func(context.Context) error
}

func (a *existingAuthFixture) SetAuthSource(source *agent.AuthSource) { a.source = source }
func (a *existingAuthFixture) ExistingMethod() *agent.AuthMethod {
	if a.source == nil || !a.available {
		return nil
	}
	return &agent.AuthMethod{ID: agent.ExistingAuthMethod, Existing: &agent.ExistingAuthConnection{Name: "Saved account"}}
}
func (a *existingAuthFixture) UseExisting(ctx context.Context) (agent.AuthState, error) {
	a.imports.Add(1)
	if a.verify != nil {
		if err := a.verify(ctx); err != nil {
			return agent.AuthState{Status: "error", Message: err.Error()}, err
		}
	}
	if err := a.store.Set("authMethod", agent.ExistingAuthMethod); err != nil {
		return agent.AuthState{}, err
	}
	return agent.AuthState{Method: agent.ExistingAuthMethod, Status: "complete"}, a.store.Set("configured", "true")
}
func (a *existingAuthFixture) Methods() []agent.AuthMethod { return nil }
func (a *existingAuthFixture) Begin(context.Context, string) (agent.AuthState, error) {
	return agent.AuthState{Status: "needs_input"}, nil
}
func (a *existingAuthFixture) Step(context.Context, map[string]string) (agent.AuthState, error) {
	return agent.AuthState{Status: "needs_input"}, nil
}
func (a *existingAuthFixture) Status(context.Context) agent.AuthStatus {
	a.checks.Add(1)
	return agent.AuthStatus{Configured: a.store.Get("configured") == "true", Method: a.store.Get("authMethod")}
}
func (a *existingAuthFixture) EnvForRun() map[string]string { return nil }
func (a *existingAuthFixture) Active() bool                 { return a.active }
func (a *existingAuthFixture) Logout(context.Context) error {
	return a.store.Set("configured", "")
}

func newExistingAuth(t *testing.T, reuse string) (*agent.ManagedAuth, *existingAuthFixture) {
	t.Helper()
	native := &existingAuthFixture{store: authtest.NewStore(nil), available: true}
	auth := agent.ManageAuth(native, native.store)
	auth.SetAuthSource(&agent.AuthSource{Home: t.TempDir(), Reuse: reuse})
	return auth, native
}

func TestTrustedNativeConnectionIsAutomaticOnFirstUse(t *testing.T) {
	for _, entry := range []string{"status", "turn"} {
		t.Run(entry, func(t *testing.T) {
			auth, native := newExistingAuth(t, "")
			if len(auth.Methods()) != 1 || native.imports.Load() != 0 {
				t.Fatal("listing methods must remain metadata-only")
			}
			var readers sync.WaitGroup
			for range 12 {
				readers.Add(1)
				go func() {
					defer readers.Done()
					if entry == "status" {
						if !auth.Status(context.Background()).Configured {
							t.Error("status required an explicit auth.begin")
						}
					} else if err := auth.PrepareAuth(context.Background()); err != nil {
						t.Error(err)
					}
				}()
			}
			readers.Wait()
			if native.imports.Load() != 1 || !auth.Status(context.Background()).Configured {
				t.Fatal("concurrent first use must verify and import exactly once")
			}
			checks := native.checks.Load()
			for range 3 {
				if err := auth.PrepareAuth(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if native.checks.Load() != checks {
				t.Fatal("later turns repeated native auth commands")
			}
		})
	}
}

func TestNativeConnectionManualReuseIsOptIn(t *testing.T) {
	auth, native := newExistingAuth(t, "manual")
	if auth.Status(context.Background()).Configured {
		t.Fatal("manual source auto-connected")
	}
	if err := auth.PrepareAuth(context.Background()); err != nil || native.imports.Load() != 0 {
		t.Fatal("turn ignored manual reuse policy")
	}
	state, err := auth.Begin(context.Background(), agent.ExistingAuthMethod)
	if err != nil || state.Status != "complete" || !auth.Status(context.Background()).Configured {
		t.Fatal("explicit reuse did not work")
	}
}

func TestNativeConnectionUnknownReuseDoesNotAutoConnect(t *testing.T) {
	// Go hosts can construct AuthSource directly, bypassing JSON validation.
	auth, native := newExistingAuth(t, "unsupported")
	if auth.Status(context.Background()).Configured || native.imports.Load() != 0 {
		t.Fatal("an unknown policy silently authorized automatic connection reuse")
	}
}

func TestNativeConnectionPreservesCurrentAndExplicitChoices(t *testing.T) {
	for _, choice := range []string{"configured", "expired-choice", "pending-begin", "pending-step"} {
		t.Run(choice, func(t *testing.T) {
			auth, native := newExistingAuth(t, "automatic")
			ctx := context.Background()
			switch choice {
			case "configured":
				_ = native.store.Set("configured", "true") // Native config may have no stored method.
			case "expired-choice":
				_ = native.store.Set("authMethod", "login")
			case "pending-begin":
				_, _ = auth.Begin(ctx, "login")
			case "pending-step":
				_, _ = auth.Step(ctx, map[string]string{"apiKey": "draft"})
			}
			auth.Status(ctx)
			_ = native.store.Set("configured", "")
			if err := auth.PrepareAuth(ctx); err != nil {
				t.Fatal(err)
			}
			auth.Status(ctx)
			if native.imports.Load() != 0 {
				t.Fatal("first-use discovery replaced an existing account choice")
			}
		})
	}
}

func TestNativeConnectionExplicitSignOutSurvivesRestart(t *testing.T) {
	auth, native := newExistingAuth(t, "")
	ctx := context.Background()
	if !auth.Status(ctx).Configured {
		t.Fatal("first use did not connect")
	}
	if err := auth.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	// A fresh wrapper has no in-memory first-use state; the persisted choice wins.
	restarted := agent.ManageAuth(native, native.store)
	restarted.SetAuthSource(native.source)
	if status := restarted.Status(ctx); status.Configured || !status.SignedOut {
		t.Fatal("restart reconnected an explicitly disconnected account")
	}
	if err := restarted.PrepareAuth(ctx); err == nil || native.imports.Load() != 1 {
		t.Fatal("direct turn ignored explicit sign-out")
	}
	state, err := restarted.Begin(ctx, agent.ExistingAuthMethod)
	if err != nil || state.Status != "complete" || !restarted.Status(ctx).Configured || native.imports.Load() != 2 {
		t.Fatal("explicit reconnect did not restore the account")
	}
}

func TestNativeConnectionCanBeDiscoveredAfterAnEmptyScan(t *testing.T) {
	auth, native := newExistingAuth(t, "")
	native.available = false
	if auth.Status(context.Background()).Configured || native.imports.Load() != 0 {
		t.Fatal("empty host claimed a connection")
	}
	native.available = true
	if !auth.Status(context.Background()).Configured || native.imports.Load() != 1 {
		t.Fatal("a later host login still required a button click")
	}
}

func TestNativeConnectionDoesNotInterruptAnActiveLogin(t *testing.T) {
	auth, native := newExistingAuth(t, "")
	native.active = true
	if auth.Status(context.Background()).Configured || native.imports.Load() != 0 {
		t.Fatal("discovery interrupted interactive authentication")
	}
	native.active = false
	if !auth.Status(context.Background()).Configured {
		t.Fatal("discovery remained blocked after the native flow ended")
	}
}

func TestNativeConnectionFailureNeedsExplicitRetry(t *testing.T) {
	auth, native := newExistingAuth(t, "")
	native.verify = func(context.Context) error { return errors.New("must-not-expose-a-credential") }
	ctx := context.Background()
	for range 3 {
		status := auth.Status(ctx)
		if status.Configured || status.Detail == "" || strings.Contains(status.Detail, "must-not-expose") {
			t.Fatal("failed verification did not return a safe recovery message")
		}
	}
	if native.imports.Load() != 1 {
		t.Fatal("polling repeatedly imported an invalid connection")
	}
	if err := auth.PrepareAuth(ctx); err == nil || strings.Contains(err.Error(), "must-not-expose") {
		t.Fatal("turn did not surface the safe connection error")
	}
	native.verify = nil
	state, err := auth.Begin(ctx, agent.ExistingAuthMethod)
	if err != nil || state.Status != "complete" || !auth.Status(ctx).Configured || native.imports.Load() != 2 {
		t.Fatal("explicit retry did not recover")
	}
}

func TestNativeConnectionCancelledScanCanRetry(t *testing.T) {
	auth, native := newExistingAuth(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	native.verify = func(context.Context) error { cancel(); return ctx.Err() }
	if auth.Status(ctx).Configured {
		t.Fatal("cancelled import was accepted")
	}
	native.verify = nil
	if !auth.Status(context.Background()).Configured || native.imports.Load() != 2 {
		t.Fatal("a detached reader permanently disabled first-use discovery")
	}
}
