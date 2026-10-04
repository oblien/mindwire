package api

import (
	"github.com/oblien/mindwire/daemon/internal/surface"
	"net/http"
)

func (a *API) surfaceRuntimeBind(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, nil)
	if s == nil {
		return
	}
	var binding surface.RuntimeBinding
	if !desktopBody(w, r, &binding) {
		return
	}
	if err := s.BindRuntime(binding, a.store); err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"configured": true})
}
func (a *API) surfaceCatalog(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, nil)
	if s == nil {
		return
	}
	value, err := s.ListDesktops(r.Context(), r.URL.Query().Get("projectId"))
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) surfaceCreateDesktop(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, nil)
	if s == nil {
		return
	}
	var request surface.CreateDesktopRequest
	if !desktopBody(w, r, &request) {
		return
	}
	value, err := s.CreateDesktop(r.Context(), request)
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 201, surface.DesktopResult{Success: true, Session: value})
}
func (a *API) surfaceProjectDesktop(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, nil)
	if s == nil {
		return
	}
	value, err := s.EnsureProjectDesktop(r.Context(), r.PathValue("id"))
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, surface.DesktopResult{Success: true, Session: value})
}
func (a *API) surfaceDesktopOperation(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, nil)
	if s == nil {
		return
	}
	operation := r.PathValue("operation")
	var update *surface.UpdateDesktopRequest
	switch r.Method {
	case "GET":
		operation = "get"
	case "PATCH":
		operation = "update"
		update = &surface.UpdateDesktopRequest{}
		if !desktopBody(w, r, update) {
			return
		}
	case "DELETE":
		operation = "delete"
	case "POST":
		if operation != "start" && operation != "stop" {
			surfaceError(w, &surface.Error{Code: "invalid_request", Message: "Choose start or stop."})
			return
		}
	}
	value, err := s.DesktopOperation(r.Context(), r.PathValue("desktopID"), operation, update)
	if err != nil {
		surfaceError(w, err)
		return
	}
	if operation == "delete" && value.Session.ID == "" {
		writeJSON(w, 200, map[string]bool{"success": value.Success})
		return
	}
	writeJSON(w, 200, value)
}
