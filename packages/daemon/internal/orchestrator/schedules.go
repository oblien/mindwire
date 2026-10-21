package orchestrator

import (
	"errors"
	"fmt"

	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/session"
)

func (s *Supervisor) StartScheduledTurn(chatID, prompt, requestID string) (session.Run, error) {
	service := s.BotService()
	if service == nil {
		return session.Run{}, errors.New("scheduled conversations are unavailable")
	}
	run, err := service.ScheduledTurn(chatID, func(chat registry.Chat, profile registry.Agent, project registry.Project) (session.Run, error) {
		a, ok := s.Resolve(profile.AgentType)
		if !ok {
			return session.Run{}, fmt.Errorf("%w: this chat's AI connection is unavailable", registry.ErrInvalid)
		}
		return s.StartTurnChecked(a, StartTurnInput{ChatID: chat.ID, CWD: project.Path, Message: prompt, RequestID: requestID, scheduled: true})
	})
	if errors.Is(err, ErrChatBusy) || errors.Is(err, ErrServiceUpdating) || errors.Is(err, registry.ErrConflict) {
		return run, fmt.Errorf("%w: %v", registry.ErrScheduleDeferred, err)
	}
	return run, err
}

func (s *Supervisor) ScheduledRun(id string) (session.Run, bool) {
	// The host already reconciles interrupted runs once at startup. Read the
	// authoritative run, including that outcome, without another lifecycle.
	return s.store.GetRun(id)
}
