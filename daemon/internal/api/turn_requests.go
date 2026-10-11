package api

import (
	"errors"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func turnRequestError(w http.ResponseWriter, err error) bool {
	var native *agent.NativeSessionError
	if errors.As(err, &native) {
		nativeSessionError(w, err)
		return true
	}
	if errors.Is(err, orchestrator.ErrInvalidTurnRequest) || errors.Is(err, orchestrator.ErrInputUnsupported) || errors.Is(err, orchestrator.ErrInputOptions) {
		badRequest(w, err.Error())
		return true
	}
	if errors.Is(err, session.ErrTurnRequestConflict) || errors.Is(err, orchestrator.ErrInputQueueFull) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return true
	}
	return false
}
