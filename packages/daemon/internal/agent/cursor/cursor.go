// Package cursor drives Cursor's official ACP CLI, independently of its IDE.
package cursor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/nativeauth"
	"github.com/oblien/mindwire/packages/daemon/internal/proc"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
)

func init() { agent.Register(newAdapter()) }

func flavor() acp.Flavor {
	return acp.Flavor{ID: "cursor", Name: "Cursor", Binary: "cursor-agent", Validate: acp.Validate,
		Args: func(in agent.TurnInput) []string {
			args := []string{"--trust"}
			if in.Config["permission-mode"] == "always-approve" {
				args = append(args, "--force")
			}
			return append(args, "acp")
		},
		Configure: func(ctx context.Context, c *acp.Client, s acp.Session, in agent.TurnInput) error {
			return acp.Configure(ctx, c, s, in.Config["model"], in.Config["mode"])
		},
		Interaction: interaction, Reply: reply, Update: update,
	}
}

func newAdapter() acp.Adapter {
	return acp.Adapter{Flavor: flavor(), Spec: distribution(), Entry: agent.CatalogEntry{ID: "cursor", Name: "Cursor", Tagline: "Cursor's native coding agent"}, Caps: acp.Capabilities(),
		Schema: agent.SettingsSchema{Sections: []agent.Section{{Title: "Execution", Fields: []agent.Field{
			{Key: "model", Label: "Model", Type: agent.FieldText, Scope: agent.ScopeUnified, Canon: agent.CanonModel, Help: "Choose a model available to your Cursor account."},
			{Key: "permission-mode", Label: "Permission mode", Type: agent.FieldSelect, Scope: agent.ScopeUnified, Canon: agent.CanonPermissionMode, Default: "ask", Options: []agent.Option{{Value: "ask", Label: "Ask before tools"}, {Value: "always-approve", Label: "Always approve"}}},
			{Key: "mode", Label: "Agent mode", Type: agent.FieldSelect, Scope: agent.ScopeCustom, Default: "agent", Options: []agent.Option{{Value: "agent", Label: "Agent"}, {Value: "plan", Label: "Plan"}, {Value: "ask", Label: "Ask"}}},
		}}}}, AuthFactory: auth}
}

func auth(store agent.CredStore) agent.AuthModule {
	return nativeauth.New(nativeauth.Config{ID: "cursor", Name: "Cursor", KeyEnv: "CURSOR_API_KEY",
		Logins: []agent.AuthMethod{{ID: "login", Label: "Sign in with Cursor", Interactive: true, Scope: agent.ScopeUnified}},
		Environment: func(home string) map[string]string {
			return map[string]string{"CURSOR_CONFIG_DIR": home, "CURSOR_DATA_DIR": home, "AGENT_CLI_CREDENTIAL_STORE": "file", "CURSOR_AUTH_TOKEN": "", "NO_OPEN_BROWSER": "1", "BROWSER": "true"}
		},
		Login: func(f *agent.AuthFlow, env map[string]string) error {
			return nativeauth.LoginCommand(f, "cursor-agent", []string{"login"}, env, "cursor.com", "www.cursor.com")
		},
		Verify: func(ctx context.Context, env map[string]string) bool {
			ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			cmd := toolchain.CommandContext(ctx, "cursor-agent", "status", "--format", "json")
			proc.Group(cmd)
			for k, v := range env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			data, err := cmd.Output()
			if err != nil {
				return false
			}
			var status struct {
				Authenticated   bool `json:"authenticated"`
				IsAuthenticated bool `json:"isAuthenticated"`
			}
			return json.Unmarshal(data, &status) == nil && (status.Authenticated || status.IsAuthenticated)
		},
		Logout: func(ctx context.Context, env map[string]string) error {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			cmd := toolchain.CommandContext(ctx, "cursor-agent", "logout")
			proc.Group(cmd)
			for k, v := range env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			return cmd.Run()
		},
	}, store)
}
