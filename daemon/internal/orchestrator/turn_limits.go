package orchestrator

import (
	"context"
	"fmt"
	"time"
)

type Option func(*Supervisor)

// WithTurnTimeout opts into a wall-clock limit. By default turns run until the
// native harness finishes or an explicit Stop/shutdown cancels them. Viewer
// disconnects never cancel a turn. Non-positive durations leave it unlimited.
func WithTurnTimeout(limit time.Duration) Option {
	return func(s *Supervisor) { s.turnTimeout = limit }
}

func (s *Supervisor) turnContext(parent context.Context) (context.Context, context.CancelFunc) {
	if s.turnTimeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeoutCause(parent, s.turnTimeout, fmt.Errorf(
		"Mindwire stopped this turn at its configured time limit (%s). Increase TURN_TIMEOUT or set it to 0, then continue this chat.", s.turnTimeout))
}
