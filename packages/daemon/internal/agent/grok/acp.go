package grok

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

func acpCommand(in agent.TurnInput) []string {
	// Disable background self-updates in every programmatic run. They create
	// nondeterministic CI/daemon behavior and Grok documents this flag for ACP.
	args := []string{"--no-auto-update"}
	if strings.EqualFold(strings.TrimSpace(in.Config[keyPermission]), "ask") {
		args = append(args, "--permission-mode", "default")
	}
	args = append(args, "agent")
	if v := strings.TrimSpace(in.Config[keyModel]); v != "" {
		args = append(args, "--model", v)
	}
	// The default MindWire execution mode is autonomous. "ask" deliberately
	// overrides stored permission defaults and surfaces session/request_permission.
	if !strings.EqualFold(strings.TrimSpace(in.Config[keyPermission]), "ask") {
		args = append(args, "--always-approve")
	}
	return append(args, "stdio")
}

// ACP requires an absolute workspace path. The daemon deliberately permits AGENT_CWD to be empty
// (then a turn inherits the daemon process directory), but passing that through as "." is rejected by
// Grok Build with `session/new: Invalid params`. Normalize once at the ACP boundary so new, loaded, and
// resumed sessions all identify the same native workspace.
func acpCWD(in agent.TurnInput) (string, error) {
	cwd := strings.TrimSpace(in.CWD)
	if cwd == "" {
		cwd = "."
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve Grok Build workspace: %w", err)
	}
	return filepath.Clean(abs), nil
}

func flavor() acp.Flavor {
	return acp.Flavor{
		ID: "grok", Name: "Grok Build", Binary: "grok", Args: acpCommand, Validate: validateACPInput,
		ResumeLatestMethod: "session/resume",
		Capabilities:       (adapter{}).Capabilities,
		AuthMeta:           map[string]any{"headless": true},
		AuthMethod: func(init acp.Initialization, in agent.TurnInput) string {
			if strings.TrimSpace(in.Env["XAI_API_KEY"]) != "" {
				return "xai.api_key"
			}
			if in.Env["MINDWIRE_NATIVE_AUTH_METHOD"] == "login" {
				return "cached_token"
			}
			return init.DefaultAuthMethodID
		},
		NewSessionMeta: func(in agent.TurnInput) map[string]any {
			meta := map[string]any{}
			if v := strings.TrimSpace(in.Config[keyRules]); v != "" {
				meta["rules"] = v
			}
			if v := agent.FirstNonEmpty(in.Options.SystemPrompt, in.Config[keySystemPrompt]); v != "" {
				meta["systemPromptOverride"] = v
			}
			if in.Config[keyPermission] == "ask" {
				meta["autoMode"] = false
			} else {
				meta["yoloMode"] = true
			}
			return meta
		},
		Interaction: grokInteraction, Reply: grokReply,
		Update: func(m acp.Message, s *acp.Stream) {
			if task := grokTask(m, s.SessionID); task != nil {
				s.Task(*task, strings.HasSuffix(m.Method, "task_backgrounded"))
				return
			}
			switch m.Method {
			case "x.ai/session/update", "_x.ai/session/update", "x.ai/session_notification", "_x.ai/session_notification":
				s.Update(m.Params)
			}
		},
		PromptID: grokPromptID, Completion: grokCompletion,
	}
}

func runACP(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	return acp.Run(ctx, flavor(), in, emit)
}
func acpRun(ctx context.Context, c *acp.Client, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	return acp.RunSession(ctx, c, flavor(), in, emit)
}

func validateACPInput(in agent.TurnInput) error {
	sessionID := agent.FirstNonEmpty(strings.TrimSpace(in.Options.SessionID), strings.TrimSpace(in.SessionID))
	// ACP supplies these parameters only on session/new. Never pretend that an
	// override was applied to an existing native session when Grok cannot receive
	// it there. MCP servers are refreshed on new/load/resume independently.
	newSessionOnly := strings.TrimSpace(in.Options.SystemPrompt) != "" ||
		strings.TrimSpace(in.Config[keySystemPrompt]) != "" ||
		strings.TrimSpace(in.Config[keyRules]) != ""
	if newSessionOnly && (sessionID != "" || in.Options.ContinueLatest) {
		return fmt.Errorf("Grok Build applies system-prompt overrides only when creating a new session; start a fresh session to change them")
	}
	return nil
}
