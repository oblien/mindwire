package orchestrator

import (
	"errors"
	"time"

	"github.com/oblien/mindwire/daemon/internal/setup"
)

var ErrAgentBusy = errors.New("agent has running chats; wait for them to finish before installing or updating")

// Setup owns the same harness lock as turn admission. An HTTP client, SDK caller, or second
// persona cannot start a turn during installation, or replace a CLI beneath an existing turn.
// Repeated setup/update requests still attach to the one detached job.
func (s *Supervisor) Setup(a *Agent, force bool) (setup.Status, error) {
	steps, err := setup.Plan(a.Adapter.InstallSteps())
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

func (s *Supervisor) SetupStatus(a *Agent) setup.Status { return s.setup.Status(a.ID()) }

// StartConflict describes a failed admission for both transports. Admission itself happens under
// s.mu in start/StartResolve; this read only supplies a useful retry reason to the client.
func (s *Supervisor) StartConflict(a *Agent) string {
	if s.setup.Status(a.ID()).Running {
		return "agent setup is still running; wait for the CLI to be ready"
	}
	return "a turn is already running for this chat"
}
