package codex

import (
	"context"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

var _ agent.ModelsModule = adapter{}

// Models comes from the installed app-server, including its per-model reasoning
// levels. Private deployments remain explicit: a public model list cannot tell
// us which deployment names exist at a user's endpoint.
func (adapter) Models(env map[string]string) ([]agent.ModelInfo, error) {
	var custom *agent.ModelInfo
	if model := env[azureModelMarker]; env[azureProviderMarker] != "" && model != "" {
		custom = &agent.ModelInfo{ID: model, Label: model, Provider: "azure", Custom: true, Default: true}
	} else if env[azureProviderMarker] != "openai" && apiKeyEnv(env) == "" {
		if provider, model := nativeProvider(); provider.ID != "" && provider.ID != "openai" && model != "" {
			custom = &agent.ModelInfo{ID: model, Label: model, Provider: provider.ID, Custom: true, Default: true}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := readNativeModels(ctx)
	if custom != nil {
		for _, model := range models {
			if model.ID == custom.ID {
				custom.Reasoning, custom.ReasoningEfforts, custom.DefaultReasoningEffort = model.Reasoning, model.ReasoningEfforts, model.DefaultReasoningEffort
				custom.Input = model.Input
				break
			}
		}
		return []agent.ModelInfo{*custom}, nil
	}
	if err != nil {
		return []agent.ModelInfo{}, nil
	}
	return models, nil
}

type nativeModel struct {
	ID                        string   `json:"id"`
	Model                     string   `json:"model"`
	DisplayName               string   `json:"displayName"`
	Hidden                    bool     `json:"hidden"`
	IsDefault                 bool     `json:"isDefault"`
	Input                     []string `json:"inputModalities"`
	DefaultReasoningEffort    string   `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []struct {
		Effort      string `json:"reasoningEffort"`
		Description string `json:"description"`
	} `json:"supportedReasoningEfforts"`
}

func readNativeModels(ctx context.Context) ([]agent.ModelInfo, error) {
	client, err := startAuthRPC(ctx)
	if err != nil {
		return nil, err
	}
	defer client.close()
	out := []agent.ModelInfo{}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result struct {
			Data       []nativeModel `json:"data"`
			NextCursor string        `json:"nextCursor"`
		}
		if err := client.call(ctx, "model/list", params, &result); err != nil {
			return nil, err
		}
		for _, model := range result.Data {
			id := agent.FirstNonEmpty(model.Model, model.ID)
			if id == "" || model.Hidden || seen[id] {
				continue
			}
			seen[id] = true
			entry := agent.ModelInfo{ID: id, Label: agent.FirstNonEmpty(model.DisplayName, id), Provider: "openai", Default: model.IsDefault,
				Input: model.Input, DefaultReasoningEffort: model.DefaultReasoningEffort}
			for _, effort := range model.SupportedReasoningEfforts {
				if effort.Effort == "" {
					continue
				}
				entry.ReasoningEfforts = append(entry.ReasoningEfforts, agent.Option{Value: effort.Effort, Label: strings.ToUpper(effort.Effort[:1]) + effort.Effort[1:], Help: effort.Description})
			}
			entry.Reasoning = len(entry.ReasoningEfforts) > 0
			out = append(out, entry)
		}
		if result.NextCursor == "" || result.NextCursor == cursor {
			break
		}
		cursor = result.NextCursor
	}
	return out, nil
}

// The schema keeps manual deployment entry available when /models is offline.
func modelChoices() []string { return nil }
