package orchestrator

import (
	"errors"
	"reflect"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
)

var ErrChatBusy = errors.New("this chat or harness is busy; try again when it is ready")
var ErrInteractionSending = errors.New("this answer is already being sent")

// InteractionContext revalidates a saved chat's harness and project before an
// async answer starts more work. The workspace registry implements this port.
type InteractionContext interface {
	InteractionCWD(chatID, agentType, fallbackCWD string) (string, error)
}

func (s *Supervisor) SetInteractionContext(provider InteractionContext) {
	s.mu.Lock()
	s.interactionContext = provider
	s.mu.Unlock()
}

func (s *Supervisor) respondMessageQuestion(runID string, reply agent.InteractionResponse) error {
	key := runID + "\x1f" + reply.InteractionID
	s.mu.Lock()
	q, ok := s.store.MessageQuestion(runID, reply.InteractionID)
	if !ok {
		s.mu.Unlock()
		return ErrInteractionNotPending
	}
	reply, err := q.Interaction.ValidateResponse(reply)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if q.Interaction.Response != nil {
		s.mu.Unlock()
		// Retrying a lost HTTP acknowledgement must not send the answer twice.
		if reflect.DeepEqual(*q.Interaction.Response, reply) {
			return nil
		}
		return ErrInteractionNotPending
	}
	if !q.Interaction.NeedsResponse {
		s.mu.Unlock()
		return ErrInteractionNotPending
	}
	if s.answering[key] {
		s.mu.Unlock()
		return ErrInteractionSending
	}
	s.answering[key] = true
	provider := s.interactionContext
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.answering, key); s.mu.Unlock() }()

	source, ok := s.store.GetRun(runID)
	if !ok {
		return ErrInteractionNotPending
	}
	a, ok := s.Resolve(source.Agent)
	if !ok || !a.Adapter.Capabilities().Input {
		return errors.New("this harness cannot receive question replies")
	}
	cwd := agent.FirstNonEmpty(s.store.ChatCWD(q.ChatID), s.cwd)
	if provider != nil {
		cwd, err = provider.InteractionCWD(q.ChatID, source.Agent, cwd)
		if err != nil {
			return err
		}
	}
	ref := session.InteractionReply{RunID: runID, Response: reply}
	text := q.Interaction.AnswerText(reply)
	for {
		s.mu.Lock()
		latest, _ := s.store.LatestRun(q.ChatID)
		_, busy := s.active[q.ChatID]
		closed := s.runClosed[latest.ID]
		if !busy {
			s.mu.Unlock()
			_, err := s.startChecked(a, StartTurnInput{ChatID: q.ChatID, Message: text, CWD: cwd, reply: &ref}, false)
			return err
		}
		if latest.Agent != source.Agent || closed == nil {
			s.mu.Unlock()
			return ErrChatBusy
		}
		if latest.Status != "running" {
			s.mu.Unlock()
			<-closed // completion is committing history; then resume the same native session
			continue
		}
		ack := make(chan error, 1)
		queued := s.trySend(latest.ID, agent.Inbound{Kind: "input", Text: text, Ack: ack})
		s.mu.Unlock()
		if !queued {
			return ErrChatBusy
		}
		select {
		case err = <-ack:
		case <-closed:
			select {
			case err = <-ack:
			default:
				err = errors.New("the harness closed before acknowledging your answer; please retry")
			}
		}
		if errors.Is(err, agent.ErrInputClosed) {
			<-closed
			continue
		}
		if err != nil {
			return err
		}
		message := session.Message{ID: newID(), ChatID: q.ChatID, Role: "user", Text: text, CreatedAt: nowISO()}
		if err := s.store.CommitInteractionReply(ref, latest.ID, message, nil); err != nil {
			return err
		}
		resolved := q.Interaction
		resolved.NeedsResponse = false
		resolved.Response = &reply
		s.recordPending(runID, resolved)
		s.hub.Publish(runID, agent.Event{Type: agent.EventInteraction, Interaction: &resolved})
		return nil
	}
}
