package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/orchestrator"
)

func (a *API) externalServiceActivity() int {
	n := 0
	if a.connectionActivity != nil {
		n += a.connectionActivity()
	}
	if a.execution != nil && a.execution.Terminals.Busy() {
		n++
	}
	if a.projects != nil {
		n += a.projects.ActiveCount()
	}
	if a.projectSync != nil {
		n += a.projectSync.ActiveCount()
	}
	if a.gitJobs != nil {
		n += a.gitJobs.ActiveCount()
	}
	if a.surfaces != nil {
		n += a.surfaces.ActiveSessionCount()
	}
	return n
}

// Computer connections are optional and share the same service-update admission.
func (a *API) SetConnectionActivity(activity func() int) { a.connectionActivity = activity }

// The daemon owns the durable manual-update policy, so an older computer CLI can
// keep using its existing lease/download/restart loop without losing owner intent.
func (a *API) SetConnectionUpdateForce(force func() bool) { a.connectionUpdateForce = force }
func (a *API) ConnectionOperation(next http.HandlerFunc) http.HandlerFunc {
	return a.serviceOperation(next)
}

func (a *API) serviceUpdateStatus(w http.ResponseWriter, r *http.Request) {
	st := a.sup.ServiceUpdateState()
	st.ActiveOperations += a.externalServiceActivity()
	st.Idle = !st.Updating && st.ActiveOperations == 0
	writeJSON(w, http.StatusOK, st)
}

func (a *API) serviceUpdateAcquire(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Force bool `json:"force"`
	}
	if err := decode(w, r, &req); err != nil && !errors.Is(err, io.EOF) {
		badRequest(w, "invalid service update request")
		return
	}
	force := req.Force || (a.connectionUpdateForce != nil && a.connectionUpdateForce())
	lease, err := a.sup.AcquireServiceUpdate(force)
	if err != nil {
		serviceUpdateError(w, err)
		return
	}
	// Admission is now closed. Automatic updates require earlier mutations and
	// their background jobs to finish; manual updates may interrupt them. Inspect
	// services outside the supervisor's mutex to preserve lock order.
	if !force && a.externalServiceActivity() > 0 {
		a.sup.ReleaseServiceUpdate(lease.ID)
		serviceUpdateError(w, orchestrator.ErrServiceBusy)
		return
	}
	writeJSON(w, http.StatusCreated, lease)
}

func (a *API) serviceUpdateRelease(w http.ResponseWriter, r *http.Request) {
	if !a.sup.ReleaseServiceUpdate(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func serviceUpdateError(w http.ResponseWriter, err error) {
	code := "service_busy"
	if errors.Is(err, orchestrator.ErrServiceUpdating) {
		code = "service_updating"
	}
	w.Header().Set("Retry-After", "5")
	writeJSON(w, http.StatusConflict, map[string]string{"code": code, "error": err.Error()})
}

func (a *API) serviceOperation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		done, err := a.sup.BeginServiceOperation()
		if err != nil {
			serviceUpdateError(w, err)
			return
		}
		defer done()
		next(w, r)
	}
}
