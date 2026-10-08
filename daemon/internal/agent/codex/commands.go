package codex

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

var codexCommands agent.CommandCache
var _ agent.CommandsModule = adapter{}

func (a adapter) Commands(ctx context.Context, query agent.CommandQuery) (agent.CommandCatalog, error) {
	return codexCommands.Get(ctx, agent.CommandCacheKey("codex", query), query.Refresh, func(ctx context.Context) (agent.CommandCatalog, error) {
		commands := agent.FilterSettingsCommands([]agent.Command{
			{Name: "model", Label: "Model", Description: "Choose the model and reasoning effort.", Kind: "settings", SettingCanons: []string{agent.CanonModel, agent.CanonReasoningEffort, agent.CanonReasoningSummary}},
			{Name: "permissions", Aliases: []string{"approvals"}, Label: "Permissions", Description: "Choose approval rules and sandbox access.", Kind: "settings", SettingCanons: []string{agent.CanonPermissionMode, agent.CanonApprovalReviewer, keySandbox, agent.CanonExtraDirs}},
			{Name: "effort", Label: "Reasoning effort", Description: "Adjust how much the model reasons.", Kind: "settings", SettingCanons: []string{agent.CanonReasoningEffort}},
			{Name: "plan", Label: "Planning mode", Description: "Choose planning or implementation.", Kind: "settings", SettingCanons: []string{keyCollaboration}},
			{Name: "compact", Label: "Compact conversation", Description: "Summarize earlier context in this conversation.", Kind: "compact", RequiresIdle: true, RequiresSession: true},
			{Name: "settings", Label: "Agent settings", Description: "View and edit all available settings.", Kind: "settings"},
			{Name: "new", Aliases: []string{"clear"}, Label: "New conversation", Description: "Start fresh and keep this conversation in history.", Kind: "new_chat"},
			{Name: "resume", Label: "Conversations", Description: "Choose another conversation in this project.", Kind: "history"},
			{Name: "rename", Label: "Rename conversation", Kind: "rename"},
		}, a.Settings())
		catalog := agent.CommandCatalog{Commands: commands}
		command := "codex app-server"
		if query.CWD != "" {
			command = "cd " + agent.ShellQuote(query.CWD) + " && " + command
		}
		client, err := startMetadataRPCWithEnv(ctx, command, query.Env)
		if err != nil {
			catalog.Warning = "Couldn't load Codex skills. You can still use the controls below."
			return catalog, nil
		}
		defer client.close()
		var response struct {
			Data []struct {
				Skills []nativeSkill `json:"skills"`
			} `json:"data"`
		}
		if err := client.call(ctx, "skills/list", map[string]any{"cwds": []string{query.CWD}, "forceReload": true}, &response); err != nil {
			catalog.Warning = "Couldn't load Codex skills. You can still use the controls below."
			return catalog, nil
		}
		seen := map[string]bool{}
		for _, entry := range commands {
			seen[entry.Name] = true
			for _, alias := range entry.Aliases {
				seen[alias] = true
			}
		}
		var skills []nativeSkill
		for _, entry := range response.Data {
			skills = append(skills, entry.Skills...)
		}
		// Prefer repository skills over user/system skills with the same invocation.
		sort.SliceStable(skills, func(i, j int) bool {
			if skills[i].Name == skills[j].Name {
				return skills[i].Scope == "repo" && skills[j].Scope != "repo"
			}
			return skills[i].Name < skills[j].Name
		})
		for _, skill := range skills {
			if !skill.Enabled || !agent.ValidCommandName(skill.Name) || !filepath.IsAbs(skill.Path) || seen[skill.Name] {
				continue
			}
			seen[skill.Name] = true
			catalog.Commands = append(catalog.Commands, agent.Command{Name: skill.Name,
				Label:       agent.FirstNonEmpty(skill.Interface.DisplayName, skill.Name),
				Description: agent.FirstNonEmpty(skill.Interface.ShortDescription, skill.ShortDescription, skill.Description),
				Kind:        "prompt", AcceptsArguments: true, ArgumentHint: "Instructions", RequiresIdle: true,
				Input: "$" + skill.Name, SkillPath: skill.Path})
		}
		return catalog, nil
	})
}

type nativeSkill struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	ShortDescription string `json:"shortDescription"`
	Path             string `json:"path"`
	Scope            string `json:"scope"`
	Enabled          bool   `json:"enabled"`
	Interface        struct {
		DisplayName      string `json:"displayName"`
		ShortDescription string `json:"shortDescription"`
	} `json:"interface"`
}
