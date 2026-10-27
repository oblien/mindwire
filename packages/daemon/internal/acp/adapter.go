package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/toolchain"
)

// Adapter is the common ACP surface. Native differences belong to Flavor,
// SettingsSchema and AuthFactory, never to a product or the orchestrator.
type Adapter struct {
	Flavor      Flavor
	Spec        toolchain.Spec
	Entry       agent.CatalogEntry
	Caps        agent.Capabilities
	Schema      agent.SettingsSchema
	AuthFactory func(agent.CredStore) agent.AuthModule
}

func (a Adapter) ID() string                       { return a.Entry.ID }
func (a Adapter) Meta() agent.CatalogEntry         { return a.Entry }
func (a Adapter) Capabilities() agent.Capabilities { return a.Caps }
func (a Adapter) Settings() agent.SettingsSchema   { return a.Schema }
func (a Adapter) Toolchain() toolchain.Spec        { return a.Spec }
func (a Adapter) InstallSteps() []agent.Step {
	return []agent.Step{{Name: a.Entry.Name, Check: a.Spec.Binary + " --version"}}
}
func (a Adapter) VersionCommand() string                                { return a.Spec.Binary + " --version" }
func (a Adapter) ConfigPath() string                                    { return "" }
func (a Adapter) Auth(store agent.CredStore) agent.AuthModule           { return a.AuthFactory(store) }
func (a Adapter) History(q agent.HistoryQuery) ([]agent.Message, error) { return q.Recorded, nil }
func (a Adapter) Notifications() agent.NotificationSpec {
	return agent.NotificationSpec{Conditions: []agent.ConditionUX{{Condition: agent.Finished, Title: a.Entry.Name + " finished"}, {Condition: agent.Errored, Title: a.Entry.Name + " hit an error"}}}
}
func (a Adapter) Doctor(ctx context.Context) []agent.Check {
	state, err := toolchain.InspectInstallation(ctx, a.Spec)
	if err != nil || state != "installed" {
		return []agent.Check{{Name: a.Entry.Name, Status: agent.CheckFail, Detail: "Run setup to install or repair " + a.Entry.Name}}
	}
	return []agent.Check{{Name: a.Entry.Name, Status: agent.CheckOK, Detail: "Native ACP executable installed"}}
}
func (a Adapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	f := a.Flavor
	if f.Capabilities == nil {
		f.Capabilities = a.Capabilities
	}
	return Run(ctx, f, in, emit)
}

func (a Adapter) Models(env map[string]string) ([]agent.ModelInfo, error) {
	if env["MINDWIRE_NATIVE_AUTH_METHOD"] == "" {
		return []agent.ModelInfo{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cwd, _ := os.Getwd()
	cwd, _ = filepath.Abs(cwd)
	in := agent.TurnInput{CWD: cwd, Env: env}
	p, err := Start(ctx, a.Flavor, in)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	init, err := Initialize(ctx, p.Client)
	if err != nil {
		return nil, err
	}
	if a.Flavor.AuthMethod != nil {
		if method := a.Flavor.AuthMethod(init, in); method != "" {
			if _, err = p.Client.Setup(ctx, "authenticate", map[string]any{"methodId": method, "_meta": a.Flavor.AuthMeta}); err != nil {
				return nil, err
			}
		}
	}
	raw, err := p.Client.Setup(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return nil, err
	}
	var session Session
	if err = json.Unmarshal(raw, &session); err != nil {
		return nil, err
	}
	session, err = AwaitModelOptions(ctx, p.Client, session)
	if err != nil {
		return nil, err
	}
	out := []agent.ModelInfo{}
	for _, m := range sessionModels(session) {
		out = append(out, agent.ModelInfo{ID: m.ID, Label: agent.FirstNonEmpty(m.Name, m.ID), Provider: a.ID()})
	}
	return out, nil
}

func Capabilities() agent.Capabilities {
	return agent.Capabilities{Protocol: agent.ProtocolACP, Output: agent.OutputStructuredJSON,
		History: agent.SupportEmulated, Sessions: agent.SupportNative, Resume: true, ToolEvents: true,
		Cancel: true, Models: true, ImageInput: true, Respond: true, Interrupt: true, SetModel: true,
		MCPServers: true, BotProfiles: true, Resolve: true}
}

func Validate(in agent.TurnInput) error {
	if in.Options.ContinueLatest {
		return fmt.Errorf("select an explicit session ID to resume this ACP agent")
	}
	if len(in.Options.OutputSchema) > 0 {
		return fmt.Errorf("this ACP agent does not expose structured output schemas")
	}
	return nil
}
