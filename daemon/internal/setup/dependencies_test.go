package setup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestBubblewrapInstallerUsesHostPackagesAndSkipsInstalledOrMacOS(t *testing.T) {
	for _, test := range []struct {
		os, manager string
		installed   bool
	}{
		{"Linux", "apt-get", false}, {"Linux", "dnf", false}, {"Linux", "yum", false},
		{"Linux", "apk", false}, {"Linux", "pacman", false}, {"Linux", "zypper", false},
		{"Linux", "apt-get", true}, {"Darwin", "brew", false},
	} {
		t.Run(test.os+"/"+test.manager+"/"+map[bool]string{true: "present", false: "missing"}[test.installed], func(t *testing.T) {
			// Stub every OS/package-manager entry point. This test never installs host packages.
			prelude := `uname() { echo "$MW_TEST_OS"; }
id() { echo 1000; }
bwrap() { [ "$MW_TEST_INSTALLED" = 1 ] && echo 'bubblewrap 0.11.0'; }
command() { [ "$1" = -v ] && { [ "$2" = "$MW_TEST_MANAGER" ] || [ "$2" = sudo ]; }; }
sudo() { [ "$1" = -n ] || exit 97; shift; "$@"; }
apt-get() { printf 'PACKAGE apt-get %s\n' "$*"; }
dnf() { printf 'PACKAGE dnf %s\n' "$*"; }
yum() { printf 'PACKAGE yum %s\n' "$*"; }
apk() { printf 'PACKAGE apk %s\n' "$*"; }
pacman() { printf 'PACKAGE pacman %s\n' "$*"; }
zypper() { printf 'PACKAGE zypper %s\n' "$*"; }
brew() { echo 'UNEXPECTED brew'; exit 98; }
`
			cmd := exec.Command("bash", "-c", prelude+bubblewrapInstall)
			installed := "0"
			if test.installed {
				installed = "1"
			}
			cmd.Env = append(os.Environ(), "MW_TEST_OS="+test.os, "MW_TEST_MANAGER="+test.manager, "MW_TEST_INSTALLED="+installed)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v", out, err)
			}
			if test.installed || test.os != "Linux" {
				if len(out) != 0 {
					t.Fatalf("unnecessary install: %s", out)
				}
			} else if !strings.Contains(string(out), "PACKAGE "+test.manager) || !strings.Contains(string(out), "bubblewrap") {
				t.Fatalf("did not install the distribution package: %s", out)
			}
		})
	}
}

func TestDependencyChecksCacheAndSetupInvalidatesMissingResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed")
	step := agent.Step{Name: "bubblewrap", Check: "test -f " + agent.ShellQuote(path), Install: "touch " + agent.ShellQuote(path)}
	tracker := NewTracker()
	ctx := context.Background()
	if missing := tracker.MissingDependencies(ctx, []agent.Step{step}, false); !contains(missing, "bubblewrap") {
		t.Fatalf("did not report missing prerequisite: %v", missing)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if missing := tracker.MissingDependencies(ctx, []agent.Step{step}, false); !contains(missing, "bubblewrap") {
		t.Fatal("did not reuse cache")
	}
	if missing := tracker.MissingDependencies(ctx, []agent.Step{step}, true); contains(missing, "bubblewrap") {
		t.Fatalf("manual refresh ignored repair: %v", missing)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_ = tracker.MissingDependencies(ctx, []agent.Step{step}, true)
	tracker.Start("fixture", []agent.Step{step}, false, time.Second*5)
	deadline := time.Now().Add(5 * time.Second)
	for tracker.Status("fixture").Running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !tracker.Status("fixture").OK {
		t.Fatal("repair did not finish")
	}
	if missing := tracker.MissingDependencies(ctx, []agent.Step{step}, false); contains(missing, "bubblewrap") {
		t.Fatalf("completed repair kept stale readiness: %v", missing)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
