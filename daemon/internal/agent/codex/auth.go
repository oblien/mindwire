package codex

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Subscription sign-in uses native app-server device auth: Codex owns the OAuth
// cache and refresh tokens. Field credentials enter runs only through EnvForRun.

// Cred-store keys.
const (
	ckMethod            = "authMethod"
	ckAPIKey            = "apiKey"
	ckAccessToken       = "accessToken"
	ckBaseURL           = "baseUrl"
	ckOrg               = "org"
	ckProject           = "project"
	ckAzureResource     = "azureResource"
	ckAzureBaseURL      = "azureBaseUrl"
	ckAzureAPIKey       = "azureApiKey"
	ckAzureToken        = "azureAuthToken"
	ckAzureModel        = "azureModel"
	ckAzureEndpointType = "azureEndpointType"
	ckAzureAuthType     = "azureAuthType"
	azureProviderID     = "azure-foundry"
	azureProviderMarker = "MINDWIRE_CODEX_MODEL_PROVIDER"
	azureModelMarker    = "MINDWIRE_CODEX_MODEL"
)

type authModule struct {
	store     agent.CredStore
	mu        sync.Mutex
	login     *agent.AuthFlow
	loginDone <-chan struct{}
}

var azureAuthSpec = agent.FoundryAuthSpec{
	Prefix: "azure", ResourceDomain: "openai.azure.com", APIPath: "/openai/v1", ModelPlaceholder: "my-codex-deployment",
}

func foundryAuthMethod() agent.AuthMethod {
	return agent.AuthMethod{
		ID: "azureFoundry", Label: "Microsoft Foundry", Scope: agent.ScopeCustom,
		Help:   "Use a Codex deployment hosted in Microsoft Foundry.",
		Fields: azureAuthSpec.Fields(), Sections: azureAuthSpec.Sections(),
	}
}

func newAuth(store agent.CredStore) *authModule { return &authModule{store: store} }

func (m *authModule) Methods() []agent.AuthMethod {
	return []agent.AuthMethod{
		{
			ID: "login", Label: "Continue with ChatGPT", Scope: agent.ScopeUnified, Interactive: true,
			Help: "Use the Codex access included with your ChatGPT plan. Open the sign-in page and enter the device code. If prompted, enable Codex device-code sign-in in your ChatGPT security settings.",
		},
		{
			ID: "apiKey", Label: "API key", Scope: agent.ScopeUnified,
			Help: "An OpenAI/Codex API key (platform.openai.com). Sent to a run as CODEX_API_KEY — env-only, no config file is written.",
			Fields: []agent.Field{
				{Key: ckAPIKey, Label: "API key", Type: agent.FieldSecret, Required: true, Placeholder: "sk-…",
					Help: "Exported as CODEX_API_KEY (and OPENAI_API_KEY for older CLI builds)."},
				{Key: ckBaseURL, Label: "Base URL", Type: agent.FieldText, Placeholder: "https://api.openai.com/v1",
					Help: "Custom endpoint (OPENAI_BASE_URL). Leave blank for the default."},
				{Key: ckOrg, Label: "Organization", Type: agent.FieldText, Placeholder: "org-…",
					Help: "Optional OpenAI organization (OPENAI_ORGANIZATION)."},
				{Key: ckProject, Label: "Project", Type: agent.FieldText, Placeholder: "proj_…",
					Help: "Optional OpenAI project (OPENAI_PROJECT)."},
			},
		},
		{
			ID: "accessToken", Label: "Access token", Scope: agent.ScopeUnified,
			Help: "A ChatGPT/personal access token. Sent to a run as CODEX_ACCESS_TOKEN — env-only.",
			Fields: []agent.Field{
				{Key: ckAccessToken, Label: "Access token", Type: agent.FieldSecret, Required: true, Placeholder: "…",
					Help: "Exported as CODEX_ACCESS_TOKEN."},
			},
		},
		foundryAuthMethod(),
	}
}

func (m *authModule) Begin(ctx context.Context, methodID string) (agent.AuthState, error) {
	switch methodID {
	case "login":
		return m.beginLogin(ctx), nil
	case "apiKey":
		return agent.AuthState{Method: "apiKey", Status: "needs_input",
			Fields: agent.MethodFields(m, "apiKey"), Message: "Enter your OpenAI/Codex API key."}, nil
	case "accessToken":
		return agent.AuthState{Method: "accessToken", Status: "needs_input",
			Fields: agent.MethodFields(m, "accessToken"), Message: "Enter your Codex access token."}, nil
	case "azureFoundry":
		method := foundryAuthMethod()
		return agent.AuthState{Method: "azureFoundry", Status: "needs_input",
			Fields: method.Fields, Sections: method.Sections}, nil
	default:
		return agent.AuthState{}, errors.New("unknown auth method: " + methodID)
	}
}

func (m *authModule) Step(_ context.Context, input map[string]string) (agent.AuthState, error) {
	if len(input) == 0 || input[agent.AuthFlowIDKey] != "" || input[agent.AuthActionKey] != "" {
		m.mu.Lock()
		flow := m.login
		m.mu.Unlock()
		if flow != nil {
			return flow.Step(input)
		}
		return agent.AuthState{Method: "login", Status: "error", Message: "No sign-in in progress. Start again."}, nil
	}
	// Choosing another connection supersedes any unfinished subscription login.
	m.cancelLogin()
	if hasAzureInput(input) {
		resolved, err := azureAuthSpec.NormalizeInput(input)
		if err != nil {
			return agent.AuthState{Method: "azureFoundry", Status: "error", Message: err.Error(),
				Fields: azureAuthSpec.Fields(), Sections: azureAuthSpec.Sections()}, nil
		}
		input = resolved
		baseURL, err := azureBaseURL(input)
		if err != nil {
			return agent.AuthState{Method: "azureFoundry", Status: "error", Message: err.Error()}, nil
		}
		credential := strings.TrimSpace(input[ckAzureToken])
		envVar := "AZURE_OPENAI_ACCESS_TOKEN"
		if credential == "" {
			credential = strings.TrimSpace(input[ckAzureAPIKey])
			envVar = "AZURE_OPENAI_API_KEY"
		}
		if credential == "" {
			return agent.AuthState{Method: "azureFoundry", Status: "error", Message: "enter an Azure API key or Entra ID token"}, nil
		}
		model := strings.TrimSpace(input[ckAzureModel])
		if model == "" {
			return agent.AuthState{Method: "azureFoundry", Status: "error", Message: "enter the exact Azure deployment name"}, nil
		}
		models := []string{model}
		provider := agent.CustomProvider{ID: azureProviderID, Name: "Microsoft Foundry", BaseURL: baseURL, Models: models, EnvVar: envVar}
		if err := (adapter{}).SetProvider(m.store, agent.MemoryUser, "", azureProviderID, provider, credential, nil); err != nil {
			return agent.AuthState{}, err
		}
		for _, key := range []string{ckAPIKey, ckAccessToken, ckBaseURL, ckOrg, ckProject} {
			if err := m.store.Set(key, ""); err != nil {
				return agent.AuthState{}, err
			}
		}
		if err := m.store.Set(ckMethod, "azureFoundry"); err != nil {
			return agent.AuthState{}, err
		}
		return agent.AuthState{Method: "azureFoundry", Status: "complete"}, nil
	}
	if key := strings.TrimSpace(input[ckAPIKey]); key != "" {
		if err := m.store.Set(ckAPIKey, key); err != nil {
			return agent.AuthState{}, err
		}
		// Optional non-secret companions; a trimmed empty value clears any prior setting.
		for _, k := range []string{ckBaseURL, ckOrg, ckProject} {
			if err := m.store.Set(k, strings.TrimSpace(input[k])); err != nil {
				return agent.AuthState{}, err
			}
		}
		_ = m.store.Set(ckAccessToken, "") // switching methods clears the other credential
		_ = m.store.Set(ckMethod, "apiKey")
		return agent.AuthState{Method: "apiKey", Status: "complete"}, nil
	}
	if tok := strings.TrimSpace(input[ckAccessToken]); tok != "" {
		if err := m.store.Set(ckAccessToken, tok); err != nil {
			return agent.AuthState{}, err
		}
		_ = m.store.Set(ckAPIKey, "")
		_ = m.store.Set(ckMethod, "accessToken")
		return agent.AuthState{Method: "accessToken", Status: "complete"}, nil
	}
	return agent.AuthState{Status: "error", Message: "no credential provided"}, nil
}

// Status uses native account/read for subscription accounts and credential
// presence for field-based providers. It never makes an inference request.
func (m *authModule) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.login != nil && m.login.Active()
}

func (m *authModule) Status(ctx context.Context) agent.AuthStatus {
	if m.store.Get(ckMethod) == "login" {
		return nativeSubscriptionStatus(ctx)
	}
	switch {
	case m.store.Get(ckMethod) == "azureFoundry" && strings.TrimSpace(m.store.Get(agent.ProviderCredKey(azureProviderID))) != "":
		return agent.AuthStatus{Configured: true, Method: "azureFoundry", Detail: "Microsoft Foundry connected"}
	case strings.TrimSpace(m.store.Get(ckAPIKey)) != "":
		return agent.AuthStatus{Configured: true, Method: "apiKey", Detail: "API key set"}
	case strings.TrimSpace(m.store.Get(ckAccessToken)) != "":
		return agent.AuthStatus{Configured: true, Method: "accessToken", Detail: "Access token set"}
	default:
		if p, model := nativeProvider(); p.ID != "" && p.BaseURL != "" && p.HasKey && model != "" {
			method := "configFile"
			if strings.Contains(p.BaseURL, ".azure.com/openai/v1") {
				method = "azureFoundry"
			}
			name := agent.FirstNonEmpty(p.Name, p.ID)
			return agent.AuthStatus{Configured: true, Method: method, Detail: "Using " + name + " from Codex config"}
		}
		return nativeSubscriptionStatus(ctx)
	}
}

// EnvForRun supplies stored field credentials, or selects the native account for
// subscription runs. Old stores without a method tag keep their presence fallback.
func (m *authModule) EnvForRun() map[string]string {
	env := map[string]string{}
	if m.store.Get(ckMethod) == "login" {
		// Explicit subscription selection must not inherit a previous Azure/API
		// endpoint or its credentials. Native Codex owns account refresh.
		env[azureProviderMarker] = "openai"
		return env
	}
	// Older Foundry setups may still have first-party companions in the store.
	// They do not apply to the Azure provider, including after a daemon upgrade.
	if m.store.Get(ckMethod) != "azureFoundry" {
		if v := strings.TrimSpace(m.store.Get(ckBaseURL)); v != "" {
			env["OPENAI_BASE_URL"] = v
		}
		if v := strings.TrimSpace(m.store.Get(ckOrg)); v != "" {
			env["OPENAI_ORGANIZATION"] = v
		}
		if v := strings.TrimSpace(m.store.Get(ckProject)); v != "" {
			env["OPENAI_PROJECT"] = v
		}
	}

	apiKey := strings.TrimSpace(m.store.Get(ckAPIKey))
	accessToken := strings.TrimSpace(m.store.Get(ckAccessToken))
	method := m.store.Get(ckMethod)
	if method == "" { // presence fallback if the method tag is missing
		switch {
		case apiKey != "":
			method = "apiKey"
		case accessToken != "":
			method = "accessToken"
		}
	}
	switch method {
	case "apiKey":
		if apiKey != "" {
			env["CODEX_API_KEY"] = apiKey  // exec-only, highest precedence
			env["OPENAI_API_KEY"] = apiKey // also set for pre-Rust CLI builds
		}
	case "accessToken":
		if accessToken != "" {
			env["CODEX_ACCESS_TOKEN"] = accessToken
		}
	case "azureFoundry":
		env[azureProviderMarker] = azureProviderID
		if models := storedModels(m.store, azureProviderID); len(models) > 0 {
			env[azureModelMarker] = models[0]
		}
	}
	// Every registered CUSTOM provider's stored key, exported under the env-var name it was registered
	// with (agent.ProviderEnvForRun) so config.toml's `[model_providers.<id>] env_key = "VAR"` resolves
	// at runtime — the config file never carries a literal secret.
	for name, key := range agent.ProviderEnvForRun(m.store) {
		env[name] = key
	}
	return env
}

func hasAzureInput(input map[string]string) bool {
	for _, key := range []string{ckAzureResource, ckAzureBaseURL, ckAzureAPIKey, ckAzureToken, ckAzureModel, ckAzureEndpointType, ckAzureAuthType} {
		if _, ok := input[key]; ok {
			return true
		}
	}
	return false
}

func azureBaseURL(input map[string]string) (string, error) {
	return agent.FoundryBaseURL(input[ckAzureBaseURL], input[ckAzureResource], "openai.azure.com", "/openai/v1")
}
