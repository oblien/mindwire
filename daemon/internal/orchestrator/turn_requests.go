package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
)

const TurnRequestVersion = 1

var ErrInvalidTurnRequest = errors.New("requestId must contain 1 to 128 letters, numbers, hyphens or underscores")

func (s *Supervisor) turnDigest(a *Agent, req StartTurnInput, mode string, resolve *ResolveOptions) (string, error) {
	if req.RequestID == "" {
		return "", nil
	}
	if len(req.RequestID) > 128 || strings.IndexFunc(req.RequestID, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) >= 0 {
		return "", ErrInvalidTurnRequest
	}
	// Credential rotation does not change the meaning of a retried operation.
	// Never store Git credentials or the prompt in the receipt.
	value := struct {
		Agent, Chat, Message, CWD, Mode string
		Options                         agent.TurnOptions
		Resolve                         *ResolveOptions
	}{a.ID(), req.ChatID, req.Message, s.activePath(req.CWD), mode, req.Options, resolve}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// Called under the supervisor lock, before busy/auth/update admission checks.
func (s *Supervisor) replayTurn(a *Agent, req StartTurnInput, mode string, resolve *ResolveOptions) (session.Run, bool, string, error) {
	digest, err := s.turnDigest(a, req, mode, resolve)
	if err != nil {
		return session.Run{}, false, "", err
	}
	run, found, err := s.store.TurnRequest(req.RequestID, digest)
	return run, found, digest, err
}
