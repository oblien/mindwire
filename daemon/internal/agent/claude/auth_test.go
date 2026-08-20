package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestAuthStatusArgs pins the exact `claude` invocation used to check auth: the CLI's own
// `auth status --json`. This is the single source of truth we test against.
func TestAuthStatusArgs(t *testing.T) {
	got := strings.Join(authStatusArgs(), " ")
	want := "auth status --json"
	if got != want {
		t.Errorf("authStatusArgs = %q, want %q", got, want)
	}
}

// TestParseAuthStatus covers decoding of the `claude auth status --json` payload and the
// derived UI labels — the CLI's verdict is authoritative, we only read it.
func TestParseAuthStatus(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantLogged bool
		wantMethod string
		wantDetail string
		wantErr    bool
	}{
		{
			name:       "api key first party",
			in:         `{"loggedIn":true,"authMethod":"api_key","apiProvider":"firstParty","apiKeySource":"ANTHROPIC_API_KEY"}`,
			wantLogged: true,
			wantMethod: "apiKey",
			wantDetail: "Signed in",
		},
		{
			name:       "oauth subscription",
			in:         `{"loggedIn":true,"authMethod":"oauth","apiProvider":"firstParty"}`,
			wantLogged: true,
			wantMethod: "login",
			wantDetail: "Signed in",
		},
		{
			name:       "third party provider names the provider",
			in:         `{"loggedIn":true,"authMethod":"api_key","apiProvider":"bedrock"}`,
			wantLogged: true,
			wantMethod: "apiKey",
			wantDetail: "Signed in via bedrock",
		},
		{
			name:       "not logged in",
			in:         `{"loggedIn":false}`,
			wantLogged: false,
		},
		{
			name:    "garbage is an error",
			in:      "not json",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, err := parseAuthStatus([]byte(c.in))
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseAuthStatus(%q) = nil error, want error", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAuthStatus(%q) unexpected error: %v", c.in, err)
			}
			if st.LoggedIn != c.wantLogged {
				t.Errorf("LoggedIn = %v, want %v", st.LoggedIn, c.wantLogged)
			}
			if !c.wantLogged {
				return
			}
			if got := st.methodLabel(); got != c.wantMethod {
				t.Errorf("methodLabel = %q, want %q", got, c.wantMethod)
			}
			if got := st.detail(); got != c.wantDetail {
				t.Errorf("detail = %q, want %q", got, c.wantDetail)
			}
		})
	}
}

// mapStore is an in-memory agent.CredStore for exercising EnvForRun.
type mapStore map[string]string

func (s mapStore) Get(k string) string    { return s[k] }
func (s mapStore) Set(k, v string) error  { s[k] = v; return nil }
func (s mapStore) All() map[string]string { return s }

// TestCloudProviderAuthLane exercises the custom-scope cloud provider lane end to end: a Step with a
// provider's declared fields persists them (secrets included), records the provider as the active
// method, and EnvForRun then routes the CLI to that backend (enable flag + mapped vars) while
// IGNORING any first-party credential — the provider owns the whole env.
func TestCloudProviderAuthLane(t *testing.T) {
	store := mapStore{}
	m := &authModule{store: store}

	st, err := m.Step(context.Background(), map[string]string{
		"bedrockRegion":      "us-west-2",
		"awsAccessKeyId":     "AKIAEXAMPLE",
		"awsSecretAccessKey": "secret",
		// optional fields left blank
	})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if st.Method != "bedrock" || st.Status != "complete" {
		t.Fatalf("Step state = %+v, want bedrock/complete", st)
	}
	if store["authMethod"] != "bedrock" {
		t.Errorf("authMethod = %q, want bedrock", store["authMethod"])
	}

	// A stale first-party key must NOT leak once a cloud provider is the active method.
	store["apiKey"] = "sk-ant-should-be-ignored"

	env := m.EnvForRun()
	want := map[string]string{
		"CLAUDE_CODE_USE_BEDROCK": "1",
		"AWS_REGION":              "us-west-2",
		"AWS_ACCESS_KEY_ID":       "AKIAEXAMPLE",
		"AWS_SECRET_ACCESS_KEY":   "secret",
	}
	if len(env) != len(want) {
		t.Fatalf("EnvForRun = %v, want exactly %v (no first-party creds, no blank optionals)", env, want)
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("EnvForRun[%q] = %q, want %q", k, env[k], v)
		}
	}
	if _, leaked := env["ANTHROPIC_API_KEY"]; leaked {
		t.Error("first-party ANTHROPIC_API_KEY leaked into a cloud-provider run")
	}
}

// TestCloudProviderForInput pins the step-input disambiguation: each provider's declared keys map back
// to exactly that provider, and a non-cloud input matches none (falls through to the login flow).
func TestCloudProviderForInput(t *testing.T) {
	for key, want := range map[string]string{
		"bedrockRegion":    "bedrock",
		"awsProfile":       "bedrock",
		"vertexProjectId":  "vertex",
		"foundryResource":  "foundry",
		"foundryAuthToken": "foundry",
	} {
		if p, ok := cloudProviderForInput(map[string]string{key: "x"}); !ok || p.id != want {
			t.Errorf("cloudProviderForInput(%q) = %q,%v; want %q,true", key, p.id, ok, want)
		}
	}
	if p, ok := cloudProviderForInput(map[string]string{"apiKey": "x"}); ok {
		t.Errorf("cloudProviderForInput(apiKey) matched %q, want no match", p.id)
	}
}

// TestCloudFieldsNotInSettings guards the security invariant: no cloud-provider field (secret or not)
// is a declared SETTING, so none can leak into a turn's Config or be overwritten by a per-turn option
// — they reach the process only via EnvForRun.
func TestCloudFieldsNotInSettings(t *testing.T) {
	keys := agent.SettingsKeys(adapter{}.Settings())
	for _, p := range cloudProviders() {
		for _, cf := range p.fields {
			if keys[cf.Key] {
				t.Errorf("cloud field %q (provider %s) is a declared setting; it must live only in the auth lane", cf.Key, p.id)
			}
		}
	}
}

// TestEnvForRunPrecedence pins the credential precedence (subscription OAuth > gateway bearer token >
// API key) and that baseUrl is exported orthogonally to whichever credential wins.
func TestEnvForRunPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		store mapStore
		want  map[string]string
	}{
		{
			name:  "api key only",
			store: mapStore{"apiKey": "sk-ant-x"},
			want:  map[string]string{"ANTHROPIC_API_KEY": "sk-ant-x"},
		},
		{
			name:  "bearer token beats api key",
			store: mapStore{"apiKey": "sk-ant-x", "bearerToken": "tok"},
			want:  map[string]string{"ANTHROPIC_AUTH_TOKEN": "tok"},
		},
		{
			name:  "oauth beats everything",
			store: mapStore{"apiKey": "sk-ant-x", "bearerToken": "tok", "oauthToken": "oat"},
			want:  map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "oat"},
		},
		{
			name:  "base url applies alongside the winning credential",
			store: mapStore{"bearerToken": "tok", "baseUrl": "https://gw.internal"},
			want:  map[string]string{"ANTHROPIC_AUTH_TOKEN": "tok", "ANTHROPIC_BASE_URL": "https://gw.internal"},
		},
		{
			name:  "base url alone (no credential) still exported",
			store: mapStore{"baseUrl": "https://gw.internal"},
			want:  map[string]string{"ANTHROPIC_BASE_URL": "https://gw.internal"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &authModule{store: c.store}
			got := m.EnvForRun()
			if len(got) != len(c.want) {
				t.Fatalf("EnvForRun = %v, want %v", got, c.want)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("EnvForRun[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
