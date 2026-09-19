package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (a *API) projectGitStart(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	var req gitOperationRequest
	if decode(w, r, &req) != nil {
		badRequest(w, "invalid Git operation")
		return
	}
	o, err := a.startGitOperation(r.Context(), r.PathValue("id"), req)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, o)
}

func (a *API) projectGitOperations(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	operations, err := a.gitJobs.List(r.PathValue("id"), r.URL.Query().Get("active") == "true")
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operations": operations})
}

func (a *API) gitOperation(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	o, err := a.gitJobs.Get(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (a *API) gitOperationCancel(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	o, err := a.gitJobs.Cancel(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (a *API) gitOperationStream(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	id := r.PathValue("id")
	changed := a.gitJobs.Changes()
	o, err := a.gitJobs.Get(id)
	if err != nil {
		workspaceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	var sequence int64 = -1
	for {
		if o.Sequence != sequence {
			data, _ := json.Marshal(o)
			if _, err = fmt.Fprintf(w, "id: %d\nevent: git_operation\ndata: %s\n\n", o.Sequence, data); err != nil {
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
		changed = a.gitJobs.Changes()
		o, err = a.gitJobs.Get(id)
		if err != nil {
			return
		}
	}
}
