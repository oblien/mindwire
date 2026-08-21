package grok

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Grok Build exposes `grok models` for interactive use. For the control plane,
// use xAI's documented account-scoped Models API instead: it is structured,
// authenticated by the same XAI_API_KEY that the ACP process receives, and
// avoids parsing terminal-oriented CLI output.
var _ agent.ModelsModule = adapter{}

var xaiModelsURL = "https://api.x.ai/v1/models"
var xaiHTTPClient = http.DefaultClient

func (adapter) Models(env map[string]string) ([]agent.ModelInfo, error) {
	// Grok's user config can declare aliases for any OpenAI-compatible endpoint.
	// They are runnable by `grok --model <alias>` even when an xAI API key is not
	// configured, so include them before attempting xAI account discovery.
	local := configuredModels()
	apiKey := strings.TrimSpace(env["XAI_API_KEY"])
	if apiKey == "" {
		return local, nil
	}
	req, err := http.NewRequest(http.MethodGet, xaiModelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := xaiHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, fmt.Errorf("xAI models API: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode xAI models API: %w", err)
	}
	out := make([]agent.ModelInfo, 0, len(local)+len(payload.Data))
	seen := make(map[string]bool, len(local)+len(payload.Data))
	for _, model := range payload.Data {
		if id := strings.TrimSpace(model.ID); id != "" && !seen[id] {
			out = append(out, agent.ModelInfo{ID: id, Label: id, Provider: "xai"})
			seen[id] = true
		}
	}
	// Account-discovered entries win when a configured alias has the same id:
	// the xAI API is authoritative for first-party model metadata/provider.
	for _, model := range local {
		if !seen[model.ID] {
			out = append(out, model)
			seen[model.ID] = true
		}
	}
	return out, nil
}

// configuredModels reads only Grok's documented `[model.<alias>]` records.
// This is read-only discovery: config editing belongs to Grok until its per-model
// structure can be represented losslessly by MindWire's provider abstraction.
func configuredModels() []agent.ModelInfo {
	path := filepath.Join(configBase(), "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []agent.ModelInfo
	var alias, label string
	flush := func() {
		if alias == "" {
			return
		}
		out = append(out, agent.ModelInfo{ID: alias, Label: agent.FirstNonEmpty(label, alias)})
		alias, label = "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			flush()
			alias = grokModelTableAlias(t)
			continue
		}
		if alias == "" {
			continue
		}
		key, value, ok := strings.Cut(t, "=")
		if ok && strings.TrimSpace(key) == "name" {
			label = grokTOMLString(value)
		}
	}
	flush()
	return out
}

func grokModelTableAlias(line string) string {
	if !strings.HasPrefix(line, "[model.") || !strings.HasSuffix(line, "]") {
		return ""
	}
	name := strings.TrimSuffix(strings.TrimPrefix(line, "[model."), "]")
	if unquoted, err := strconv.Unquote(name); err == nil {
		return strings.TrimSpace(unquoted)
	}
	if name == "" || strings.ContainsAny(name, " \t.") {
		return ""
	}
	return name
}

func grokTOMLString(value string) string {
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return strings.Trim(value, "'\"")
}
