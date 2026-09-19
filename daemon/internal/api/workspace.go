package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

func workspaceError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, registry.ErrInvalid), errors.Is(err, gitaccess.ErrCredential):
		status = http.StatusBadRequest
	case errors.Is(err, registry.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, registry.ErrDeleted):
		status = http.StatusGone
	case errors.Is(err, registry.ErrNotFound):
		status = http.StatusNotFound
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = "could not persist workspace metadata"
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func (a *API) requireRegistry(w http.ResponseWriter) bool {
	if a.registry != nil {
		return true
	}
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "workspace registry is unavailable; update the daemon"})
	return false
}

func (a *API) workspaceSnapshot(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	var since *int64
	if strings.HasSuffix(r.URL.Path, "/changes") {
		n, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		if err != nil || n < 0 {
			badRequest(w, "since must be a non-negative revision")
			return
		}
		since = &n
	}
	snapshot, err := a.registry.Snapshot(since)
	if err != nil {
		workspaceError(w, err)
		return
	}
	if expected := r.URL.Query().Get("workspaceId"); expected != "" && expected != snapshot.WorkspaceID {
		workspaceError(w, registry.ErrConflict)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (a *API) workspaceImport(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	var batch registry.Import
	if err := decode(w, r, &batch); err != nil {
		badRequest(w, "invalid workspace import")
		return
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	if err := a.registry.Import(batch); err != nil {
		workspaceError(w, err)
		return
	}
	if err := a.registry.RestoreSessions(batch.Chats, a.store); err != nil {
		workspaceError(w, err)
		return
	}
	a.writeWorkspaceSnapshot(w)
}

func (a *API) workspacePut(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	var req struct {
		Record           json.RawMessage `json:"record"`
		ExpectedRevision *int64          `json:"expectedRevision"`
	}
	if err := decode(w, r, &req); err != nil || len(req.Record) == 0 {
		badRequest(w, "record is required")
		return
	}
	kind, id := r.PathValue("kind"), r.PathValue("id")
	if kind == "agents" {
		var profile registry.Agent
		if err := json.Unmarshal(req.Record, &profile); err != nil {
			badRequest(w, "invalid agent profile")
			return
		}
		if _, ok := a.sup.Resolve(profile.AgentType); !ok {
			badRequest(w, "unknown harness type")
			return
		}
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	var err error
	if kind == "projects" {
		if !a.requireProjects(w) {
			return
		}
		err = a.projects.Save(id, req.Record, req.ExpectedRevision)
	} else {
		err = a.registry.Put(kind, id, req.Record, req.ExpectedRevision)
	}
	if err != nil {
		workspaceError(w, err)
		return
	}
	a.writeWorkspaceSnapshot(w)
}

func (a *API) workspaceDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	kind, id := r.PathValue("kind"), r.PathValue("id")
	var expected *int64
	if value := r.URL.Query().Get("revision"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 {
			badRequest(w, "revision must be non-negative")
			return
		}
		expected = &n
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	if kind == "projects" {
		if !a.requireProjects(w) {
			return
		}
		if err := a.projects.Remove(id, expected); err != nil {
			workspaceError(w, err)
			return
		}
		a.writeWorkspaceSnapshot(w)
		return
	}
	snapshot, err := a.registry.Snapshot(nil)
	if err != nil {
		workspaceError(w, err)
		return
	}
	for _, chat := range snapshot.Chats {
		if (kind == "chats" && chat.ID == id || kind == "agents" && chat.AgentID == id || kind == "projects" && chat.ProjectID == id) && a.sup.Busy(chat.ID) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "stop the running chat before removing this record"})
			return
		}
	}
	if err := a.registry.Delete(kind, id, expected, false); err != nil {
		workspaceError(w, err)
		return
	}
	a.writeWorkspaceSnapshot(w)
}

func (a *API) writeWorkspaceSnapshot(w http.ResponseWriter) {
	snapshot, err := a.registry.Snapshot(nil)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}
