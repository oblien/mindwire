package api

import (
	"errors"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func turnRequestError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, orchestrator.ErrInvalidTurnRequest) {
		badRequest(w, err.Error())
		return true
	}
	if errors.Is(err, session.ErrTurnRequestConflict) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return true
	}
	return false
}
