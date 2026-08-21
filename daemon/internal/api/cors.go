package api

import "net/http"

// CORS optionally wraps the whole handler with permissive cross-origin headers for
// local development (a web console on another origin). OFF unless enabled — production
// sandboxes keep the daemon same-origin only. Must wrap OUTSIDE Auth so OPTIONS
// preflight (which carries no token) is answered before the auth check.
func CORS(enabled bool, next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-Mindwire-Token, Content-Type")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
