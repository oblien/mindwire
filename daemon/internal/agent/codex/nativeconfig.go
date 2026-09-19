package codex

import (
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// nativeSelection reads the default provider/model, including a selected profile.
// It deliberately reads only routing metadata; credentials stay in config.toml.
func nativeSelection(content string) (provider, model string) {
	values := nativeRootSettings(content)
	return values["model_provider"], values["model"]
}

var nativeSettingKeys = map[string]string{
	"model": keyModel, "model_reasoning_effort": keyEffort, "model_reasoning_summary": keySummary,
	"approvals_reviewer": keyReviewer, "approval_policy": keyApproval, "sandbox_mode": keySandbox,
	"model_auto_compact_token_limit": keyAutoCompact,
}

func nativeRootSettings(content string) map[string]string {
	root := map[string]string{}
	profiles := map[string]map[string]string{}
	current := root
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			current = nil
			if parts, ok := parseTableHeader(line); ok && len(parts) == 2 && parts[0] == "profiles" {
				if profiles[parts[1]] == nil {
					profiles[parts[1]] = map[string]string{}
				}
				current = profiles[parts[1]]
			}
			continue
		}
		if current == nil {
			continue
		}
		if key, value, ok := splitKV(line); ok && (key == "model_provider" || key == "profile" || nativeSettingKeys[key] != "") {
			current[key] = parseTOMLValue(value)
		}
	}
	if profile := profiles[root["profile"]]; profile != nil {
		for key, value := range profile {
			root[key] = value
		}
	}
	return root
}

func (adapter) NativeSettings(store agent.CredStore) map[string]string {
	content, _ := readConfig(mcpConfigPath())
	root := nativeRootSettings(content)
	out := map[string]string{}
	for native, key := range nativeSettingKeys {
		if value := root[native]; value != "" {
			out[key] = value
		}
	}
	provider := root["model_provider"]
	switch store.Get(ckMethod) {
	case "azureFoundry":
		provider = azureProviderID
		if model := store.Get(ckAzureModel); model != "" {
			out[keyModel] = model
		}
	case "login", "apiKey", "accessToken":
		// A native private deployment is not a ChatGPT/OpenAI model ID.
		if provider != "" && provider != "openai" {
			delete(out, keyModel)
		}
		provider = "openai"
	}
	if provider == "" {
		provider = "openai"
	}
	active := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			parts, ok := parseTableHeader(line)
			active = ok && len(parts) == 2 && parts[0] == "model_providers" && parts[1] == provider
			continue
		}
		if !active {
			continue
		}
		key, value, ok := splitKV(line)
		if !ok {
			continue
		}
		for _, setting := range providerSettings {
			if key == setting.native {
				out[setting.key] = parseTOMLValue(value)
			}
		}
	}
	return out
}

// nativeProvider exposes only public provider metadata and credential presence.
// Codex itself consumes the original bearer token or env reference at run time.
func nativeProvider() (agent.CustomProvider, string) {
	content, err := readConfig(mcpConfigPath())
	if err != nil {
		return agent.CustomProvider{}, ""
	}
	id, model := nativeSelection(content)
	return parseModelProviders(content)[id], model
}
