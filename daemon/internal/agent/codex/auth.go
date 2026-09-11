package codex

import (
	"context"
	"errors"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// auth.go is Codex's AuthModule. Codex authenticates a headless run entirely from the process
// environment — new secrets are never written to a config file,
// which is exactly the security posture mindwire wants: secrets enter a run ONLY through EnvForRun,
// never via TurnInput.Config or the shell string.
//
// Three field-based methods (all non-interactive — the ChatGPT `localhost:1455` OAuth loopback is not
// headless-compatible, so it is deliberately not offered):
//   - "apiKey":      an OpenAI/Codex API key → CODEX_API_KEY (+ OPENAI_API_KEY for pre-Rust builds),
//                    with optional base URL / organization / project.
//   - "accessToken": a ChatGPT/PAT access token → CODEX_ACCESS_TOKEN.
//   - "azureFoundry": Microsoft Foundry/Azure OpenAI v1 via native Codex provider configuration;
//     the API key or Entra token still enters the process through an env-var reference only.
//
// Status is presence-based: daemon credentials do not appear in `codex login
// status`. An existing selected native provider can also authenticate a run;
// recognize its bearer token or populated env_key without copying the secret.

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

type authModule struct{ store agent.CredStore }

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

func (m *authModule) Begin(_ context.Context, methodID string) (agent.AuthState, error) {
	switch methodID {
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

// Status reports credential presence in the daemon or the selected native provider.
// This does not make an inference request or verify that a credential is unexpired.
func (m *authModule) Status(_ context.Context) agent.AuthStatus {
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
		return agent.AuthStatus{Configured: false, Detail: "No credential set — connect an account or configure a provider in Codex config."}
	}
}

// EnvForRun is the ONLY place credentials enter a run. Endpoint/org/project are orthogonal to the
// credential and exported whenever set; the credential itself is chosen by the stored method, falling
// back to whichever secret is present so a run still authenticates if the method tag was lost.
func (m *authModule) EnvForRun() map[string]string {
	env := map[string]string{}
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
