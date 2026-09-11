package codex

import (
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// nativeSelection reads the default provider/model, including a selected profile.
// It deliberately reads only routing metadata; credentials stay in config.toml.
func nativeSelection(content string) (provider, model string) {
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
		if key, value, ok := splitKV(line); ok && (key == "model_provider" || key == "model" || key == "profile") {
			current[key] = parseTOMLValue(value)
		}
	}
	provider, model = root["model_provider"], root["model"]
	if profile := profiles[root["profile"]]; profile != nil {
		if value := profile["model_provider"]; value != "" {
			provider = value
		}
		if value := profile["model"]; value != "" {
			model = value
		}
	}
	return provider, model
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
