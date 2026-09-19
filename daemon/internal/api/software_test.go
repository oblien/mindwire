package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

type softwareAdapter struct{ resolveFakeAdapter }

func (a softwareAdapter) Toolchain() toolchain.Spec {
	return toolchain.Spec{ID: a.ID(), Name: "Software fixture", Binary: "mindwire-software-fixture", VersionArgs: []string{"--version"}}
}

func TestSoftwareEndpointAndAgentInfoSharePolicyAndRejectIncompatibleTurn(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", root)
	ad := softwareAdapter{resolveFakeAdapter: resolveFakeAdapter{id: "software-http"}}
	agent.Register(ad)
	policy := toolchain.Bundled()
	policy.Revision++
	policy.Harnesses[ad.ID()] = toolchain.Harness{Releases: []toolchain.Release{
		{Version: "1.0.0", Daemon: []toolchain.DaemonRange{{Min: "0.1.15"}}},
		{Version: "1.1.0", Daemon: []toolchain.DaemonRange{{Min: "0.1.15"}}},
		{Version: "2.0.0", Daemon: []toolchain.DaemonRange{{Min: "0.2.0"}}},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(policy) }))
	defer server.Close()
	t.Setenv("MINDWIRE_HARNESS_CATALOG_URL", server.URL)
	selectVersion := func(version string) {
		bin := filepath.Join(root, ad.Toolchain().Binary, "versions", version, "bin", ad.Toolchain().Binary)
		if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("#!/bin/sh\necho "+version+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(map[string]string{"version": version})
		if err := os.WriteFile(filepath.Join(root, ad.Toolchain().Binary, "selected.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	selectVersion("1.0.0")
	store, err := session.Open(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Fanout(nil), root, ad.ID())
	api := New(store, hub, sup)
	defer api.Close()
	request := func(handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	w := request(api.agentSoftware, "GET", "/agent/software", "")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var software toolchain.Software
	if err := json.Unmarshal(w.Body.Bytes(), &software); err != nil {
		t.Fatal(err)
	}
	if software.InstalledVersion != "1.0.0" || software.RecommendedVersion != "1.1.0" || !software.Managed || !software.RequiresDaemonUpdate || !software.UpdateAvailable {
		t.Fatalf("%+v", software)
	}
	w = request(api.agentInfo, "GET", "/agent", "")
	var info struct {
		Software toolchain.Software `json:"software"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info.Software, software) {
		t.Fatalf("endpoint disagreement: %+v / %+v", software, info.Software)
	}
	selectVersion("2.0.0")
	w = request(api.turn, "POST", "/turns", `{"chatId":"incompatible-chat","message":"hello"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "incompatible") {
		t.Fatalf("incompatible turn admitted: %d %s", w.Code, w.Body)
	}
}
