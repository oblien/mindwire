package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/oblien/mindwire/daemon/internal/projects"
)

func (a *API) requireProjects(w http.ResponseWriter) bool {
	if a.projects != nil {
		return true
	}
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "project operations are unavailable; update the daemon"})
	return false
}

func (a *API) projectStart(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjects(w) {
		return
	}
	var req projects.Request
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid project request")
		return
	}
	o, err := a.projects.Start(req)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, o)
}

func (a *API) projectRemoveFiles(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjects(w) {
		return
	}
	var req projects.RemoveRequest
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid project removal")
		return
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	o, err := a.projects.RemoveFiles(r.PathValue("id"), req)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, o)
}
func (a *API) projectOperations(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjects(w) {
		return
	}
	operations, err := a.projects.List(r.URL.Query().Get("active") == "true")
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operations": operations})
}
func (a *API) projectOperation(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjects(w) {
		return
	}
	o, err := a.projects.Get(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}
func (a *API) projectCancel(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjects(w) {
		return
	}
	o, err := a.projects.Cancel(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}
func (a *API) projectRetry(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjects(w) {
		return
	}
	var req struct {
		Auth projects.Auth `json:"auth,omitempty"`
	}
	if r.ContentLength != 0 {
		if err := decode(w, r, &req); err != nil {
			badRequest(w, "invalid project retry")
			return
		}
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	o, err := a.projects.Retry(r.PathValue("id"), req.Auth)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// Each event is a complete durable snapshot, not a replay of old progress. SSE
// disconnect only ends this observer; the daemon's project operation keeps running.
func (a *API) projectOperationStream(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjects(w) {
		return
	}
	id := r.PathValue("id")
	changed := a.projects.Changes()
	o, err := a.projects.Get(id)
	if err != nil {
		workspaceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	var sequence int64 = -1
	for {
		if o.Sequence != sequence {
			data, _ := json.Marshal(o)
			if _, err = fmt.Fprintf(w, "id: %d\nevent: project_operation\ndata: %s\n\n", o.Sequence, data); err != nil {
				return
			}
			if err = http.NewResponseController(w).Flush(); err != nil {
				return
			}
			sequence = o.Sequence
		}
		if !o.Active() {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-heartbeat.C:
			if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err = http.NewResponseController(w).Flush(); err != nil {
				return
			}
		}
		changed = a.projects.Changes()
		o, err = a.projects.Get(id)
		if err != nil {
			return
		}
	}
}
