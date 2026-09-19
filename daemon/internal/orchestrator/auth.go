package orchestrator

import (
	"context"
	"errors"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

var ErrAuthBusy = errors.New("wait for this agent's running chats or installation to finish before signing out")
var ErrAuthDisconnected = errors.New("agent is signed out; connect an account to continue")

// Logout shares turn/setup admission. Credentials cannot be removed from a
// running chat, and a new turn cannot slip in while native sign-out is pending.
func (s *Supervisor) Logout(ctx context.Context, a *Agent) (agent.AuthStatus, error) {
	s.mu.Lock()
	if s.serviceUpdatingLocked() {
		s.mu.Unlock()
		return agent.AuthStatus{}, ErrServiceUpdating
	}
	if s.activeAgents[a.ID()] > 0 || s.setup.Status(a.ID()).Running || s.authChanging[a.ID()] {
		s.mu.Unlock()
		return agent.AuthStatus{}, ErrAuthBusy
	}
	s.authChanging[a.ID()] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.authChanging, a.ID()); s.mu.Unlock() }()
	logout, ok := a.Auth.(agent.AuthLogoutModule)
	if !ok {
		return agent.AuthStatus{}, errors.New("this agent does not support sign-out")
	}
	if err := logout.Logout(ctx); err != nil {
		return agent.AuthStatus{}, err
	}
	return a.Auth.Status(ctx), nil
}

func (s *Supervisor) authAdmissionLocked(a *Agent) error {
	if s.authChanging[a.ID()] {
		return ErrAuthBusy
	}
	if managed, ok := a.Auth.(interface{ Disconnected() bool }); ok && managed.Disconnected() {
		return ErrAuthDisconnected
	}
	return nil
}
