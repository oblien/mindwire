package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/orchestrator"
)

func TestServiceUpdateHTTPAuthenticationLeaseAndMutationAdmission(t *testing.T) {
	h, _, a := newRegistryAPIEnv(t)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		path := "/service/update"
		if method == "DELETE" {
			path += "/unknown"
		}
		if got := serve(t, h, method, path, ""); got.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", method, got.Code)
		}
	}
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	got := serve(t, authed, "POST", "/service/update", "")
	var lease orchestrator.ServiceUpdateLease
	if got.Code != 201 || json.Unmarshal(got.Body.Bytes(), &lease) != nil || lease.ID == "" {
		t.Fatalf("acquire: %d %s", got.Code, got.Body.String())
	}
	if got := serve(t, authed, "POST", "/service/update", ""); got.Code != 409 || !strings.Contains(got.Body.String(), "service_updating") {
		t.Fatalf("second installer: %d %s", got.Code, got.Body.String())
	}
	for _, route := range a.Routes() {
		if route.Method == "GET" || strings.HasPrefix(route.Pattern, "/service/update") {
			continue
		}
		path := strings.NewReplacer("{id}", "fixture", "{kind}", "agents", "{operation}", "pull", "{name}", "fixture").Replace(route.Pattern)
		if got := serve(t, authed, route.Method, path, "{}"); got.Code != 409 || !strings.Contains(got.Body.String(), "service_updating") {
			t.Errorf("mutation admitted: %s %s: %d %s", route.Method, path, got.Code, got.Body.String())
		}
	}
	if got := serve(t, authed, "GET", "/workspace", ""); got.Code != 200 {
		t.Fatal("history/metadata reads blocked")
	}
	if got := serve(t, authed, "DELETE", "/service/update/wrong", ""); got.Code != 404 {
		t.Fatal("wrong lease released update")
	}
	if got := serve(t, authed, "DELETE", "/service/update/"+lease.ID, ""); got.Code != 200 {
		t.Fatal("lease release failed")
	}
	if got := serve(t, authed, "POST", "/service/update", ""); got.Code != 201 {
		t.Fatalf("admission not reopened: %d %s", got.Code, got.Body.String())
	}
}

func TestServiceUpdateWaitsForGitOperationAndKeepsItsResult(t *testing.T) {
	h, a, dir := gitHTTPFixture(t)
	startBlockingGitCommit(t, a, dir)
	if got := serve(t, h, "GET", "/service/update", ""); !strings.Contains(got.Body.String(), `"idle":false`) {
		t.Fatalf("activity: %s", got.Body.String())
	}
	if got := serve(t, h, "POST", "/service/update", ""); got.Code != 409 || !strings.Contains(got.Body.String(), "service_busy") {
		t.Fatalf("active Git replaced: %d %s", got.Code, got.Body.String())
	}
	if a.sup.ServiceUpdateState().Updating {
		t.Fatal("refused update left admission closed")
	}
	if err := os.WriteFile(filepath.Join(dir, ".git/test-release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	operation, err := a.gitJobs.Wait(ctx, "blocked-commit")
	if err != nil || operation.Status != "succeeded" {
		t.Fatalf("update interrupted commit: %+v %v", operation, err)
	}
	if got := serve(t, h, "POST", "/service/update", ""); got.Code != 201 {
		t.Fatalf("idle update: %d %s", got.Code, got.Body.String())
	}
}
