package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/setup"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

var ErrAgentBusy = errors.New("agent has running chats; wait for them to finish before installing or updating")

// Setup owns the same harness lock as turn admission. An HTTP client, SDK caller, or second
// persona cannot start a turn during installation, or replace a CLI beneath an existing turn.
// Repeated setup/update requests still attach to the one detached job.
func (s *Supervisor) Setup(a *Agent, force bool) (setup.Status, error) {
	steps, err := s.setupPlan(a, force)
	if err != nil {
		return setup.Status{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.setup.Status(a.ID()); current.Running {
		return current, nil
	}
	if s.activeAgents[a.ID()] > 0 {
		return setup.Status{}, ErrAgentBusy
	}
	return s.setup.Start(a.ID(), steps, force, 20*time.Minute), nil
}

func (s *Supervisor) setupPlan(a *Agent, force bool) ([]agent.Step, error) {
	plan := append([]agent.Step(nil), a.Adapter.InstallSteps()...)
	if mod, ok := a.Adapter.(agent.ToolchainModule); ok {
		spec := mod.Toolchain()
		found := false
		for i := range plan {
			if plan[i].Name != spec.Name {
				continue
			}
			// Bind the managed installer without discarding adapter prerequisites or other steps.
			plan[i].CheckFunc = func(ctx context.Context) (string, error) { return s.toolchain.Check(ctx, spec) }
			plan[i].InstallFunc = func(ctx context.Context) (string, error) { return s.toolchain.Install(ctx, spec, force) }
			found = true
		}
		if !found {
			return nil, fmt.Errorf("managed CLI %q has no declared setup step", spec.Name)
		}
	}
	return setup.Plan(plan)
}

func (s *Supervisor) SetupStatus(a *Agent) setup.Status { return s.setup.Status(a.ID()) }

// StartConflict describes a failed admission for both transports. Admission itself happens under
// s.mu in start/StartResolve; this read only supplies a useful retry reason to the client.
func (s *Supervisor) StartConflict(a *Agent) string {
	if s.setup.Status(a.ID()).Running {
		return "agent setup is still running; wait for the CLI to be ready"
	}
	if err := s.checkSoftware(a); err != nil {
		return err.Error()
	}
	return "a turn is already running for this chat"
}

func (s *Supervisor) Software(ctx context.Context, a *Agent, refresh bool) *toolchain.Software {
	mod, ok := a.Adapter.(agent.ToolchainModule)
	if !ok {
		return nil
	}
	var info toolchain.Software
	if refresh {
		info = s.toolchain.RefreshSoftware(ctx, mod.Toolchain(), true)
	} else {
		info = s.toolchain.Software(ctx, mod.Toolchain())
	}
	return s.withDependencies(ctx, a, info, refresh)
}

func (s *Supervisor) RefreshSoftware(ctx context.Context, a *Agent, force bool) *toolchain.Software {
	mod, ok := a.Adapter.(agent.ToolchainModule)
	if !ok {
		return nil
	}
	info := s.toolchain.RefreshSoftware(ctx, mod.Toolchain(), force)
	return s.withDependencies(ctx, a, info, force)
}

func (s *Supervisor) withDependencies(ctx context.Context, a *Agent, info toolchain.Software, force bool) *toolchain.Software {
	info.MissingDependencies = s.setup.MissingDependencies(ctx, a.Adapter.InstallSteps(), force)
	return &info
}

func (s *Supervisor) CompatibilityCatalog() toolchain.Catalog { return s.toolchain.Catalog() }

func (s *Supervisor) checkSoftware(a *Agent) error {
	mod, ok := a.Adapter.(agent.ToolchainModule)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.toolchain.CheckAdmission(ctx, mod.Toolchain())
}
