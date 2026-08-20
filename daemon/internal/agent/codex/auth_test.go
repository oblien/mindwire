package codex

import (
	"context"
	"testing"
)

// mapStore is an in-memory agent.CredStore for exercising the auth module.
type mapStore map[string]string

func (s mapStore) Get(k string) string    { return s[k] }
func (s mapStore) Set(k, v string) error  { s[k] = v; return nil }
func (s mapStore) All() map[string]string { return s }

// A full API-key setup persists the credential + optional companions, tags the method, and EnvForRun
// then exports CODEX_API_KEY (+ OPENAI_API_KEY) alongside the endpoint/org/project vars.
func TestStepAPIKeyAndEnv(t *testing.T) {
	store := mapStore{}
	m := newAuth(store)

	st, err := m.Step(context.Background(), map[string]string{
		ckAPIKey:  "sk-abc",
		ckBaseURL: "https://api.example/v1",
		ckOrg:     "org-1",
		ckProject: "proj-1",
	})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if st.Method != "apiKey" || st.Status != "complete" {
		t.Fatalf("Step state = %+v, want apiKey/complete", st)
	}
	if store[ckMethod] != "apiKey" {
		t.Errorf("authMethod = %q, want apiKey", store[ckMethod])
	}

	env := m.EnvForRun()
	want := map[string]string{
		"CODEX_API_KEY":       "sk-abc",
		"OPENAI_API_KEY":      "sk-abc",
		"OPENAI_BASE_URL":     "https://api.example/v1",
		"OPENAI_ORGANIZATION": "org-1",
		"OPENAI_PROJECT":      "proj-1",
	}
	assertEnv(t, env, want)
}

// An access-token setup exports only CODEX_ACCESS_TOKEN.
func TestStepAccessTokenAndEnv(t *testing.T) {
	store := mapStore{}
	m := newAuth(store)
	if _, err := m.Step(context.Background(), map[string]string{ckAccessToken: "tok-1"}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	assertEnv(t, m.EnvForRun(), map[string]string{"CODEX_ACCESS_TOKEN": "tok-1"})
}

// Switching methods clears the other credential so a stale secret can't leak into a run.
func TestStepSwitchClearsOtherCredential(t *testing.T) {
	store := mapStore{}
	m := newAuth(store)
	if _, err := m.Step(context.Background(), map[string]string{ckAPIKey: "sk-old"}); err != nil {
		t.Fatalf("Step apiKey: %v", err)
	}
	if _, err := m.Step(context.Background(), map[string]string{ckAccessToken: "tok-new"}); err != nil {
		t.Fatalf("Step accessToken: %v", err)
	}
	env := m.EnvForRun()
	if _, leaked := env["CODEX_API_KEY"]; leaked {
		t.Errorf("stale CODEX_API_KEY leaked after switching to access token: %v", env)
	}
	assertEnv(t, env, map[string]string{"CODEX_ACCESS_TOKEN": "tok-new"})
}

// Optional companions left blank are not exported (env stays minimal).
func TestEnvForRunOmitsBlankCompanions(t *testing.T) {
	m := newAuth(mapStore{ckMethod: "apiKey", ckAPIKey: "sk-x"})
	assertEnv(t, m.EnvForRun(), map[string]string{"CODEX_API_KEY": "sk-x", "OPENAI_API_KEY": "sk-x"})
}

// With no method tag, EnvForRun falls back to whichever secret is present so a run still authenticates.
func TestEnvForRunPresenceFallback(t *testing.T) {
	assertEnv(t, newAuth(mapStore{ckAPIKey: "sk-y"}).EnvForRun(),
		map[string]string{"CODEX_API_KEY": "sk-y", "OPENAI_API_KEY": "sk-y"})
	assertEnv(t, newAuth(mapStore{ckAccessToken: "tok-z"}).EnvForRun(),
		map[string]string{"CODEX_ACCESS_TOKEN": "tok-z"})
}

// Status is presence-based (env-only creds don't show in `codex login status`).
func TestStatus(t *testing.T) {
	if got := newAuth(mapStore{ckAPIKey: "sk"}).Status(context.Background()); !got.Configured || got.Method != "apiKey" {
		t.Errorf("api key status = %+v, want configured/apiKey", got)
	}
	if got := newAuth(mapStore{ckAccessToken: "t"}).Status(context.Background()); !got.Configured || got.Method != "accessToken" {
		t.Errorf("access token status = %+v, want configured/accessToken", got)
	}
	if got := newAuth(mapStore{}).Status(context.Background()); got.Configured {
		t.Errorf("empty store status = %+v, want not configured", got)
	}
}

// An empty step is an error, not a silent success.
func TestStepNoCredentialErrors(t *testing.T) {
	st, _ := newAuth(mapStore{}).Step(context.Background(), map[string]string{})
	if st.Status != "error" {
		t.Errorf("empty Step status = %q, want error", st.Status)
	}
}

func assertEnv(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("EnvForRun = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("EnvForRun[%q] = %q, want %q", k, got[k], v)
		}
	}
}
