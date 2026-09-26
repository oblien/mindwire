package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/oblien/mindwire/daemon/internal/conversations"
	"github.com/oblien/mindwire/daemon/internal/projectsync"
)

func (a *API) discoverForSync(ctx context.Context, projectID string) error {
	issues, err := a.conversations.Refresh(ctx, conversations.Query{ProjectID: projectID, Refresh: true})
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return fmt.Errorf("refresh native conversations before synchronizing: %s", issues[0].Message)
	}
	return nil
}
func (a *API) requireProjectSync(w http.ResponseWriter) bool {
	if a.projectSync == nil {
		writeJSON(w, 503, map[string]string{"error": "Project synchronization requires an updated Mindwire service with workspace metadata."})
		return false
	}
	return true
}
func (a *API) projectSyncExport(w http.ResponseWriter, r *http.Request) {
	a.projectSyncStart(w, r, "export")
}
func (a *API) projectSyncImport(w http.ResponseWriter, r *http.Request) {
	a.projectSyncStart(w, r, "import")
}
func (a *API) projectSyncStart(w http.ResponseWriter, r *http.Request, kind string) {
	if !a.requireProjectSync(w) {
		return
	}
	var req projectsync.Request
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid project sync request")
		return
	}
	o, err := a.projectSync.Start(kind, req)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, o)
}
func (a *API) projectSyncOperations(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	v, err := a.projectSync.List()
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"operations": v})
}
func (a *API) projectSyncOperation(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	v, err := a.projectSync.Get(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (a *API) projectSyncRetry(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	v, err := a.projectSync.Retry(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 202, v)
}
func (a *API) projectSyncCancel(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	v, err := a.projectSync.Cancel(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (a *API) projectSyncCheckpoint(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	v, err := a.projectSync.Checkpoint(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (a *API) projectSyncAccept(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	var req projectsync.Checkpoint
	if err := decode(w, r, &req); err != nil || req.ID != r.PathValue("id") {
		badRequest(w, "invalid checkpoint")
		return
	}
	if err := a.projectSync.AcceptCheckpoint(r.Context(), req); err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, req)
}
func (a *API) projectSyncObjects(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	cursor := 0
	var err error
	if value := r.URL.Query().Get("cursor"); value != "" {
		cursor, err = strconv.Atoi(value)
		if err != nil {
			badRequest(w, "invalid cursor")
			return
		}
	}
	v, err := a.projectSync.Objects(r.PathValue("id"), cursor)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (a *API) projectSyncMissing(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	var req struct {
		Objects []string `json:"objects"`
	}
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid object list")
		return
	}
	v, err := a.projectSync.Missing(req.Objects)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"objects": v})
}
func (a *API) projectSyncObject(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	b, err := a.projectSync.GetObject(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": b})
}
func (a *API) projectSyncObjectPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireProjectSync(w) {
		return
	}
	var req struct {
		Data []byte `json:"data"`
	}
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid object data")
		return
	}
	if err := a.projectSync.PutObject(r.PathValue("id"), req.Data); err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
