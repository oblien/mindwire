package orchestrator

import "github.com/oblien/mindwire/daemon/internal/agent"

// WithAuthSource is native-host configuration; no API route accepts a source path.
func WithAuthSource(source *agent.AuthSource) Option {
	return func(s *Supervisor) { s.authSource = source }
}
