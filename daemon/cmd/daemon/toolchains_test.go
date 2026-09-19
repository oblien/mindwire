package main

import (
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

func TestEveryManagedAdapterHasBundledCompatibleImageVersion(t *testing.T) {
	plan, err := bundledToolchains()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) == 0 {
		t.Fatal("empty runtime image toolchain")
	}
	for _, item := range plan {
		if item.Package == "" || item.Binary == "" || len(item.VersionArgs) == 0 {
			t.Fatalf("incomplete package declaration: %+v", item)
		}
		if got := toolchain.Bundled().Evaluate(item.Agent, agent.Version, item.Version); got.Compatibility != "supported" {
			t.Fatalf("unqualified image: %+v", got)
		}
	}
}
