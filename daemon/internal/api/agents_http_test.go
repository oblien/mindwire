package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/inventory"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

type installedHTTPAdapter struct {
	resolveFakeAdapter
	binary string
}

func (a installedHTTPAdapter) Toolchain() toolchain.Spec {
	return toolchain.Spec{ID: a.ID(), Name: a.Meta().Name, Binary: a.binary}
}

func TestHTTPDiscoversInstalledHarnessWithoutProjectsAndReusesSetup(t *testing.T) {
	fixture := installedHTTPAdapter{resolveFakeAdapter: resolveFakeAdapter{id: "http-installed-fixture"}, binary: "mindwire-http-inventory-fixture"}
	agent.Register(fixture)
	dir := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", filepath.Join(dir, "managed"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	binary := filepath.Join(dir, fixture.binary)
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	h, _, api := newRegistryAPIEnv(t)
	api.agents = inventory.NewAgents(api.registry, []agent.Adapter{fixture}, &api.registryMu)
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer workspace-secret")
		h.ServeHTTP(w, r)
	})
	read := func(path string) registry.Snapshot {
		t.Helper()
		response := serve(t, authed, "GET", path, "")
		var snapshot registry.Snapshot
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &snapshot) != nil {
			t.Fatalf("snapshot: %d %s", response.Code, response.Body.String())
		}
		return snapshot
	}
	first := read("/workspace")
	if len(first.Agents) != 1 || first.Agents[0].AgentType != fixture.ID() || first.Agents[0].Installation != "installed" || len(first.Projects) != 0 {
		t.Fatal("an installed harness without any project/chats was not discovered", first)
	}
	id := first.Agents[0].ID
	for range 3 {
		response := serve(t, authed, "POST", "/workspace/agents/ensure", `{"agentType":"http-installed-fixture","name":"Do not duplicate"}`)
		var result registry.EnsureAgentResult
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.AgentID != id || len(result.Snapshot.Agents) != 1 {
			t.Fatalf("repeated setup: %d %s", response.Code, response.Body.String())
		}
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	// A normal navigation uses its fresh cache; pull-to-refresh observes uninstall.
	if current := read("/workspace"); current.Revision != first.Revision {
		t.Fatal("passive navigation bypassed the cache")
	}
	absent := read("/workspace?refresh=true")
	if len(absent.Agents) != 1 || absent.Agents[0].ID != id || absent.Agents[0].Installation != "missing" || len(absent.Deleted) != 0 {
		t.Fatal(absent)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	reinstalled := read("/workspace?refresh=true")
	if len(reinstalled.Agents) != 1 || reinstalled.Agents[0].ID != id || reinstalled.Agents[0].Installation != "installed" {
		t.Fatal(reinstalled)
	}
	if response := serve(t, h, "POST", "/workspace/agents/ensure", `{"agentType":"http-installed-fixture"}`); response.Code != http.StatusUnauthorized {
		t.Fatal("setup endpoint skipped authentication")
	}
}
