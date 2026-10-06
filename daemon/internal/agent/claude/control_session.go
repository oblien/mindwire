package claude

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Correlate runtime permission changes with Claude's acknowledgement, including rejections.
type controlSession struct {
	mu      sync.Mutex
	seq     int
	pending map[string]agent.Inbound
	inputs  map[string]agent.Inbound
	closed  bool
	cleanup []func()
}

func newControlSession() *controlSession {
	return &controlSession{pending: map[string]agent.Inbound{}, inputs: map[string]agent.Inbound{}}
}

func (s *controlSession) encode(in agent.Inbound) ([]byte, bool) {
	if in.Kind == "input" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			if in.Ack != nil {
				in.Ack <- agent.ErrInputClosed
			}
			return nil, false
		}
		// Claude echoes this UUID when consuming the queued user message. A
		// successful stdin write alone is not native acknowledgement.
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			if in.Ack != nil {
				in.Ack <- err
			}
			return nil, false
		}
		id[6], id[8] = id[6]&0x0f|0x40, id[8]&0x3f|0x80
		h := hex.EncodeToString(id[:])
		uuid := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
		var attachments []agent.Attachment
		if in.Input != nil {
			attachments = in.Input.Attachments
		}
		_, suffix, images, cleanup, err := materialize(agent.TurnOptions{Attachments: attachments})
		if err != nil {
			cleanup()
			if in.Ack != nil {
				in.Ack <- err
			}
			return nil, false
		}
		s.cleanup = append(s.cleanup, cleanup)
		message := userMessageBlocks(in.Text+suffix, images)
		message["uuid"] = uuid
		line, err := json.Marshal(message)
		if err != nil {
			if in.Ack != nil {
				in.Ack <- err
			}
			return nil, false
		}
		s.inputs[uuid] = in
		return line, true
	}
	if in.Kind == "set_permission_mode" || in.Kind == "set_model" {
		s.mu.Lock()
		s.seq++
		in.ControlID = fmt.Sprintf("mw-control-%d", s.seq)
		s.pending[in.ControlID] = in
		s.mu.Unlock()
	}
	return encodeInbound(in)
}

func (s *controlSession) parse(r io.Reader, emit agent.Emit) (agent.TurnResult, bool) {
	return parseStreamHooks(r, emit, s.response, s.user, s.result)
}

func (s *controlSession) user(env streamEnvelope, emit agent.Emit) {
	// Replays may include the initial prompt and native merged messages. UUIDs
	// identify our inputs without mistaking tool results or equal text for a send.
	s.mu.Lock()
	in, ok := s.inputs[env.UUID]
	delete(s.inputs, env.UUID)
	s.mu.Unlock()
	if ok {
		agent.AcknowledgeInput(in, nil, emit)
	}
}

func (s *controlSession) result(ev agent.Event, emit agent.Emit) bool {
	s.mu.Lock()
	more := len(s.inputs) > 0 && ev.Result != nil && !ev.Result.IsError
	if !more {
		s.closed = true
	}
	s.mu.Unlock()
	if more {
		// stream-json can emit a result before consuming its queued messages.
		// Keep stdin and the shared run alive; Claude decides when to consume them.
		emit(agent.Event{Type: agent.EventContinuation, Result: ev.Result,
			Continuation: &agent.ContinuationInfo{Reason: "queued_input"}})
	}
	return !more
}

func (s *controlSession) response(raw json.RawMessage, emit agent.Emit) {
	var response struct {
		RequestID string `json:"request_id"`
		Subtype   string `json:"subtype"`
		Error     string `json:"error"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return
	}
	s.mu.Lock()
	in, ok := s.pending[response.RequestID]
	delete(s.pending, response.RequestID)
	s.mu.Unlock()
	if !ok {
		return
	}
	var err error
	if response.Subtype == "error" {
		err = errors.New(agent.FirstNonEmpty(response.Error, "Claude rejected the setting change"))
		emit(agent.Event{Type: agent.EventInteraction, Interaction: &agent.Interaction{ID: response.RequestID, Kind: "error", Title: "Setting change failed", Detail: err.Error()}})
	} else {
		key := "model"
		if in.Kind == "set_permission_mode" {
			key = "permissionMode"
		}
		emit(agent.Event{Type: agent.EventStatus, Meta: map[string]any{key: in.Text}})
	}
	if in.Ack != nil {
		in.Ack <- err
	}
}

func (s *controlSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, in := range s.pending {
		if in.Ack != nil {
			in.Ack <- errors.New("Claude stopped before applying the setting")
		}
	}
	s.pending = map[string]agent.Inbound{}
	s.closed = true
	for _, in := range s.inputs {
		if in.Ack != nil {
			in.Ack <- errors.New("Claude closed before confirming delivery. Check the conversation before sending again.")
		}
	}
	s.inputs = map[string]agent.Inbound{}
	for _, cleanup := range s.cleanup {
		cleanup()
	}
	s.cleanup = nil
}
