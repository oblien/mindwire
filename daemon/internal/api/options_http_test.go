package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func TestSettingsAndLogoutHTTPPreserveWorkspaceData(t *testing.T) {
	h, store, a := newRegistryAPIEnv(t)
	if got := serve(t, h, "POST", "/auth/logout?agent=codex", ""); got.Code != 401 {
		t.Fatalf("unauthenticated logout: %d", got.Code)
	}
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	if got := serve(t, authed, "POST", "/auth/step?agent=codex", `{"apiKey":"fixture-api-key"}`); got.Code != 200 {
		t.Fatalf("connect: %d %s", got.Code, got.Body.String())
	}
	config := `{"model":"gpt-6-astra","reasoningEffort":"max","reasoningSummary":"auto","request-max-retries":"4","stream-max-retries":"10","stream-idle-timeout-ms":"300000","apiKey":"must-not-overwrite"}`
	if got := serve(t, authed, "PUT", "/config?agent=codex", config); got.Code != 204 {
		t.Fatalf("save: %d %s", got.Code, got.Body.String())
	}
	ag, _ := a.sup.Resolve("codex")
	if ag.Creds.Get("apiKey") != "fixture-api-key" {
		t.Fatal("settings overwrote credentials")
	}
	before := serve(t, authed, "GET", "/config?agent=codex", "")
	var saved map[string]string
	if before.Code != 200 || json.Unmarshal(before.Body.Bytes(), &saved) != nil || saved["reasoning-effort"] != "max" || saved["reasoning-summary"] != "auto" {
		t.Fatalf("read-back: %d %s", before.Code, before.Body.String())
	}
	if strings.Contains(before.Body.String(), "apiKey") {
		t.Fatal("credential included in settings")
	}
	if got := serve(t, authed, "PUT", "/config?agent=codex", `{"model":"broken-model","request-max-retries":"-1"}`); got.Code != 400 {
		t.Fatalf("invalid settings: %d", got.Code)
	}
	if after := serve(t, authed, "GET", "/config?agent=codex", ""); after.Body.String() != before.Body.String() {
		t.Fatal("invalid patch partially changed settings")
	}
	if err := ag.Creds.Set("provider:shared:apiKey", "shared-key"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddMessage(session.Message{ID: "kept", ChatID: "chat", Role: "assistant", Text: "Keep this history", CreatedAt: "2026-09-19T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	got := serve(t, authed, "POST", "/auth/logout?agent=codex", "")
	var status agent.AuthStatus
	if got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &status) != nil || status.Configured || !status.SignedOut {
		t.Fatalf("logout: %d %s", got.Code, got.Body.String())
	}
	if ag.Creds.Get("apiKey") != "" || ag.Creds.Get("provider:shared:apiKey") != "shared-key" {
		t.Fatal("incorrect credential scope cleared")
	}
	if after := serve(t, authed, "GET", "/config?agent=codex", ""); after.Body.String() != before.Body.String() {
		t.Fatal("logout changed settings")
	}
	if got := serve(t, authed, "GET", "/chats/chat/messages?agent=codex", ""); got.Code != 200 || !strings.Contains(got.Body.String(), "Keep this history") {
		t.Fatalf("history lost: %d %s", got.Code, got.Body.String())
	}
	if !ag.Capabilities().AuthLogout {
		t.Fatal("logout capability missing")
	}
	if got := serve(t, authed, "POST", "/auth/step?agent=codex", `{"apiKey":"new-fixture-key"}`); got.Code != 200 {
		t.Fatalf("reconnect: %d", got.Code)
	}
	if got := serve(t, authed, "GET", "/auth/status?agent=codex", ""); !strings.Contains(got.Body.String(), `"configured":true`) {
		t.Fatal("completed connection did not clear sign-out")
	}
}
