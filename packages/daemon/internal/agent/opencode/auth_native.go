package opencode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/nativeauth"
	"github.com/oblien/mindwire/packages/daemon/internal/proc"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
)

type nativeLoginMethod struct {
	Provider   string
	Index      int
	Method     agent.AuthMethod
	Conditions map[string]nativeAuthCondition
}
type nativeAuthCondition struct{ Key, Op, Value string }
type nativeAuthTransport struct {
	server server
	base   string
	close  func()
}

var openAuthServer = startAuthServer
var errNativeAuthUnavailable = errors.New("install OpenCode before discovering native sign-in methods")

func startAuthServer(ctx context.Context) (*nativeAuthTransport, error) {
	ctx, cancel := context.WithCancel(ctx)
	port, err := freePort()
	if err != nil {
		cancel()
		return nil, err
	}
	s := server{password: rand.Text() + rand.Text()}
	cmd := toolchain.CommandContext(ctx, "opencode", "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
	cmd.Env = append(cmd.Env, "OPENCODE_PASSWORD="+s.password, "OPENCODE_SERVER_PASSWORD="+s.password, "OPENCODE_SERVER_USERNAME=opencode", "BROWSER=true")
	proc.Group(cmd)
	if err = cmd.Start(); err != nil {
		cancel()
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, errNativeAuthUnavailable
		}
		return nil, errors.New("could not start OpenCode sign-in service")
	}
	close := func() { cancel(); _ = cmd.Wait() }
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if _, err = s.waitHealthy(ctx, base); err != nil {
		close()
		return nil, errors.New("OpenCode sign-in service did not become ready")
	}
	return &nativeAuthTransport{server: s, base: base, close: close}, nil
}

func (t *nativeAuthTransport) call(ctx context.Context, method, path string, body any, out any) error {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	return t.server.doJSON(ctx, &http.Client{}, method, t.base+path, raw, out)
}

func (m *authModule) DiscoverMethods(ctx context.Context) ([]agent.AuthMethod, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	transport, err := openAuthServer(ctx)
	if err != nil {
		if errors.Is(err, errNativeAuthUnavailable) {
			return m.Methods(), nil
		}
		return nil, err
	}
	defer transport.close()
	var providers map[string][]struct {
		Type, Label string
		Prompts     []struct {
			Type, Key, Message, Placeholder string
			Options                         []struct{ Label, Value string }
			When                            *nativeAuthCondition
		}
	}
	if err = transport.call(ctx, http.MethodGet, "/provider/auth", nil, &providers); err != nil {
		return nil, err
	}
	methods := map[string]nativeLoginMethod{}
	for provider, items := range providers {
		for i, item := range items {
			if item.Type != "oauth" {
				continue
			}
			digest := sha256.Sum256([]byte(item.Label))
			id := fmt.Sprintf("oauth:%s:%x", provider, digest[:8])
			method := agent.AuthMethod{ID: id, Label: provider + ": " + item.Label, Interactive: true, Scope: agent.ScopeUnified}
			conditions := map[string]nativeAuthCondition{}
			for _, p := range item.Prompts {
				field := agent.Field{Key: p.Key, Label: p.Message, Placeholder: p.Placeholder, Type: agent.FieldText}
				if p.When != nil {
					conditions[p.Key] = *p.When
					if p.When.Op == "eq" {
						field.VisibleWhen = &agent.FieldCondition{Key: p.When.Key, Equals: p.When.Value}
					}
				}
				if p.Type == "select" {
					field.Type = agent.FieldSelect
					for _, o := range p.Options {
						field.Options = append(field.Options, agent.Option{Value: o.Value, Label: o.Label})
					}
				}
				method.Fields = append(method.Fields, field)
			}
			methods[id] = nativeLoginMethod{Provider: provider, Index: i, Method: method, Conditions: conditions}
		}
	}
	m.mu.Lock()
	m.nativeMethods = methods
	m.mu.Unlock()
	out := m.Methods()
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *authModule) beginNative(ctx context.Context, id string) (agent.AuthState, error) {
	// Refresh before using an index so a provider plugin update cannot select a
	// different method using a stale catalog entry.
	if _, err := m.DiscoverMethods(ctx); err != nil {
		return agent.AuthState{}, err
	}
	m.mu.Lock()
	if m.login != nil && m.login.Active() {
		flow := m.login
		m.mu.Unlock()
		return flow.InitialState(ctx), nil
	}
	method, ok := m.nativeMethods[id]
	if !ok {
		m.mu.Unlock()
		return agent.AuthState{}, errors.New("native sign-in method is no longer available")
	}
	flow := agent.NewAuthFlow(id)
	m.login = flow
	m.loginDone = make(chan struct{})
	done := m.loginDone
	m.loginInputs = make(chan map[string]string, 1)
	inputs := m.loginInputs
	m.loginFields = method.Method.Fields
	m.mu.Unlock()
	go func() { defer close(done); m.runNativeLogin(flow, method, inputs) }()
	return flow.InitialState(ctx), nil
}

func (m *authModule) runNativeLogin(flow *agent.AuthFlow, method nativeLoginMethod, inputs <-chan map[string]string) {
	values := map[string]string{}
	if len(method.Method.Fields) > 0 {
		flow.Update(agent.AuthState{Status: "needs_input", Fields: method.Method.Fields, Message: "Complete the provider's sign-in options."})
		select {
		case values = <-inputs:
		case <-flow.Context().Done():
			return
		}
	}
	for key, when := range method.Conditions {
		if when.Op == "eq" && values[when.Key] != when.Value || when.Op == "neq" && values[when.Key] == when.Value {
			delete(values, key)
		}
	}
	transport, err := openAuthServer(flow.Context())
	if err != nil {
		flow.Fail("Could not start OpenCode sign-in.")
		return
	}
	defer transport.close()
	path := "/provider/" + url.PathEscape(method.Provider) + "/oauth/"
	var authorization struct{ URL, Method, Instructions string }
	if err = transport.call(flow.Context(), http.MethodPost, path+"authorize", map[string]any{"method": method.Index, "inputs": values}, &authorization); err != nil {
		flow.Fail("OpenCode could not start this provider's sign-in.")
		return
	}
	u, err := url.Parse(authorization.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		flow.Fail("OpenCode returned an invalid sign-in URL.")
		return
	}
	state := agent.AuthState{Status: "pending", URL: u.String(), Message: authorization.Instructions}
	body := map[string]any{"method": method.Index}
	if authorization.Method == "code" {
		codes := make(chan string, 1)
		flow.SetInput(func(code string) error {
			select {
			case codes <- code:
				return nil
			case <-flow.Context().Done():
				return flow.Context().Err()
			}
		})
		state.Status = "needs_input"
		state.Fields = []agent.Field{{Key: agent.AuthCodeKey, Label: "Authorization code", Type: agent.FieldSecret, Required: true}}
		flow.Update(state)
		select {
		case code := <-codes:
			body["code"] = code
		case <-flow.Context().Done():
			return
		}
	} else if authorization.Method == "auto" {
		flow.Update(state)
		// The native provider supplies the URL; bind a relay only to its exact
		// loopback redirect/state for remote product clients.
		nativeauth.CaptureURL(flow, u.String(), u.Hostname())
	} else {
		flow.Fail("This native provider uses an unsupported sign-in handoff.")
		return
	}
	var success bool
	if err = transport.call(flow.Context(), http.MethodPost, path+"callback", body, &success); err != nil || !success {
		flow.Fail("Provider sign-in did not complete. Please try again.")
		return
	}
	flow.Complete(func() error {
		if err := m.store.Set("oauthProvider", method.Provider); err != nil {
			return err
		}
		if err := m.store.Set(ckAPIKey, ""); err != nil {
			return err
		}
		return m.store.Set("authMethod", method.Method.ID)
	})
}

func (m *authModule) stepNative(input map[string]string) (agent.AuthState, error) {
	m.mu.Lock()
	flow, fields, inputs := m.login, m.loginFields, m.loginInputs
	m.mu.Unlock()
	if flow == nil {
		return agent.AuthState{}, errors.New("no sign-in in progress")
	}
	if input[agent.AuthFlowIDKey] != flow.State().FlowID {
		return agent.AuthState{}, errors.New("sign-in flow ID required")
	}
	if input[agent.AuthActionKey] != "" || input[agent.AuthCodeKey] != "" || len(fields) == 0 {
		return flow.Step(input)
	}
	provided := false
	for _, field := range fields {
		_, ok := input[field.Key]
		provided = provided || ok
	}
	if !provided {
		return flow.Step(input)
	}
	values, err := agent.NormalizeAuthInput(fields, input)
	if err != nil {
		return agent.AuthState{}, err
	}
	select {
	case inputs <- values:
	case <-flow.Context().Done():
		return flow.State(), nil
	default:
		return agent.AuthState{}, errors.New("sign-in options already submitted")
	}
	m.mu.Lock()
	m.loginFields = nil
	m.mu.Unlock()
	flow.Update(agent.AuthState{Status: "pending", Message: "Starting provider sign-in…"})
	return flow.State(), nil
}
func (m *authModule) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.login != nil && m.login.Active()
}
func (m *authModule) Callback(_ context.Context, id, raw string) (agent.AuthState, error) {
	m.mu.Lock()
	f := m.login
	m.mu.Unlock()
	if f == nil {
		return agent.AuthState{}, errors.New("no sign-in in progress")
	}
	return f.Step(map[string]string{agent.AuthFlowIDKey: id, agent.AuthCallbackKey: raw})
}
func (m *authModule) Logout(ctx context.Context) error {
	m.mu.Lock()
	if m.login != nil {
		m.login.Cancel()
	}
	done := m.loginDone
	m.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if !strings.HasPrefix(m.store.Get("authMethod"), "oauth:") {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	transport, err := openAuthServer(ctx)
	if err != nil {
		return err
	}
	defer transport.close()
	return transport.call(ctx, http.MethodDelete, "/auth/"+url.PathEscape(m.store.Get("oauthProvider")), nil, nil)
}
func nativeAuthPresent(provider string) bool {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	data, err := agent.ReadNativeAuthFile(filepath.Join(base, "opencode", "auth.json"))
	if err != nil {
		return false
	}
	var entries map[string]struct{ Type, Access, Refresh string }
	if json.Unmarshal(data, &entries) != nil {
		return false
	}
	v := entries[provider]
	return v.Type == "oauth" && (v.Access != "" || v.Refresh != "")
}
