package integrations

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/registry"
)

// Claim hands one verified event to an authenticated host. Expired leases may be reclaimed;
// hosts must use ID as their own operation idempotency key before performing side effects.
func (s *Service) Claim(id string) (*HostEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(id)
	if err != nil {
		return nil, err
	}
	if s.closed || !c.Enabled || c.Dispatch != "host" || c.Mode == "notifications" {
		return nil, errForbidden
	}
	rows, err := s.db.IntegrationEvents(id, true)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	for _, raw := range rows {
		var e envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		if !s.allowed(c, e) {
			// Policy changes must not strand disallowed work in the bounded queue.
			// Do not send a response to a conversation that is no longer allowed.
			e.State, e.Error = "failed", "conversation is no longer allowed"
			if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
				return nil, err
			}
			continue
		}
		if e.HostReply != nil || e.HostLeaseUntil > now {
			continue
		}
		e.HostLease, e.HostLeaseUntil, e.HostRevision = rand.Text(), now+90, c.Revision
		e.State = "running"
		if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
			return nil, err
		}
		return &HostEvent{ID: e.ID, IntegrationID: id, WorkspaceID: e.Workspace, ChannelID: e.ChannelID, UserID: e.UserID, ThreadID: e.ThreadID, Text: e.Text, Command: e.Command, Lease: e.HostLease, Revision: c.Revision}, nil
	}
	return nil, nil
}

// Complete persists the result before delivery. Repeating the same completion never executes
// work again. Configuration changes or expired claims cannot publish a stale/private response.
func (s *Service) Complete(id, eventID, lease, text string) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(id)
	if err != nil {
		return Receipt{}, err
	}
	if s.closed || !c.Enabled || c.Dispatch != "host" {
		return Receipt{}, errForbidden
	}
	if len(text) > 12000 || lease == "" {
		return Receipt{}, invalid("a claim and a bounded reply are required")
	}
	var e envelope
	if err := s.db.SurfaceGet(eventKind, eventID, &e); err != nil {
		return Receipt{}, err
	}
	if e.IntegrationID != id || !s.allowed(c, e) || c.Revision != e.HostRevision || subtle.ConstantTimeCompare([]byte(e.HostLease), []byte(lease)) != 1 {
		return Receipt{}, errForbidden
	}
	if e.HostReply != nil {
		if *e.HostReply != text {
			return Receipt{}, registry.ErrConflict
		}
		return e.Receipt, nil
	}
	if e.HostLeaseUntil <= time.Now().Unix() {
		return Receipt{}, registry.ErrConflict
	}
	e.HostReply = &text
	e.State = "queued"
	if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
		return Receipt{}, err
	}
	s.signal()
	return e.Receipt, nil
}
