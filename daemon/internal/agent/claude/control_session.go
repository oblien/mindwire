package claude

import (
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
}

func newControlSession() *controlSession { return &controlSession{pending: map[string]agent.Inbound{}} }

func (s *controlSession) encode(in agent.Inbound) ([]byte, bool) {
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
	return parseStreamControlled(r, emit, s.response)
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
}
