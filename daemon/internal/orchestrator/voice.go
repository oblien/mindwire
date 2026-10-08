package orchestrator

import "github.com/oblien/mindwire/daemon/internal/agent"

func (s *Supervisor) Voice(runID, clientID string) (*agent.VoiceBridge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	voice := s.voices[runID]
	return voice, voice != nil && voice.ClientID == clientID
}
