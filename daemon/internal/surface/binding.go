package surface

import (
	"context"
	"encoding/json"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"time"
)

// Credentials is the daemon's existing private credential store, never registry metadata.
type Credentials interface {
	Get(string) string
	Set(string, string) error
}

const bindingKey = "#surface:oblien"

func NewConfigured(db *registry.Store, credentials Credentials) (*Service, error) {
	s, err := New(db)
	if err != nil {
		return nil, err
	}
	if raw := credentials.Get(bindingKey); raw != "" {
		var binding Binding
		if json.Unmarshal([]byte(raw), &binding) == nil && binding.RegistryID == db.Identity() {
			if provider, err := NewOblien(binding); err == nil {
				_ = s.Configure(provider)
			} else {
				s.mu.Lock()
				s.snapshot.State = "needs_authorization"
				s.snapshot.Error = asError(err)
				s.mu.Unlock()
			}
		}
	}
	return s, nil
}

func (s *Service) Bind(binding Binding, credentials Credentials) error {
	if binding.RegistryID != s.db.Identity() {
		return problem("workspace_mismatch", "The workspace service changed. Reload the workspace before connecting.")
	}
	if _, err := NewOblien(binding); err != nil {
		return err
	}
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	previous := s.provider
	old, ok := previous.(*Oblien)
	s.mu.Unlock()
	if ok && old.binding.WorkspaceID != binding.WorkspaceID {
		return problem("workspace_mismatch", "This service is already bound to another workspace.")
	}
	// A second phone can request its own VNC grant while the agent keeps using the
	// still-valid service grant. Refreshing authorization must not steal control.
	if ok && time.Until(old.ExpiresAt()) > 5*time.Minute {
		binding.Connection = old.binding.Connection
	}
	encoded, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	if err := credentials.Set(bindingKey, string(encoded)); err != nil {
		return err
	}
	if ok && old.binding.Connection == binding.Connection {
		old.binding.GatewayToken = binding.GatewayToken
		return nil
	}
	provider, err := NewOblien(binding)
	if err != nil {
		return err
	}
	if previous != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = previous.Release(ctx)
		cancel()
		_ = previous.Close()
	}
	s.mu.Lock()
	s.provider = provider
	s.snapshot.Geometry = nil
	s.snapshot.Error = nil
	s.snapshot.State = "disconnected"
	expires := provider.ExpiresAt()
	s.snapshot.AuthorizationExpiresAt = &expires
	s.signalLocked()
	s.mu.Unlock()
	return nil
}
