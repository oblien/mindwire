package surface

import (
	"encoding/json"
	"net/http"
)

func desktopFixtureRespond(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		detail, status := ErrorResponse(err)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": detail.Message, "code": detail.Code})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

// Shared HTTP boundary for native client fixtures. These routes exercise the
// same surface service; test-only providers cannot reach the host's desktop.
func registerDesktopFixtureRoutes(mux *http.ServeMux, s *Service) {
	respond := desktopFixtureRespond
	user := Actor{Kind: "user", Name: "iPhone"}
	mux.HandleFunc("GET /surfaces/desktop", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("refresh") == "true" {
			value, err := s.Refresh(r.Context())
			respond(w, value, err)
		} else {
			respond(w, s.Snapshot(), nil)
		}
	})
	mux.HandleFunc("POST /surfaces/desktop/sessions", func(w http.ResponseWriter, r *http.Request) {
		var req OpenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respond(w, nil, err)
			return
		}
		value, err := s.Open(r.Context(), user, req)
		respond(w, value, err)
	})
	mux.HandleFunc("POST /surfaces/desktop/sessions/{id}/control", func(w http.ResponseWriter, r *http.Request) {
		var req ControlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respond(w, nil, err)
			return
		}
		value, err := s.Control(r.Context(), r.PathValue("id"), user, req)
		respond(w, value, err)
	})
	mux.HandleFunc("DELETE /surfaces/desktop/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"closed": true}, s.CloseSession(r.PathValue("id"), user))
	})
	mux.HandleFunc("POST /surfaces/desktop/actions", func(w http.ResponseWriter, r *http.Request) {
		var req ActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respond(w, nil, err)
			return
		}
		value, err := s.Apply(r.Context(), user, req)
		respond(w, value, err)
	})
}
