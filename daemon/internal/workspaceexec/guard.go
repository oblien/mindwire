package workspaceexec

import (
	"sync"

	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

// SetMutationGuard shares admission with turns and project transactions. Configure
// it before serving requests; the release lasts until a command/terminal exits.
func (s *Service) SetMutationGuard(gate *sync.Mutex, check func(string) error) {
	s.gate, s.checkMutation = gate, check
}

func (s *Service) beginMutation(path string) (func(), error) {
	canonical, err := workspacepath.Canonical(path)
	if err != nil {
		return nil, err
	}
	if s.gate != nil {
		s.gate.Lock()
		defer s.gate.Unlock()
	}
	if s.checkMutation != nil {
		if err = s.checkMutation(canonical); err != nil {
			return nil, err
		}
	}
	s.activityMu.Lock()
	if s.activePaths == nil {
		s.activePaths = map[string]int{}
	}
	s.activePaths[canonical]++
	s.activityMu.Unlock()
	return sync.OnceFunc(func() {
		s.activityMu.Lock()
		defer s.activityMu.Unlock()
		s.activePaths[canonical]--
		if s.activePaths[canonical] == 0 {
			delete(s.activePaths, canonical)
		}
	}), nil
}

func (s *Service) BusyPath(path string) bool {
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	for active := range s.activePaths {
		if workspacepath.Overlaps(path, active) {
			return true
		}
	}
	return false
}
