package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// ConfirmPermissionMode returns only after the harness has applied or rejected the mode.
func (s *Supervisor) ConfirmPermissionMode(ctx context.Context, runID, mode string) error {
	run, ok := s.store.GetRun(runID)
	if !ok {
		return ErrInteractionNotPending
	}
	a, ok := s.Resolve(run.Agent)
	if !ok || !a.Adapter.Capabilities().SetPermissionMode {
		return fmt.Errorf("this agent applies permission changes on the next turn")
	}
	valid := false
	for _, section := range a.Adapter.Settings().Sections {
		for _, field := range section.Fields {
			if field.Canon != agent.CanonPermissionMode {
				continue
			}
			for _, option := range field.Options {
				if option.Value == mode && mode != "" {
					valid = true
				}
			}
		}
	}
	if !valid {
		return fmt.Errorf("unsupported permission mode %q", mode)
	}
	ack := make(chan error, 1)
	s.mu.Lock()
	sent := s.trySend(runID, agent.Inbound{Kind: "set_permission_mode", Text: mode, Ack: ack})
	s.mu.Unlock()
	if !sent {
		return ErrInteractionNotPending
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return fmt.Errorf("permission change was not confirmed: %w", ctx.Err())
	}
}
