package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopSetupRequiresLocalCredentialInAdditionToWorkspaceAuth(t *testing.T) {
	const local = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	a := &API{localDesktopControlToken: local}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /surfaces/desktop/local", a.surfaceLocalConfigure)
	handler := Auth("paired-workspace-token", mux)
	for _, test := range []struct {
		name, bearer, owner string
		status              int
	}{
		{"missing workspace token", "", local, 401},
		{"phone credential only", "paired-workspace-token", "", 403},
		{"wrong local credential", "paired-workspace-token", "different", 403},
		{"truncated local credential", "paired-workspace-token", local[:32], 403},
		// Valid local auth reaches the capability check. This fixture deliberately
		// has no provider, so it must never touch real Screen Sharing on the host.
		{"local CLI credential", "paired-workspace-token", local, 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/surfaces/desktop/local", strings.NewReader(`{"enabled":true,"username":"owner","password":"private-mac-password"}`))
			if test.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+test.bearer)
			}
			req.Header.Set("X-Mindwire-Local-Control", test.owner)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != test.status {
				t.Fatalf("status %d, want %d", response.Code, test.status)
			}
			body := response.Body.String()
			if strings.Contains(body, local) || strings.Contains(body, "private-mac-password") || strings.Contains(body, "paired-workspace-token") {
				t.Fatal("desktop setup response leaked a credential")
			}
		})
	}
}
