package agent

import "strings"

// FoundryAuthSpec declares the entire form in the daemon, shared by adapters
// while keeping their endpoints and supported authentication methods distinct.
type FoundryAuthSpec struct {
	Prefix                string
	ResourceDomain        string
	APIPath               string
	ModelPlaceholder      string
	AllowAzureCredentials bool
}

func (s FoundryAuthSpec) Fields() []Field {
	p := s.Prefix
	when := func(key, value string) *FieldCondition { return &FieldCondition{Key: p + key, Equals: value} }
	authOptions := []Option{{Value: "apiKey", Label: "API key"}, {Value: "entraToken", Label: "Entra ID"}}
	if s.AllowAzureCredentials {
		authOptions = append(authOptions, Option{Value: "azureCredentials", Label: "Azure sign-in",
			Help: "Uses Azure sign-in or a managed identity already configured in this workspace."})
	}
	fields := []Field{
		{Key: p + "EndpointType", Label: "Endpoint", Type: FieldSelect, Required: true, Default: "url", Presentation: "segmented",
			Options: []Option{{Value: "url", Label: "URL"}, {Value: "resource", Label: "Resource name"}}},
		{Key: p + "BaseUrl", Label: "Endpoint URL", Type: FieldText, Required: true, VisibleWhen: when("EndpointType", "url"), InputMode: "url",
			Placeholder: "Paste endpoint URL",
			Help:        "Copy your Foundry endpoint. The " + s.APIPath + " path is added if needed."},
		{Key: p + "Resource", Label: "Resource name", Type: FieldText, Required: true, VisibleWhen: when("EndpointType", "resource"),
			Placeholder: "my-resource", Help: "The resource name before ." + s.ResourceDomain + " in your endpoint."},
		{Key: p + "AuthType", Label: "Authentication", Type: FieldSelect, Required: true, Default: "apiKey", Presentation: "segmented", Options: authOptions},
		{Key: p + "ApiKey", Label: "API key", Type: FieldSecret, Required: true, VisibleWhen: when("AuthType", "apiKey"),
			Placeholder: "Paste API key", Help: "Find this under Endpoints and keys in Foundry."},
		{Key: p + "AuthToken", Label: "Entra access token", Type: FieldSecret, Required: true, VisibleWhen: when("AuthType", "entraToken"),
			Placeholder: "Paste access token", Help: "Use a valid Microsoft Entra access token."},
		{Key: p + "Model", Label: "Deployment name", Type: FieldText, Required: true,
			Placeholder: s.ModelPlaceholder, Help: "The exact deployment name in Foundry."},
	}
	for i := range fields {
		fields[i].Scope, fields[i].Canon = ScopeCustom, fields[i].Key
	}
	return fields
}

func (s FoundryAuthSpec) Sections() []AuthSection {
	p := s.Prefix
	return []AuthSection{
		{ID: "connection", Title: "Connection details", FieldKeys: []string{p + "EndpointType", p + "BaseUrl", p + "Resource"}},
		{ID: "authentication", Title: "Authentication", FieldKeys: []string{p + "AuthType", p + "ApiKey", p + "AuthToken"}},
		{ID: "deployment", Title: "Deployment name", FieldKeys: []string{p + "Model"}},
	}
}

// NormalizeInput honors explicit choices. Requests from older clients have no
// selectors, so retain their URL/token precedence and Claude's Azure sign-in path.
func (s FoundryAuthSpec) NormalizeInput(input map[string]string) (map[string]string, error) {
	values := make(map[string]string, len(input)+2)
	for key, value := range input {
		values[key] = value
	}
	p := s.Prefix
	if _, provided := values[p+"EndpointType"]; !provided {
		values[p+"EndpointType"] = "url"
		if strings.TrimSpace(values[p+"BaseUrl"]) == "" && strings.TrimSpace(values[p+"Resource"]) != "" {
			values[p+"EndpointType"] = "resource"
		}
	}
	if _, provided := values[p+"AuthType"]; !provided {
		switch {
		case strings.TrimSpace(values[p+"AuthToken"]) != "":
			values[p+"AuthType"] = "entraToken"
		case s.AllowAzureCredentials && strings.TrimSpace(values[p+"ApiKey"]) == "":
			values[p+"AuthType"] = "azureCredentials"
		default:
			values[p+"AuthType"] = "apiKey"
		}
	}
	return NormalizeAuthInput(s.Fields(), values)
}
