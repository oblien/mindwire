package claude

import (
	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Cloud providers are Claude's third-party model backends — Amazon Bedrock, Google Vertex AI, and
// Microsoft Foundry. They are the concrete demonstration of the CUSTOM lane in the field taxonomy:
// unlike the API key / login / gateway-bearer methods (which every agent has some form of, so they
// are Scope unified), routing through a cloud provider is agent-specific. Every provider, every input
// field, and every env var it sets is DECLARED HERE — adding one requires a daemon build, never open
// passthrough. That is what "typed & declared custom props" means in practice.
//
// A provider authenticates by setting its enable flag plus a set of env vars, each fed from a declared
// field. The values (including secrets like an AWS secret key) live in the cred store and reach the
// process ONLY through EnvForRun — they are never declared in the settings schema, so they can neither
// leak into a turn's Config nor be overwritten by a per-turn option (the same invariant that protects
// the first-party apiKey).

// cloudProvider is one third-party backend: its stable id, display label, help text, the enable-flag
// env var (always set to "1"), and its declared fields.
type cloudProvider struct {
	id        string
	label     string
	help      string
	enableEnv string // env var that routes the CLI to this backend (value is always "1")
	fields    []cloudField
}

// cloudField is one declared input and the env var it populates on a run. The embedded Field's Key
// doubles as the cred-store key; secret fields are masked and never returned, exactly like apiKey.
type cloudField struct {
	agent.Field
	env string // the process env var this field's value is exported as
}

// cloudProviders is the single source of truth for the custom-scope auth lane. Field keys are
// namespaced per provider (disjoint sets) so a step's input maps unambiguously to one provider.
func cloudProviders() []cloudProvider {
	custom := func(key, label string, typ agent.FieldType, env string, f agent.Field) cloudField {
		f.Key, f.Label, f.Type, f.Scope, f.Canon = key, label, typ, agent.ScopeCustom, key
		return cloudField{Field: f, env: env}
	}
	return []cloudProvider{
		{
			id: "bedrock", label: "Amazon Bedrock", enableEnv: "CLAUDE_CODE_USE_BEDROCK",
			help: "Run Claude through Amazon Bedrock in your own AWS account. Provide static keys, or leave them blank to use an AWS profile / the ambient credential chain.",
			fields: []cloudField{
				custom("bedrockRegion", "AWS region", agent.FieldText, "AWS_REGION", agent.Field{Required: true, Placeholder: "us-east-1"}),
				custom("awsAccessKeyId", "AWS access key ID", agent.FieldSecret, "AWS_ACCESS_KEY_ID", agent.Field{Help: "Leave the key fields blank to use an AWS profile or the ambient credential chain."}),
				custom("awsSecretAccessKey", "AWS secret access key", agent.FieldSecret, "AWS_SECRET_ACCESS_KEY", agent.Field{}),
				custom("awsSessionToken", "AWS session token", agent.FieldSecret, "AWS_SESSION_TOKEN", agent.Field{Help: "Optional — for temporary/STS credentials."}),
				custom("awsProfile", "AWS profile", agent.FieldText, "AWS_PROFILE", agent.Field{Placeholder: "default", Help: "Optional — an AWS config/SSO profile to use instead of static keys."}),
				custom("awsBearerTokenBedrock", "Bedrock API key", agent.FieldSecret, "AWS_BEARER_TOKEN_BEDROCK", agent.Field{Help: "Optional — a Bedrock API key (bearer token) to use instead of AWS SigV4 credentials."}),
				custom("bedrockBaseUrl", "Bedrock base URL", agent.FieldText, "ANTHROPIC_BEDROCK_BASE_URL", agent.Field{Placeholder: "https://bedrock-runtime.us-east-1.amazonaws.com", Help: "Optional — override the Bedrock runtime endpoint (VPC endpoint / gateway)."}),
			},
		},
		{
			id: "vertex", label: "Google Vertex AI", enableEnv: "CLAUDE_CODE_USE_VERTEX",
			help: "Run Claude through Google Cloud Vertex AI. Uses Application Default Credentials, or a service-account key file if you give a path.",
			fields: []cloudField{
				custom("vertexProjectId", "GCP project ID", agent.FieldText, "ANTHROPIC_VERTEX_PROJECT_ID", agent.Field{Required: true, Placeholder: "my-project"}),
				custom("vertexRegion", "Region", agent.FieldText, "CLOUD_ML_REGION", agent.Field{Required: true, Placeholder: "us-east5"}),
				custom("googleAppCredentials", "Service-account key path", agent.FieldText, "GOOGLE_APPLICATION_CREDENTIALS", agent.Field{Placeholder: "/path/to/sa.json", Help: "Optional — path to a service-account JSON. Leave blank to use Application Default Credentials."}),
				custom("vertexBaseUrl", "Vertex base URL", agent.FieldText, "ANTHROPIC_VERTEX_BASE_URL", agent.Field{Placeholder: "https://us-east5-aiplatform.googleapis.com", Help: "Optional — override the Vertex endpoint (regional endpoint / proxy)."}),
			},
		},
		{
			id: "foundry", label: "Microsoft Foundry", enableEnv: "CLAUDE_CODE_USE_FOUNDRY",
			help: "Run Claude through Microsoft Foundry (Azure). Set either the Azure resource name or the full base URL, plus an API key or a Microsoft Entra ID token.",
			fields: []cloudField{
				custom("foundryResource", "Azure resource name", agent.FieldText, "ANTHROPIC_FOUNDRY_RESOURCE", agent.Field{Placeholder: "my-resource", Help: "Set this OR the base URL."}),
				custom("foundryBaseUrl", "Base URL", agent.FieldText, "ANTHROPIC_FOUNDRY_BASE_URL", agent.Field{Placeholder: "https://my-resource.services.ai.azure.com/anthropic", Help: "Set this OR the resource name."}),
				custom("foundryApiKey", "API key", agent.FieldSecret, "ANTHROPIC_FOUNDRY_API_KEY", agent.Field{Help: "Azure API key. Leave blank to use an Entra ID token or the Azure credential chain."}),
				custom("foundryAuthToken", "Entra ID token", agent.FieldSecret, "ANTHROPIC_FOUNDRY_AUTH_TOKEN", agent.Field{Help: "Optional — a Microsoft Entra ID bearer token (takes precedence over the API key)."}),
			},
		},
	}
}

// method renders the provider as a custom-scope AuthMethod for the options list.
func (p cloudProvider) method() agent.AuthMethod {
	fields := make([]agent.Field, len(p.fields))
	for i, cf := range p.fields {
		fields[i] = cf.Field
	}
	return agent.AuthMethod{ID: p.id, Label: p.label, Scope: agent.ScopeCustom, Help: p.help, Fields: fields}
}

// cloudProviderByID returns the provider with that id (the persisted authMethod), ok=false if none.
func cloudProviderByID(id string) (cloudProvider, bool) {
	for _, p := range cloudProviders() {
		if p.id == id {
			return p, true
		}
	}
	return cloudProvider{}, false
}

// cloudProviderForInput picks the provider a step's input belongs to: the first provider that owns any
// key present in the input. Field keys are disjoint across providers, so the match is unambiguous.
func cloudProviderForInput(input map[string]string) (cloudProvider, bool) {
	for _, p := range cloudProviders() {
		for _, cf := range p.fields {
			if _, ok := input[cf.Key]; ok {
				return p, true
			}
		}
	}
	return cloudProvider{}, false
}
