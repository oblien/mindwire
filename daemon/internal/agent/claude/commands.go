package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

var claudeCommands agent.CommandCache
var _ agent.CommandsModule = adapter{}

func (a adapter) Commands(ctx context.Context, query agent.CommandQuery) (agent.CommandCatalog, error) {
	return claudeCommands.Get(ctx, agent.CommandCacheKey("claude", query), query.Refresh, func(ctx context.Context) (agent.CommandCatalog, error) {
		commands := agent.FilterSettingsCommands([]agent.Command{
			{Name: "model", Label: "Model", Description: "Choose the model and reasoning effort.", Kind: "settings", SettingCanons: []string{agent.CanonModel, agent.CanonReasoningEffort, agent.CanonFallbackModel}},
			{Name: "permissions", Label: "Permissions", Description: "Choose approval mode and tool permissions.", Kind: "settings", SettingCanons: []string{agent.CanonPermissionMode, agent.CanonAllowedTools, agent.CanonAllowRules, agent.CanonDenyRules}},
			{Name: "effort", Label: "Reasoning effort", Description: "Adjust how much the model reasons.", Kind: "settings", SettingCanons: []string{agent.CanonReasoningEffort}},
			{Name: "plan", Label: "Planning mode", Description: "Choose planning or an execution permission mode.", Kind: "settings", SettingCanons: []string{agent.CanonPermissionMode}},
			{Name: "compact", Label: "Compact conversation", Description: "Summarize earlier context in this conversation.", Kind: "compact", AcceptsArguments: true, ArgumentHint: "Optional instructions", RequiresIdle: true, RequiresSession: true},
			{Name: "autocompact", Label: "Auto-compaction", Description: "Adjust the context window before automatic compaction.", Kind: "settings", SettingCanons: []string{agent.CanonAutoCompactTokens}},
			{Name: "config", Aliases: []string{"settings"}, Label: "Agent settings", Description: "View and edit all available settings.", Kind: "settings"},
			{Name: "new", Aliases: []string{"clear", "reset"}, Label: "New conversation", Description: "Start fresh and keep this conversation in history.", Kind: "new_chat"},
			{Name: "resume", Label: "Conversations", Description: "Choose another conversation in this project.", Kind: "history"},
			{Name: "rename", Aliases: []string{"name"}, Label: "Rename conversation", Kind: "rename"},
		}, a.Settings())
		native, err := readClaudeCommands(ctx, query)
		catalog := agent.CommandCatalog{Commands: commands}
		if err != nil {
			catalog.Warning = "Couldn't load Claude's additional commands. You can still use the controls below."
			return catalog, nil
		}
		seen := map[string]bool{}
		for _, command := range commands {
			seen[command.Name] = true
			for _, alias := range command.Aliases {
				seen[alias] = true
			}
		}
		// Terminal presentation and process-only settings cannot control this client's UI
		// or survive a per-turn process. Expose them when a native persistent control exists.
		for _, name := range []string{"color", "focus", "heapdump", "fast", "output-style", "reload-plugins", "reload-skills", "workflow-launch-exec"} {
			seen[name] = true
		}
		sort.SliceStable(native, func(i, j int) bool { return native[i].Name < native[j].Name })
		for _, command := range native {
			if !agent.ValidCommandName(command.Name) || seen[command.Name] {
				continue
			}
			seen[command.Name] = true
			aliases := make([]string, 0, len(command.Aliases))
			for _, alias := range command.Aliases {
				if agent.ValidCommandName(alias) && !seen[alias] {
					aliases = append(aliases, alias)
					seen[alias] = true
				}
			}
			catalog.Commands = append(catalog.Commands, agent.Command{Name: command.Name, Label: command.Name,
				Description: command.Description, Kind: "prompt", Aliases: aliases, ArgumentHint: command.ArgumentHint,
				AcceptsArguments: true, RequiresIdle: true, Input: "/" + command.Name})
		}
		return catalog, nil
	})
}

type nativeCommand struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgumentHint string   `json:"argumentHint"`
	Aliases      []string `json:"aliases"`
}

func readClaudeCommands(ctx context.Context, query agent.CommandQuery) ([]nativeCommand, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	input := agent.TurnInput{Config: query.Config, Env: query.Env}
	command := buildCommand(input, materialized{}, transportPersistent) + " --no-session-persistence"
	if query.Env[subscriptionMarker] != "" {
		command = "(" + subscriptionShell(query.Env) + command + ")"
	}
	if query.CWD != "" {
		command = "cd " + agent.ShellQuote(query.CWD) + " && " + command
	}
	cmd := exec.CommandContext(ctx, "bash", "-lc", toolchain.Shell(command))
	proc.Group(cmd)
	cmd.Env = toolchain.Environment()
	for k, v := range query.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if permissionMode(input) == "bypassPermissions" {
		cmd.Env = append(cmd.Env, "IS_SANDBOX=1")
	}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	// Initialize only. No user prompt, new session, model request or transcript is written.
	if err = json.NewEncoder(stdin).Encode(map[string]any{"type": "control_request", "request_id": initializeRequestID,
		"request": map[string]any{"subtype": "initialize"}}); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(io.LimitReader(stdout, 4<<20))
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var frame struct {
			Type     string `json:"type"`
			Response struct {
				ID      string `json:"request_id"`
				Subtype string `json:"subtype"`
				Data    struct {
					Commands []nativeCommand `json:"commands"`
				} `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil || frame.Type != "control_response" || frame.Response.ID != initializeRequestID {
			continue
		}
		if frame.Response.Subtype != "success" {
			return nil, fmt.Errorf("Claude command discovery failed")
		}
		return frame.Response.Data.Commands, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("Claude command discovery: %w", err)
	}
	return nil, fmt.Errorf("Claude did not return a command list")
}
