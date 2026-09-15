package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
)

// A custom provider's selected deployment is known to work; Codex's stock review
// model may not exist there. Preserve the CLI's complete native model metadata and
// bind automatic review to the selected model using auto_review_model_override.
// The native reviewer still owns the policy, reasoning, and approval decision.
// model_catalog_json must be a startup override: a thread config override is a no-op.
func (a *appServer) prepareReviewModel(ctx context.Context) (func(), error) {
	noop := func() {}
	if a.reviewer != "" && a.reviewer != "auto_review" && a.reviewer != "guardian_subagent" {
		return noop, nil
	}
	content, err := readConfig(filepath.Join(configBase(), "config.toml"))
	if err != nil {
		return noop, fmt.Errorf("read Codex review configuration: %w", err)
	}
	reviewer := agent.FirstNonEmpty(a.reviewer, rootConfigString(content, "approvals_reviewer"))
	if reviewer != "auto_review" && reviewer != "guardian_subagent" {
		return noop, nil
	}
	provider := agent.FirstNonEmpty(a.provider, rootConfigString(content, "model_provider"))
	if provider == "" || provider == "openai" {
		return noop, nil
	}
	baseURL := parseModelProviders(content)[provider].BaseURL
	if providers, ok := a.config["model_providers"].(map[string]any); ok {
		if configured, ok := providers[provider].(map[string]any); ok {
			baseURL, _ = configured["base_url"].(string)
		}
	}
	if endpoint, err := url.Parse(baseURL); err == nil && strings.EqualFold(endpoint.Hostname(), "api.openai.com") {
		return noop, nil
	}
	// An explicit native catalog already defines the provider's model inventory and
	// reviewer choices. Respect it instead of replacing a user-managed configuration.
	if rootConfigString(content, "model_catalog_json") != "" {
		return noop, nil
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(readCtx, "codex", "debug", "models")
	proc.Group(cmd)
	cmd.Dir = a.cwd
	cmd.Env = os.Environ()
	for key, value := range a.env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	data, err := cmd.Output()
	if err != nil {
		return noop, fmt.Errorf("could not read Codex model metadata for automatic review: %w", err)
	}
	model := agent.FirstNonEmpty(a.model, rootConfigString(content, "model"))
	data, err = reviewCatalog(data, model)
	if err != nil {
		return noop, err
	}
	var temporary agent.TempFiles
	path, err := temporary.Write("mindwire-codex-review-models-*.json", data)
	if err != nil {
		return temporary.Cleanup, err
	}
	a.command += " -c " + agent.ShellQuote("model_catalog_json="+tomlString(path))
	return temporary.Cleanup, nil
}

func reviewCatalog(data []byte, model string) ([]byte, error) {
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("read native Codex model catalog: %w", err)
	}
	var models []map[string]json.RawMessage
	if err := json.Unmarshal(catalog["models"], &models); err != nil {
		return nil, fmt.Errorf("read native Codex models: %w", err)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("native Codex model catalog is empty")
	}
	found := model == ""
	for _, entry := range models {
		var slug string
		if json.Unmarshal(entry["slug"], &slug) != nil || slug == "" {
			continue
		}
		found = found || slug == model
		// All other native instructions, capabilities, and model limits stay intact.
		entry["auto_review_model_override"], _ = json.Marshal(agent.FirstNonEmpty(model, slug))
	}
	if !found {
		// For an opaque deployment alias Codex already uses native fallback metadata.
		// Remove the stock review-only deployment so the native reviewer falls back
		// to that alias as well. Keep native chat metadata: Codex rejects empty catalogs.
		filtered := models[:0]
		for _, entry := range models {
			var slug string
			_ = json.Unmarshal(entry["slug"], &slug)
			if slug != "codex-auto-review" {
				filtered = append(filtered, entry)
			}
		}
		models = filtered
		if len(models) == 0 {
			return nil, fmt.Errorf("native Codex model catalog has no chat models for automatic review")
		}
	}
	catalog["models"], _ = json.Marshal(models)
	return json.Marshal(catalog)
}

func rootConfigString(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			break
		}
		if k, value, ok := splitKV(line); ok && k == key {
			return parseTOMLValue(value)
		}
	}
	return ""
}
