package api

import (
	"net/http"

	"github.com/oblien/mindwire/packages/daemon/internal/registry"
)

func (a *API) chatSchedules(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	jobs, err := a.registry.ChatSchedules(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (a *API) chatScheduleSave(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	var input registry.ScheduleSpec
	if err := decode(w, r, &input); err != nil {
		badRequest(w, "invalid conversation schedule")
		return
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	job, err := a.apps.SaveChatSchedule(r.PathValue("id"), input)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (a *API) chatScheduleDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	if err := a.apps.DeleteChatSchedule(r.PathValue("id"), r.PathValue("jobId")); err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}
