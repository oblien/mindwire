package orchestrator

import (
	"context"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/nativesessions"
	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/runner"
)

func (s *Supervisor) NativeSessions() *nativesessions.Service { return s.nativeSessions }

// CheckNativeChatMutation protects native transcript files even when Mindwire
// did not launch their owner. Call after hydrating the registry's session mapping.
func (s *Supervisor) CheckNativeChatMutation(ctx context.Context, chatID string) error {
	for _, a := range s.agents {
		if _, ok := a.Adapter.(agent.NativeSessionModule); !ok || s.store.PendingFork(a.ID(), chatID) != nil {
			continue
		}
		sid := s.store.Session(a.ID(), chatID)
		if sid == "" {
			continue
		}
		value, err := s.nativeSessions.Activity(ctx, a.Adapter, agent.NativeSessionQuery{SessionID: sid, CWD: agent.FirstNonEmpty(s.store.ChatCWD(chatID), s.cwd)})
		if err != nil {
			return err
		}
		if value.Owner == "native" {
			return agent.NativeError("native_session_owned", "Close this conversation in its terminal before deleting or forking it.")
		}
	}
	return nil
}

func (s *Supervisor) checkNativeSession(a *Agent, req StartTurnInput, control bool) (bool, error) {
	_, ok := a.Adapter.(agent.NativeSessionModule)
	if !ok || req.Options.ForkOnResume || s.store.PendingFork(a.ID(), req.ChatID) != nil {
		return false, nil
	}
	sid := agent.FirstNonEmpty(req.Options.SessionID, s.store.Session(a.ID(), req.ChatID))
	if sid == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	activity, err := s.nativeSessions.Activity(ctx, a.Adapter, agent.NativeSessionQuery{SessionID: sid, CWD: agent.FirstNonEmpty(req.CWD, s.store.ChatCWD(req.ChatID), s.cwd)})
	if err != nil || activity.Owner != "native" {
		return false, err
	}
	if req.scheduled {
		return true, registry.ErrScheduleDeferred
	}
	if !activity.Capabilities.Input {
		return true, agent.NativeActionUnsupported("sending")
	}
	if control {
		return true, agent.NativeActionUnsupported("this command")
	}
	if err := s.checkBotNativeOwner(req.ChatID); err != nil {
		return true, err
	}
	return true, nativeOverrides(req.Options)
}

func (s *Supervisor) checkBotNativeOwner(chatID string) error {
	s.mu.Lock()
	service := s.bots
	s.mu.Unlock()
	if service != nil && service.IsBotChat(chatID) {
		return agent.NativeError("native_settings_owned", "This bot uses its saved Mindwire instructions and tools. Close its active terminal session before continuing here.")
	}
	return nil
}

func nativeOverrides(o agent.TurnOptions) error {
	if o.Command != nil || o.Voice != nil || len(o.Settings) > 0 || o.SystemPrompt != "" || o.ContextInstructions != "" || len(o.MCPServers) > 0 || len(o.OutputSchema) > 0 || len(o.Subagents) > 0 || len(o.ClaudeSettings) > 0 {
		return agent.NativeError("native_settings_owned", "This live conversation uses its terminal's settings and tools. Send a message without startup overrides, or change those settings in its terminal.")
	}
	return nil
}

func (s *Supervisor) prepareTurn(ctx context.Context, a *Agent, turn runner.Turn) (runner.Turn, func(), error) {
	sid := agent.FirstNonEmpty(turn.Options.SessionID, s.store.Session(a.ID(), turn.ChatID))
	if _, ok := a.Adapter.(agent.NativeSessionModule); ok && sid != "" && !turn.Options.ForkOnResume && s.store.PendingFork(a.ID(), turn.ChatID) == nil {
		activity, err := s.nativeSessions.Activity(ctx, a.Adapter, agent.NativeSessionQuery{SessionID: sid, CWD: agent.FirstNonEmpty(turn.CWD, s.store.ChatCWD(turn.ChatID), s.cwd)})
		if err != nil {
			return turn, func() {}, err
		}
		if activity.Owner == "native" {
			if err := s.checkBotNativeOwner(turn.ChatID); err != nil {
				return turn, func() {}, err
			}
			if !activity.Capabilities.Input {
				return turn, func() {}, agent.NativeActionUnsupported("sending")
			}
			if err := nativeOverrides(turn.Options); err != nil {
				return turn, func() {}, err
			}
			turn.NativeRun = func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
				return s.nativeSessions.RunTurn(ctx, a.Adapter, in, emit)
			}
			// Its existing owner already has authentication, environment and tools.
			// In particular, don't inject another MCP configuration into that owner.
			return turn, func() {}, nil
		}
	}
	turn = s.attachServiceEmitter(turn)
	turn, desktopCleanup, err := s.desktopTurn(ctx, a, turn)
	if err != nil {
		return turn, desktopCleanup, err
	}
	s.mu.Lock()
	service := s.bots
	s.mu.Unlock()
	if service == nil {
		return turn, desktopCleanup, nil
	}
	turn, botCleanup, err := service.Bind(ctx, a.Adapter, turn)
	if err != nil {
		desktopCleanup()
		return turn, func() {}, err
	}
	return turn, func() { botCleanup(); desktopCleanup() }, nil
}
