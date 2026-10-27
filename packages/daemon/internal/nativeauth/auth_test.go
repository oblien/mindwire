package nativeauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

type store struct {
	mu   sync.Mutex
	data map[string]string
}

func (s *store) Get(k string) string   { s.mu.Lock(); defer s.mu.Unlock(); return s.data[k] }
func (s *store) Set(k, v string) error { s.mu.Lock(); defer s.mu.Unlock(); s.data[k] = v; return nil }
func (s *store) All() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

func TestCanceledNativeLoginCannotCommitAndLogoutRotatesScope(t *testing.T) {
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", t.TempDir()+"/toolchains")
	s := &store{data: map[string]string{}}
	release := make(chan struct{})
	a := New(Config{ID: "fixture", Name: "Fixture", KeyEnv: "FIXTURE_API_KEY", Logins: []agent.AuthMethod{{ID: "login"}}, Environment: func(home string) map[string]string { return map[string]string{"FIXTURE_HOME": home} }, Login: func(f *agent.AuthFlow, env map[string]string) error {
		if !strings.Contains(env["FIXTURE_HOME"], "/auth/fixture/") {
			t.Error("credentials not isolated")
		}
		f.Update(agent.AuthState{Status: "pending", URL: "https://example.test/login"})
		<-release
		return nil
	}}, s)
	state, err := a.Begin(t.Context(), "login")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Step(t.Context(), map[string]string{agent.AuthFlowIDKey: state.FlowID, agent.AuthActionKey: "cancel"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-a.done
	if s.Get("authMethod") != "" {
		t.Fatal("canceled login committed")
	}
	if _, err = a.Step(t.Context(), map[string]string{"apiKey": "secret"}); err != nil {
		t.Fatal(err)
	}
	if a.EnvForRun()["FIXTURE_API_KEY"] != "secret" {
		t.Fatal("key not supplied to run")
	}
	before := a.EnvForRun()["FIXTURE_HOME"]
	managed := agent.ManageAuth(a, s)
	if err = managed.Logout(t.Context()); err != nil {
		t.Fatal(err)
	}
	if a.EnvForRun()["FIXTURE_HOME"] == before || managed.Status(t.Context()).Configured || s.Get("apiKey") != "" {
		t.Fatal("logout retained selected account")
	}
}

func TestOAuthCallbackIsBoundToThisFlow(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Store(true); w.WriteHeader(200) }))
	defer server.Close()
	f := agent.NewAuthFlow("login")
	defer f.Cancel()
	authorize := "https://accounts.google.com/auth?state=expected&redirect_uri=" + url.QueryEscape(server.URL+"/callback")
	CaptureURL(f, "Open "+authorize, "accounts.google.com")
	state := f.State()
	if state.URL == "" || state.CallbackURL == "" {
		t.Fatal("missing callback metadata")
	}
	for _, raw := range []string{server.URL + "/other?state=expected&code=x", server.URL + "/callback?state=wrong&code=x", "http://127.0.0.1:9/callback?state=expected&code=x"} {
		if _, err := f.Step(map[string]string{agent.AuthFlowIDKey: state.FlowID, agent.AuthCallbackKey: raw}); err == nil {
			t.Fatal("unrelated callback accepted")
		}
	}
	if called.Load() {
		t.Fatal("invalid callback reached listener")
	}
	if _, err := f.Step(map[string]string{agent.AuthFlowIDKey: state.FlowID, agent.AuthCallbackKey: server.URL + "/callback?state=expected&code=x"}); err != nil {
		t.Fatal(err)
	}
	if !called.Load() {
		t.Fatal("valid callback was not delivered")
	}
}

func TestPrepareAuthRequiresConnection(t *testing.T) {
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", t.TempDir()+"/toolchains")
	a := New(Config{ID: "fixture", Name: "Fixture", Environment: func(home string) map[string]string { return map[string]string{"HOME_FIXTURE": home} }}, &store{data: map[string]string{}})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := agent.ManageAuth(a, a.store).PrepareAuth(ctx); err == nil {
		t.Fatal("native session admitted before connection")
	}
}

func TestNativeDeviceHandoffRetainsSplitURLAndCode(t *testing.T) {
	f := agent.NewAuthFlow("login")
	defer f.Cancel()
	w := &loginOutput{flow: f, hosts: []string{"auth.x.ai"}}
	for _, chunk := range []string{"Open https://auth.x.ai/", "device\nThen enter this code:\n", "ABCD-", "EFGH\n"} {
		w.Write([]byte(chunk))
	}
	s := f.State()
	if s.URL != "https://auth.x.ai/device" || s.Code != "ABCD-EFGH" {
		t.Fatalf("lost handoff: %+v", s)
	}
	CaptureURL(f, "https://untrusted.test/?user_code=XXXX-YYYY", "auth.x.ai")
	if f.State().URL != s.URL {
		t.Fatal("untrusted handoff accepted")
	}
}
