// Package nativeauth adapts native login processes to the product's existing
// flow-scoped auth API. Each connection owns a private credential directory.
package nativeauth

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
)

type Config struct {
	ID, Name, KeyEnv string
	Logins           []agent.AuthMethod
	Environment      func(string) map[string]string
	Login            func(*agent.AuthFlow, map[string]string) error
	Verify           func(context.Context, map[string]string) bool
	Logout           func(context.Context, map[string]string) error
}

type Auth struct {
	config Config
	store  agent.CredStore
	mu     sync.Mutex
	flow   *agent.AuthFlow
	done   chan struct{}
}

func New(config Config, store agent.CredStore) *Auth { return &Auth{config: config, store: store} }
func (a *Auth) Methods() []agent.AuthMethod {
	out := append([]agent.AuthMethod{}, a.config.Logins...)
	if a.config.KeyEnv != "" {
		out = append(out, agent.AuthMethod{ID: "apiKey", Label: a.config.Name + " API key", Scope: agent.ScopeUnified, Fields: []agent.Field{{Key: "apiKey", Label: "API key", Type: agent.FieldSecret, Required: true}}})
	}
	return out
}

func (a *Auth) home() string {
	id := a.store.Get("nativeAuthID")
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, "/\\:") {
		id = "unconfigured"
	}
	return filepath.Join(filepath.Dir(toolchain.Root()), "auth", a.config.ID, id)
}

func (a *Auth) prepare() error {
	if a.store.Get("nativeAuthID") == "" {
		if err := a.store.Set("nativeAuthID", rand.Text()); err != nil {
			return err
		}
	}
	return os.MkdirAll(a.home(), 0700)
}

func (a *Auth) PrepareAuth(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store.Get("authMethod") == "" {
		return errors.New("connect " + a.config.Name + " before starting a turn")
	}
	return a.prepare()
}

func (a *Auth) EnvForRun() map[string]string {
	env := a.config.Environment(a.home())
	env["MINDWIRE_NATIVE_AUTH_METHOD"] = a.store.Get("authMethod")
	if a.config.KeyEnv != "" {
		env[a.config.KeyEnv] = ""
		if a.store.Get("authMethod") == "apiKey" {
			env[a.config.KeyEnv] = a.store.Get("apiKey")
		}
	}
	return env
}

func (a *Auth) Begin(ctx context.Context, method string) (agent.AuthState, error) {
	a.mu.Lock()
	if err := a.prepare(); err != nil {
		a.mu.Unlock()
		return agent.AuthState{}, err
	}
	if method == "apiKey" && a.config.KeyEnv != "" {
		if a.flow != nil && a.flow.Active() {
			a.mu.Unlock()
			return agent.AuthState{}, errors.New("cancel the current sign-in first")
		}
		a.mu.Unlock()
		return agent.AuthState{Method: method, Status: "needs_input", Fields: agent.MethodFields(a, method)}, nil
	}
	valid := false
	for _, m := range a.config.Logins {
		valid = valid || method == m.ID
	}
	if !valid {
		a.mu.Unlock()
		return agent.AuthState{}, errors.New("unknown auth method")
	}
	if a.flow != nil && a.flow.Active() {
		flow := a.flow
		a.mu.Unlock()
		return flow.InitialState(ctx), nil
	}
	flow := agent.NewAuthFlow(method)
	a.flow = flow
	a.done = make(chan struct{})
	done := a.done
	env := a.EnvForRun()
	// Login deliberately excludes a previously selected API key.
	if a.config.KeyEnv != "" {
		env[a.config.KeyEnv] = ""
	}
	env["MINDWIRE_NATIVE_AUTH_METHOD"] = method
	a.mu.Unlock()
	go func() {
		defer close(done)
		if err := a.config.Login(flow, env); err != nil {
			flow.Fail(a.config.Name + " sign-in failed. Check the installation and try again.")
			return
		}
		flow.Complete(func() error {
			if err := a.store.Set("apiKey", ""); err != nil {
				return err
			}
			return a.store.Set("authMethod", method)
		})
	}()
	return flow.InitialState(ctx), nil
}

func (a *Auth) Step(_ context.Context, input map[string]string) (agent.AuthState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if input[agent.AuthFlowIDKey] != "" || input[agent.AuthActionKey] != "" {
		if a.flow == nil {
			return agent.AuthState{}, errors.New("no sign-in in progress")
		}
		return a.flow.Step(input)
	}
	if a.flow != nil && a.flow.Active() {
		return agent.AuthState{}, errors.New("poll or cancel using the current sign-in flow ID")
	}
	key := strings.TrimSpace(input["apiKey"])
	if a.config.KeyEnv == "" || key == "" {
		return agent.AuthState{}, errors.New("enter an API key")
	}
	if err := a.prepare(); err != nil {
		return agent.AuthState{}, err
	}
	if err := a.store.Set("apiKey", key); err != nil {
		return agent.AuthState{}, err
	}
	if err := a.store.Set("authMethod", "apiKey"); err != nil {
		return agent.AuthState{}, err
	}
	return agent.AuthState{Method: "apiKey", Status: "complete"}, nil
}

func (a *Auth) Callback(_ context.Context, id, raw string) (agent.AuthState, error) {
	a.mu.Lock()
	flow := a.flow
	a.mu.Unlock()
	if flow == nil {
		return agent.AuthState{}, errors.New("no sign-in in progress")
	}
	return flow.Step(map[string]string{agent.AuthFlowIDKey: id, agent.AuthCallbackKey: raw})
}

func (a *Auth) Status(ctx context.Context) agent.AuthStatus {
	method := a.store.Get("authMethod")
	configured := method != ""
	if method == "apiKey" {
		configured = strings.TrimSpace(a.store.Get("apiKey")) != ""
	}
	if configured && method != "apiKey" && a.config.Verify != nil {
		configured = a.config.Verify(ctx, a.EnvForRun())
	}
	detail := "Connect " + a.config.Name + " to continue."
	if configured {
		detail = a.config.Name + " connection saved"
	}
	return agent.AuthStatus{Configured: configured, Method: method, Detail: detail}
}
func (a *Auth) Active() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.flow != nil && a.flow.Active()
}

func (a *Auth) Logout(ctx context.Context) error {
	a.mu.Lock()
	if a.flow != nil {
		a.flow.Cancel()
	}
	done := a.done
	a.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if a.config.Logout != nil && a.store.Get("authMethod") != "" && a.store.Get("authMethod") != "apiKey" {
		if err := a.config.Logout(ctx, a.EnvForRun()); err != nil {
			return err
		}
	}
	// Rotate the private credential namespace. A late native write or another
	// daemon process can never reconnect the signed-out connection.
	return a.store.Set("nativeAuthID", rand.Text())
}
