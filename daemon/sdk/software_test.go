package mindwire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

type softwareFake struct{ fakeAdapter }

func (softwareFake) ID() string { return "software-sdk" }
func (a softwareFake) Toolchain() toolchain.Spec {
	return toolchain.Spec{ID: a.ID(), Name: "Software SDK fixture", Binary: "mindwire-sdk-fixture", VersionArgs: []string{"--version"}}
}

func TestSoftwareSDKUsesSharedCompatibilityPolicy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", root)
	ad := softwareFake{}
	agent.Register(ad)
	policy := toolchain.Bundled()
	policy.Revision++
	policy.Harnesses[ad.ID()] = toolchain.Harness{Releases: []toolchain.Release{{Version: "1.0.0", Daemon: []toolchain.DaemonRange{{Min: "0.1.15"}}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(policy) }))
	defer server.Close()
	t.Setenv("MINDWIRE_HARNESS_CATALOG_URL", server.URL)
	bin := filepath.Join(root, ad.Toolchain().Binary, "versions", "1.0.0", "bin", ad.Toolchain().Binary)
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 1.0.0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ad.Toolchain().Binary, "selected.json"), []byte(`{"version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{Agent: ad.ID(), StatePath: filepath.Join(root, "state.json"), CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := c.Software(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compatibility != "supported" || !got.Managed || got.UpdateAvailable {
		t.Fatalf("%+v", got)
	}
	info, err := c.Agent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Software == nil || !reflect.DeepEqual(*info.Software, got) {
		t.Fatalf("agent/software disagree: %+v %+v", info.Software, got)
	}
	if c.Health().HarnessPolicyVersion != 1 || c.Catalog().HarnessCompatibility.Revision != policy.Revision {
		t.Fatal("missing protocol capability or accepted catalog")
	}
}
