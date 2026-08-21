package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthAcceptsBearerOrProxyToken(t *testing.T) {
	h := Auth("runtime-secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for name, header := range map[string]struct {
		name  string
		value string
	}{
		"bearer":       {name: "Authorization", value: "Bearer runtime-secret"},
		"oblien proxy": {name: "X-Mindwire-Token", value: "runtime-secret"},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/catalog", nil)
			r.Header.Set(header.name, header.value)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
			}
		})
	}
}

func TestAuthRejectsWrongProxyToken(t *testing.T) {
	h := Auth("runtime-secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	r := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	r.Header.Set("X-Mindwire-Token", "wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
