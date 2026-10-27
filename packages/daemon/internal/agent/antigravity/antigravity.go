// Package antigravity drives Google's official ACP runtime and companion harness.
package antigravity

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/nativeauth"
)

func init() { agent.Register(newAdapter()) }

func flavor() acp.Flavor {
	return acp.Flavor{ID: "antigravity", Name: "Antigravity", Binary: "agy-acp", Validate: acp.Validate,
		Args: func(agent.TurnInput) []string {
			if runtime.GOOS == "linux" {
				return []string{"--uid="}
			}
			return nil
		},
		ResumeMethod: "session/resume",
		AuthMethod: func(_ acp.Initialization, in agent.TurnInput) string {
			if in.Env["MINDWIRE_NATIVE_AUTH_METHOD"] == "apiKey" {
				return "gemini-api-key"
			}
			return in.Env["MINDWIRE_NATIVE_AUTH_METHOD"]
		},
		Configure: func(ctx context.Context, c *acp.Client, s acp.Session, in agent.TurnInput) error {
			return acp.Configure(ctx, c, s, in.Config["model"], in.Config["permission-mode"])
		},
	}
}

func newAdapter() acp.Adapter {
	return acp.Adapter{Flavor: flavor(), Spec: distribution(), Entry: agent.CatalogEntry{ID: "antigravity", Name: "Antigravity", Tagline: "Google's native coding agent"}, Caps: acp.Capabilities(),
		Schema: agent.SettingsSchema{Sections: []agent.Section{{Title: "Execution", Fields: []agent.Field{
			{Key: "model", Label: "Model", Type: agent.FieldText, Scope: agent.ScopeUnified, Canon: agent.CanonModel, Help: "Choose a model available to your Google account."},
			{Key: "permission-mode", Label: "Permission mode", Type: agent.FieldSelect, Scope: agent.ScopeUnified, Canon: agent.CanonPermissionMode, Default: "default", Options: []agent.Option{{Value: "default", Label: "Ask before tools"}, {Value: "auto_edit", Label: "Approve edits"}, {Value: "yolo", Label: "Always approve"}}},
		}}}}, AuthFactory: auth}
}

func auth(store agent.CredStore) agent.AuthModule {
	return nativeauth.New(nativeauth.Config{ID: "antigravity", Name: "Antigravity", KeyEnv: "GEMINI_API_KEY",
		Logins: []agent.AuthMethod{{ID: "oauth-personal", Label: "Sign in with Google", Interactive: true, Scope: agent.ScopeUnified}, {ID: "oauth-business", Label: "Gemini Enterprise", Interactive: true, Scope: agent.ScopeCustom}},
		Environment: func(home string) map[string]string {
			return map[string]string{"GEMINI_HOME": home, "AGY_ACP_FORCE_FILE_STORAGE": "1", "BROWSER": "true", "GOOGLE_API_KEY": "", "GOOGLE_GENAI_USE_VERTEXAI": "", "GOOGLE_APPLICATION_CREDENTIALS": ""}
		},
		Login: func(flow *agent.AuthFlow, env map[string]string) error {
			f := flavor()
			f.OnOutput = func(line string) { nativeauth.CaptureURL(flow, line, "accounts.google.com") }
			p, err := acp.Start(flow.Context(), f, agent.TurnInput{Env: env})
			if err != nil {
				return err
			}
			defer p.Close()
			init, err := acp.Initialize(flow.Context(), p.Client)
			if err != nil {
				return err
			}
			method := env["MINDWIRE_NATIVE_AUTH_METHOD"]
			available := false
			for _, m := range init.AuthMethods {
				available = available || m.ID == method
			}
			if !available {
				return fmt.Errorf("native auth method unavailable")
			}
			_, err = p.Client.Setup(flow.Context(), "authenticate", map[string]string{"methodId": method})
			return err
		},
		Logout: func(ctx context.Context, env map[string]string) error {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			p, err := acp.Start(ctx, flavor(), agent.TurnInput{Env: env})
			if err != nil {
				return err
			}
			defer p.Close()
			if _, err = acp.Initialize(ctx, p.Client); err != nil {
				return err
			}
			_, err = p.Client.Setup(ctx, "auth/logout", map[string]any{})
			return err
		},
	}, store)
}
