package orchestrator

import (
	"context"
	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/runner"
	"github.com/oblien/mindwire/daemon/internal/surface"
)

func (s *Supervisor) SetSurfaces(service *surface.Service) {
	s.mu.Lock()
	s.surfaces = service
	s.mu.Unlock()
	service.SetApproval(s.approveDesktop)
}

// Service approvals use the same reducer, notification, response validation and
// saved transcript as native harness approvals. They never enter CLI ingress.
func (s *Supervisor) approveDesktop(ctx context.Context, actor surface.Actor, title string) error {
	id := "desktop-" + newID()
	interaction := agent.Interaction{ID: id, Kind: "approval", Title: title,
		Detail:        "The agent can see this desktop, type, use its clipboard and click until this session ends. You can take control from Desktop at any time.",
		Options:       []agent.Action{{ID: "allow", Label: "Allow for this session"}, {ID: "deny", Label: "Don't allow"}},
		NeedsResponse: true, Meta: map[string]any{"origin": "surface", "surfaceId": "desktop"}}
	response := make(chan agent.InteractionResponse, 1)
	s.mu.Lock()
	emit := s.serviceEmit[actor.RunID]
	if emit == nil || s.cancels[actor.RunID] == nil {
		s.mu.Unlock()
		return &surface.Error{Code: "run_ended", Message: "This agent run is no longer active."}
	}
	if s.serviceReplies[actor.RunID] == nil {
		s.serviceReplies[actor.RunID] = map[string]chan agent.InteractionResponse{}
	}
	if s.pending[actor.RunID] == nil {
		s.pending[actor.RunID] = map[string]agent.Interaction{}
	}
	s.pending[actor.RunID][id] = interaction
	s.serviceReplies[actor.RunID][id] = response
	s.mu.Unlock()
	emit(agent.Event{Type: agent.EventInteraction, Interaction: &interaction})
	decision := "interrupted"
	defer func() {
		s.mu.Lock()
		delete(s.pending[actor.RunID], id)
		delete(s.serviceReplies[actor.RunID], id)
		s.mu.Unlock()
		resolved := interaction
		resolved.NeedsResponse = false
		resolved.Meta = map[string]any{"origin": "surface", "surfaceId": "desktop", "decision": decision}
		emit(agent.Event{Type: agent.EventInteraction, Interaction: &resolved})
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case reply := <-response:
		decision = reply.Decision
		if decision != "allow" {
			return &surface.Error{Code: "denied", Message: "Desktop access was not approved."}
		}
		return nil
	}
}

func (s *Supervisor) desktopTurn(ctx context.Context, a *Agent, turn runner.Turn) (runner.Turn, func(), error) {
	s.mu.Lock()
	service := s.surfaces
	s.mu.Unlock()
	if service == nil {
		return turn, func() {}, nil
	}
	actor := surface.Actor{Kind: "agent", Name: a.ID(), RunID: turn.RunID, ChatID: turn.ChatID}
	overlay, env, cleanup, err := service.BindRun(ctx, actor, a.ID(), turn.Options.MCPServers)
	if err != nil {
		return turn, func() {}, err
	}
	settings, err := surface.PermissionSettings(a.ID(), turn.Options.ClaudeSettings)
	if err != nil {
		cleanup()
		return turn, func() {}, err
	}
	turn.Options.MCPServers = overlay
	turn.Options.ClaudeSettings = settings
	turn.Environment = env
	turn.AttachEmitter = func(emit agent.Emit) func() {
		s.mu.Lock()
		s.serviceEmit[turn.RunID] = emit
		s.mu.Unlock()
		return func() {
			s.mu.Lock()
			delete(s.serviceEmit, turn.RunID)
			delete(s.serviceReplies, turn.RunID)
			s.mu.Unlock()
		}
	}
	return turn, cleanup, nil
}
