package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/oblien/mindwire/daemon/internal/surface"
)

var desktopUser = surface.Actor{Kind: "user", Name: "You"}

func (a *API) EnableLocalDesktop(controlToken string) error {
	if a.surfaces == nil || controlToken == "" {
		return &surface.Error{Code: "unsupported", Message: "Local desktop control is unavailable."}
	}
	if err := a.surfaces.EnableLocalDesktop(a.store); err != nil {
		return err
	}
	a.localDesktopControlToken = controlToken
	_, err := a.surfaces.Refresh(context.Background())
	return err
}

func (a *API) AuthorizeDesktopViewer(sessionID string) error {
	if a.surfaces == nil {
		return &surface.Error{Code: "unsupported", Message: "Desktop control is unavailable."}
	}
	return a.surfaces.AuthorizeViewer(sessionID)
}
func (a *API) ServeDesktopViewer(ctx context.Context, sessionID string, stream io.ReadWriteCloser) error {
	if a.surfaces == nil {
		return &surface.Error{Code: "unsupported", Message: "Desktop control is unavailable."}
	}
	return a.surfaces.ServeViewer(ctx, sessionID, stream)
}

func (a *API) surfaceLocalInfo(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	info, err := s.LocalDesktopInfo(r.Context())
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, info)
}

func (a *API) surfaceLocalConfigure(w http.ResponseWriter, r *http.Request) {
	// Pairing credentials alone cannot opt a computer into desktop access. The
	// local CLI reads this separate 0600 token; it is never sent to a phone.
	if a.localDesktopControlToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Mindwire-Local-Control")), []byte(a.localDesktopControlToken)) != 1 {
		surfaceError(w, &surface.Error{Code: "forbidden", Message: "Run mindwire desktop on this Mac to manage desktop access."})
		return
	}
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	var settings surface.LocalDesktopSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&settings); err != nil {
		surfaceError(w, &surface.Error{Code: "invalid_request", Message: "Invalid desktop setup request."})
		return
	}
	value, err := s.ConfigureLocalDesktop(r.Context(), settings, a.store)
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, value)
}

func surfaceError(w http.ResponseWriter, err error) {
	detail, status := surface.ErrorResponse(err)
	writeJSON(w, status, map[string]any{"error": detail.Message, "code": detail.Code})
}

func (a *API) desktop(w http.ResponseWriter, r *http.Request) *surface.Service {
	if a.surfaces == nil {
		surfaceError(w, &surface.Error{Code: "unsupported", Message: "Desktop control requires an updated workspace service."})
	}
	if a.surfaces != nil && r != nil {
		value, err := a.surfaces.ForDesktop(r.URL.Query().Get("desktopId"))
		if err != nil {
			surfaceError(w, err)
			return nil
		}
		return value
	}
	return a.surfaces
}
func desktopBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(out); err != nil {
		surfaceError(w, &surface.Error{Code: "invalid_request", Message: "Invalid desktop request."})
		return false
	}
	return true
}
func (a *API) surfacesList(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	writeJSON(w, 200, []surface.Snapshot{s.Snapshot()})
}
func (a *API) surfaceStatus(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	if r.URL.Query().Get("refresh") == "true" {
		snapshot, _ := s.Refresh(r.Context())
		writeJSON(w, 200, snapshot)
		return
	}
	writeJSON(w, 200, s.Snapshot())
}
func (a *API) surfaceBind(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, nil)
	if s == nil {
		return
	}
	var binding surface.Binding
	if !desktopBody(w, r, &binding) {
		return
	}
	if err := s.Bind(binding, a.store); err != nil {
		surfaceError(w, err)
		return
	}
	selected, err := s.ForDesktop(binding.DesktopID)
	if err != nil {
		surfaceError(w, err)
		return
	}
	snapshot, _ := selected.Refresh(r.Context())
	writeJSON(w, 200, snapshot)
}
func (a *API) surfaceOpen(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	var request surface.OpenRequest
	if !desktopBody(w, r, &request) {
		return
	}
	value, err := s.Open(r.Context(), desktopUser, request)
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 201, value)
}
func (a *API) surfaceControl(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	var request surface.ControlRequest
	if !desktopBody(w, r, &request) {
		return
	}
	value, err := s.Control(r.Context(), r.PathValue("id"), desktopUser, request)
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) surfaceClose(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	if err := s.CloseSession(r.PathValue("id"), desktopUser); err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"closed": true})
}
func (a *API) surfaceCapture(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	value, err := s.Capture(r.Context(), r.PathValue("id"), desktopUser)
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 201, value)
}
func (a *API) surfaceAction(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	var request surface.ActionRequest
	if !desktopBody(w, r, &request) {
		return
	}
	value, err := s.Apply(r.Context(), desktopUser, request)
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) surfaceReceipt(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	value, err := s.Receipt(r.PathValue("id"))
	if err != nil {
		surfaceError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) artifact(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	value, err := s.Artifacts().Get(r.PathValue("id"))
	if err != nil {
		surfaceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	writeJSON(w, 200, value)
}
func (a *API) surfaceEvents(w http.ResponseWriter, r *http.Request) {
	s := a.desktop(w, r)
	if s == nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		changed := s.Changes()
		data, _ := json.Marshal(s.Snapshot())
		if _, err := w.Write(append(append([]byte("event: surface\ndata: "), data...), []byte("\n\n")...)); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-heartbeat.C:
		}
	}
}
