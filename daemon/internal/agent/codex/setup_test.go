package codex

import (
	"testing"

	"github.com/oblien/mindwire/daemon/internal/setup"
)

func TestCodexSetupIncludesBubblewrapOnlyOnLinux(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			plan, err := setup.Plan(installSteps(goos))
			if err != nil {
				t.Fatal(err)
			}
			bwrap, cli := -1, -1
			for i, step := range plan {
				if step.Name == "bubblewrap" {
					bwrap = i
				}
				if step.Name == "Codex CLI" {
					cli = i
				}
			}
			if goos == "linux" && (bwrap < 0 || cli <= bwrap) {
				t.Fatalf("Linux must verify Bubblewrap before Codex: %+v", plan)
			}
			if goos == "darwin" && bwrap >= 0 {
				t.Fatal("macOS must keep using native Seatbelt")
			}
		})
	}
}
