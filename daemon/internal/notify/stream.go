package notify

import (
	"context"
	"sync"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Stream is an in-process notification broadcaster that also implements Notifier.
// The daemon fans every emitted notification to it alongside the webhook push, so local
// clients (the web console, dev tooling) can watch notifications live over SSE without
// depending on any external delivery. It keeps a small replay ring so a just-connected client
// still sees the last few. Slow subscribers are dropped, never block a turn.
type Stream struct {
	mu     sync.Mutex
	subs   map[int]chan agent.Notification
	nextID int
	recent []agent.Notification
}

const streamReplay = 25

func NewStream() *Stream { return &Stream{subs: map[int]chan agent.Notification{}} }

// Notify implements Notifier: record into the replay ring + broadcast (non-blocking).
func (s *Stream) Notify(_ context.Context, n agent.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recent = append(s.recent, n)
	if len(s.recent) > streamReplay {
		s.recent = s.recent[len(s.recent)-streamReplay:]
	}
	for _, ch := range s.subs {
		select {
		case ch <- n:
		default: // drop for a slow subscriber rather than block the turn
		}
	}
	return nil
}

// Subscribe returns a live channel, the current replay buffer, and an unsubscribe func.
func (s *Stream) Subscribe() (<-chan agent.Notification, []agent.Notification, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	ch := make(chan agent.Notification, 16)
	s.subs[id] = ch
	replay := append([]agent.Notification(nil), s.recent...)
	return ch, replay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if c, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(c)
		}
	}
}
