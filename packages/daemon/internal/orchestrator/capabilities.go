package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

// RunCapabilities prefers the actual native handshake over catalog defaults.
// The same snapshot is replayed to clients after reconnecting to the run.
func (s *Supervisor) RunCapabilities(runID string) agent.Capabilities {
	if snapshot, ok := s.hub.Snapshot(runID); ok && snapshot.Capabilities != nil {
		return *snapshot.Capabilities
	}
	if run, ok := s.store.GetRun(runID); ok {
		if a, ok := s.Resolve(run.Agent); ok {
			return a.Capabilities()
		}
	}
	return agent.Capabilities{}
}

func (s *Supervisor) ConfirmModel(ctx context.Context, runID, model string) error {
	if !s.RunCapabilities(runID).SetModel {
		return fmt.Errorf("this session applies model changes on the next turn")
	}
	ack := make(chan error, 1)
	s.mu.Lock()
	sent := s.trySend(runID, agent.Inbound{Kind: "set_model", Text: model, Ack: ack})
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
		return fmt.Errorf("model change was not confirmed: %w", ctx.Err())
	}
}
