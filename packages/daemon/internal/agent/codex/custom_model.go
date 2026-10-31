package codex

import (
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"maps"
	"strings"
)

// A registered provider/model is a per-turn route, never a change to native login
// or the default model in config.toml. Unregistered slash-containing model IDs
// retain their ordinary meaning at the current endpoint.
func registeredModel(in agent.TurnInput) agent.TurnInput {
	id, model, found := strings.Cut(in.Config[keyModel], "/")
	if !found || model == "" {
		return in
	}
	content, err := readConfig(mcpConfigPath())
	if err != nil {
		return in
	}
	provider, exists := parseModelProviders(content)[id]
	if !exists || provider.BaseURL == "" {
		return in
	}
	in.Config = maps.Clone(in.Config)
	in.Env = maps.Clone(in.Env)
	if in.Env == nil {
		in.Env = map[string]string{}
	}
	in.Config[keyModel] = model
	in.Env[azureProviderMarker] = id
	delete(in.Env, azureModelMarker)
	// First-party credentials must never accompany a custom-provider request.
	for _, key := range []string{"CODEX_API_KEY", "OPENAI_API_KEY", "CODEX_ACCESS_TOKEN", "OPENAI_BASE_URL", "OPENAI_ORGANIZATION", "OPENAI_PROJECT"} {
		if key != provider.EnvVar {
			delete(in.Env, key)
		}
	}
	return in
}
