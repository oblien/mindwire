package mindwire

import (
	"context"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

type Command = agent.Command
type CommandCatalog = agent.CommandCatalog
type CommandInvocation = agent.CommandInvocation

type CommandsOptions struct {
	Directory string
	Refresh   bool // Refresh completed metadata; concurrent discovery is still shared.
}

// Commands discovers controls and native commands in the project's directory. It does
// not start or resume a model turn. Execute prompt entries with TurnOptions.Command;
// use Compact or Config for controls instead of sending their slash text to the model.
func (c *Client) Commands(ctx context.Context, options CommandsOptions, opts ...ScopedOption) (CommandCatalog, error) {
	ag, err := c.resolve(opts)
	if err != nil {
		return CommandCatalog{}, err
	}
	module, ok := ag.Adapter.(agent.CommandsModule)
	if !ok {
		return CommandCatalog{}, &APIError{Message: "this harness does not expose native commands", Status: http.StatusBadRequest, Op: "Commands"}
	}
	return module.Commands(ctx, agent.CommandQuery{CWD: agent.ResolveDir(options.Directory, c.core.sup.CWD()), Refresh: options.Refresh,
		Config: agent.ReadSettings(ag.Adapter, ag.Creds), Env: ag.Auth.EnvForRun()})
}
