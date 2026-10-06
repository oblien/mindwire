package orchestrator

import (
	"errors"
	"reflect"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
)

const QueuedInputVersion = 1

var (
	ErrInputUnsupported = errors.New("this harness does not support queued messages")
	ErrInputOptions     = errors.New("a running conversation accepts messages and attachments; change turn settings after it finishes")
	ErrInputQueueFull   = errors.New("too many queued messages; wait for the agent to accept them")
)

// SendInputChecked is the run-addressed counterpart of queueIfRunning. It
// preserves the old endpoint's active-run contract and adds durable retries.
func (s *Supervisor) SendInputChecked(runID, text, requestID string, attachments []agent.Attachment) (session.Run, error) {
	if !agent.HasUserInput(text, agent.TurnOptions{Attachments: attachments}) {
		return session.Run{}, ErrInvalidTurnRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.store.GetRun(runID)
	if !ok {
		return session.Run{}, ErrChatBusy
	}
	a, ok := s.Resolve(run.Agent)
	if !ok {
		return session.Run{}, ErrInputUnsupported
	}
	if requestID == "" {
		requestID = newID()
	}
	req := StartTurnInput{RequestID: requestID, ChatID: run.ChatID, Message: text, CWD: s.store.ChatCWD(run.ChatID),
		QueueIfRunning: true, Options: agent.TurnOptions{Attachments: attachments}}
	if cwd, active := s.active[run.ChatID]; active {
		req.CWD = cwd
	}
	replay, found, digest, err := s.replayTurn(a, req, "turn", nil)
	if found || err != nil {
		return replay, err
	}
	return s.enqueueRunInputLocked(a, run, req, digest)
}

type inputDelivery struct {
	input agent.UserInput
	ack   chan error
	sent  bool
}

// Only active runs have this small receipt list. There is no polling, background
// queue worker, or idle native process. The harness owns consumption scheduling.
type runInputQueue struct {
	accepting  bool
	cancelled  bool
	deliveries []*inputDelivery
}

// The caller holds s.mu, just like ordinary turn admission. Persist and publish
// before opening native ingress so even an immediate acknowledgement cannot race.
func (s *Supervisor) enqueueRunInputLocked(a *Agent, run session.Run, req StartTurnInput, digest string) (session.Run, error) {
	if !a.Adapter.Capabilities().QueuedInput {
		return session.Run{}, ErrInputUnsupported
	}
	q := s.inputQueues[run.ID]
	if q == nil || !q.accepting || q.cancelled || run.Agent != a.ID() || run.Kind != "" || s.active[run.ChatID] != s.activePath(req.CWD) {
		return session.Run{}, ErrChatBusy
	}
	options := req.Options
	options.Attachments = nil
	if !reflect.DeepEqual(options, agent.TurnOptions{}) {
		return session.Run{}, ErrInputOptions
	}
	ch := s.inputs[run.ID]
	if ch != nil && len(ch) == cap(ch) {
		return session.Run{}, ErrInputQueueFull
	}
	pending := 0
	for _, input := range run.Inputs {
		if input.Status == "queued" {
			pending++
		}
	}
	if pending >= 16 {
		return session.Run{}, ErrInputQueueFull
	}
	input := agent.UserInput{ID: req.RequestID, Text: req.Message, Attachments: req.Options.Attachments, CreatedAt: nowISO(), Status: "queued"}
	updated, err := s.store.SaveRunInput(run.ID, input, digest)
	if err != nil {
		return session.Run{}, err
	}
	s.hub.Publish(run.ID, agent.Event{Type: agent.EventInput, Input: &input})
	delivery := &inputDelivery{input: input, ack: make(chan error, 1)}
	q.deliveries = append(q.deliveries, delivery)
	if ch != nil {
		delivery.sent = true
		ch <- agent.Inbound{Kind: "input", Text: input.Text, Input: &delivery.input, Ack: delivery.ack}
	}
	return updated, nil
}

func (s *Supervisor) closeRunIngressLocked(runID string) {
	if ch := s.inputs[runID]; ch != nil {
		delete(s.inputs, runID)
		close(ch)
		for in := range ch {
			if in.Ack != nil {
				in.Ack <- agent.ErrInputClosed
			}
		}
	}
}

func (s *Supervisor) publishInput(runID string, input agent.UserInput) error {
	if err := s.store.UpdateRunInput(runID, input); err != nil {
		return err
	}
	s.hub.Publish(runID, agent.Event{Type: agent.EventInput, Input: &input})
	return nil
}

// After the adapter and its writer have stopped, only explicit not-submitted
// acknowledgements can become a continuation. Ambiguous failures remain visible
// and never replay automatically. Admission and the final empty check are atomic.
func (s *Supervisor) advanceRunInputs(runID string, continueRun bool, failure string) (*agent.UserInput, chan agent.Inbound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.inputQueues[runID]
	if q == nil {
		return nil, nil, nil
	}
	s.closeRunIngressLocked(runID)
	var pending []*inputDelivery
	saved, _ := s.store.GetRun(runID)
	statuses := make(map[string]string, len(saved.Inputs))
	for _, input := range saved.Inputs {
		statuses[input.ID] = input.Status
	}
	for _, delivery := range q.deliveries {
		// A native user echo is authoritative even if its RPC response was
		// lost afterward. Never turn consumed input back into a failed draft.
		if statuses[delivery.input.ID] == "accepted" {
			continue
		}
		err := agent.ErrInputClosed
		if delivery.sent {
			select {
			case err = <-delivery.ack:
			default:
				err = errors.New("The agent closed before confirming delivery. Check the conversation before sending again.")
			}
		}
		if err == nil {
			err = errors.New("The agent stopped before confirming this message in the conversation. Check history before sending again.")
		}
		if errors.Is(err, agent.ErrInputClosed) && continueRun && !q.cancelled {
			pending = append(pending, delivery)
			continue
		}
		input := delivery.input
		input.Status, input.Error = "failed", err.Error()
		if q.cancelled {
			input.Status = "cancelled"
		}
		if errors.Is(err, agent.ErrInputClosed) {
			input.Error = agent.FirstNonEmpty(failure, "The run ended before this message was sent.")
			if q.cancelled {
				input.Status = "cancelled"
			}
		}
		if err := s.publishInput(runID, input); err != nil {
			return nil, nil, err
		}
	}
	q.deliveries = nil
	if len(pending) == 0 {
		q.accepting = false
		return nil, nil, nil
	}
	first := pending[0].input
	first.Status = "accepted"
	if err := s.store.UpdateRunInput(runID, first); err != nil {
		return nil, nil, err
	}
	ch := make(chan agent.Inbound, 16)
	s.inputs[runID] = ch
	s.pending[runID] = map[string]agent.Interaction{}
	for _, delivery := range pending[1:] {
		delivery.ack, delivery.sent = make(chan error, 1), true
		q.deliveries = append(q.deliveries, delivery)
		ch <- agent.Inbound{Kind: "input", Text: delivery.input.Text, Input: &delivery.input, Ack: delivery.ack}
	}
	return &first, ch, nil
}
