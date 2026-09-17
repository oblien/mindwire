package codex

import (
	"strconv"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Resolve transport-independent turn options once for chat and compaction. Credentials remain
// process environment values; RPC config contains provider metadata and environment references.
func newAppServer(in agent.TurnInput, files materialized) appServer {
	a := appServer{
		command: "codex app-server", env: map[string]string{}, message: in.Message,
		model:  strings.TrimSpace(agent.FirstNonEmpty(in.Config[keyModel], in.Env[azureModelMarker])),
		effort: strings.TrimSpace(in.Config[keyEffort]), provider: strings.TrimSpace(in.Env[azureProviderMarker]),
		sandbox: sandbox(in), approval: approvalPolicy(in),
		reviewer: strings.TrimSpace(in.Config[keyReviewer]), collaboration: strings.TrimSpace(in.Config[keyCollaboration]),
		cwd:          strings.TrimSpace(agent.FirstNonEmpty(in.Config[keyWorkdir], in.CWD)),
		resumeID:     agent.FirstNonEmpty(in.Options.SessionID, in.SessionID),
		instructions: files.systemPrompt, images: files.imagePaths, outputSchema: in.Options.OutputSchema,
		config: map[string]any{},
	}
	for key, value := range in.Env {
		if key != azureProviderMarker && key != azureModelMarker {
			a.env[key] = value
		}
	}
	if in.Options.SessionID == "" && in.Options.ContinueLatest {
		a.resumeID, a.continueLatest = "", true
	}
	if a.provider != "" {
		a.command += " -c " + agent.ShellQuote("model_provider="+a.provider)
	} else if keyName := apiKeyEnv(in.Env); keyName != "" {
		// CODEX_API_KEY is exec-only. Give app-server the same key through a transient provider
		// env reference, without logging in or replacing the user's native auth/config files.
		a.provider = "mindwire-api"
		provider := map[string]any{
			"name": "Mindwire API connection", "wire_api": "responses", "env_key": keyName,
			"base_url":             strings.TrimRight(agent.FirstNonEmpty(in.Env["OPENAI_BASE_URL"], "https://api.openai.com/v1"), "/"),
			"requires_openai_auth": false,
		}
		headers := map[string]string{}
		for header, env := range map[string]string{"OpenAI-Organization": "OPENAI_ORGANIZATION", "OpenAI-Project": "OPENAI_PROJECT"} {
			if in.Env[env] != "" {
				headers[header] = env
			}
		}
		if len(headers) > 0 {
			provider["env_http_headers"] = headers
		}
		a.config["model_providers"] = map[string]any{a.provider: provider}
	}
	if in.CWD != "" {
		a.command = "cd " + agent.ShellQuote(in.CWD) + " && " + a.command
	}
	if limit, err := strconv.Atoi(strings.TrimSpace(in.Config[keyAutoCompact])); err == nil && limit > 0 {
		a.config["model_auto_compact_token_limit"] = limit
	}
	if dir := strings.TrimSpace(in.Config[keyAddDir]); dir != "" {
		a.config["sandbox_workspace_write"] = map[string]any{"writable_roots": []string{dir}}
	}
	if len(files.mcpServers) > 0 {
		a.config["mcp_servers"] = mcpConfigValues(files.mcpServers)
	}
	return a
}

func apiKeyEnv(env map[string]string) string {
	for _, name := range []string{"CODEX_API_KEY", "OPENAI_API_KEY"} {
		if strings.TrimSpace(env[name]) != "" {
			return name
		}
	}
	return ""
}

func mcpConfigValues(servers map[string]codexMCPServer) map[string]any {
	out := map[string]any{}
	for name, server := range servers {
		fields := map[string]any{}
		for key, value := range map[string]string{
			"command": server.Command, "cwd": server.Cwd, "url": server.URL,
			"bearer_token_env_var":        server.BearerTokenEnvVar,
			"default_tools_approval_mode": server.DefaultToolsApprovalMode,
		} {
			if value != "" {
				fields[key] = value
			}
		}
		if len(server.Args) > 0 {
			fields["args"] = server.Args
		}
		if len(server.Env) > 0 {
			fields["env"] = server.Env
		}
		if len(server.HTTPHeaders) > 0 {
			fields["http_headers"] = server.HTTPHeaders
		}
		out[name] = fields
	}
	return out
}
