package orchestrator

import (
	"context"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/session"
)

type GitPreparation func(context.Context, string, string, *gitaccess.Auth) (*gitaccess.Lease, error)

func (s *Supervisor) SetGitPreparation(prepare GitPreparation) {
	s.mu.Lock()
	s.prepareGit = prepare
	s.mu.Unlock()
}

func (s *Supervisor) StartTurnChecked(a *Agent, req StartTurnInput) (session.Run, error) {
	return s.startChecked(a, req, false)
}

func (req *StartTurnInput) gitEnvironment() map[string]string {
	if req.gitLease == nil {
		return nil
	}
	// Surface preparation adds per-child environment values. Never mutate the
	// shared parent lease during an autonomous resolve loop.
	env := map[string]string{}
	for k, v := range req.gitLease.Environment {
		env[k] = v
	}
	return env
}

func (req *StartTurnInput) closeGit() {
	if req.gitLease != nil {
		req.gitLease.Close()
	}
}
