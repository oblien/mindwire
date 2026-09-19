package codex

import (
	"strconv"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

var providerSettings = []struct{ key, native string }{
	{keyRequestRetries, "request_max_retries"},
	{keyStreamRetries, "stream_max_retries"},
	{keyStreamTimeout, "stream_idle_timeout_ms"},
}

func providerTuning(in agent.TurnInput, selected string) []string {
	provider := agent.FirstNonEmpty(selected, in.Env[azureProviderMarker])
	if provider == "" {
		content, _ := readConfig(mcpConfigPath())
		provider, _ = nativeSelection(content)
	}
	if provider == "" {
		provider = "openai"
	}
	var overrides []string
	for _, setting := range providerSettings {
		value := strings.TrimSpace(in.Config[setting.key])
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil || (setting.key == keyStreamTimeout && n == 0) {
			continue
		}
		// Codex splits CLI override paths on dots; TOML quotes here become
		// literal key characters and create an invalid second provider.
		overrides = append(overrides, "model_providers."+provider+"."+setting.native+"="+strconv.FormatUint(n, 10))
	}
	return overrides
}
