package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func shellPath(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'" }

func waitForStage(t *testing.T, tracker *Tracker, id, stage string) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status := tracker.Status(id); status.Stage == stage {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("setup did not reach %s: %+v", stage, tracker.Status(id))
	return Status{}
}

func TestRunStopsAfterFailedDependency(t *testing.T) {
	child := filepath.Join(t.TempDir(), "child-ran")
	results := Run(context.Background(), []agent.Step{
		{Name: "node", Check: "false", Install: "echo prerequisite-failed; exit 1"},
		{Name: "codex", Requires: []string{"node"}, Install: "touch " + shellPath(child)},
	}, false, nil, nil)
	if OK(results) || len(results) != 1 || results[0].Name != "node" || !strings.Contains(results[0].Output, "prerequisite-failed") {
		t.Fatalf("dependency failure: %+v", results)
	}
	if _, err := os.Stat(child); !os.IsNotExist(err) {
		t.Fatalf("dependent installer must not run: %v", err)
	}
}

func TestSuccessfulInstallerMustPassItsCheck(t *testing.T) {
	results := Run(context.Background(), []agent.Step{{Name: "codex", Check: "echo cli-missing; exit 1", Install: "true"}}, false, nil, nil)
	if OK(results) || len(results) != 1 || !strings.Contains(results[0].Output, "verification failed") || !strings.Contains(results[0].Output, "cli-missing") {
		t.Fatalf("exit 0 alone cannot report an installed CLI: %+v", results)
	}
}

func TestTrackerKeepsJobRunningThroughVerification(t *testing.T) {
	dir := t.TempDir()
	installed, release := filepath.Join(dir, "installed"), filepath.Join(dir, "release")
	steps := []agent.Step{{Name: "codex",
		Check:   "test -f " + shellPath(installed) + " || exit 1; while [ ! -f " + shellPath(release) + " ]; do sleep 0.01; done",
		Install: "touch " + shellPath(installed)}}
	tracker := NewTracker()
	tracker.Start("codex", steps, false, 5*time.Second)
	status := waitForStage(t, tracker, "codex", "verifying")
	if !status.Running || status.OK || status.Current != "codex" || len(status.Steps) != 0 {
		t.Fatalf("verification is part of the active install: %+v", status)
	}
	if joined := tracker.Start("codex", steps, true, 5*time.Second); !joined.Running || joined.Operation != "setup" || joined.Stage != "verifying" {
		t.Fatalf("update must join the install until verification ends: %+v", joined)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if status := waitForSetup(t, tracker, "codex"); !status.OK || status.Stage != "" || status.Steps[0].Status != "installed" {
		t.Fatalf("verified completion: %+v", status)
	}
}

func TestHarnessesSharePackageMutationLockAndRecheckPrerequisites(t *testing.T) {
	dir := t.TempDir()
	installed, release, count := filepath.Join(dir, "node"), filepath.Join(dir, "release"), filepath.Join(dir, "count")
	dependency := agent.Step{Name: "node", Check: "test -f " + shellPath(installed),
		Install: "printf x >> " + shellPath(count) + "; while [ ! -f " + shellPath(release) + " ]; do sleep 0.01; done; touch " + shellPath(installed)}
	tracker := NewTracker()
	tracker.Start("codex", []agent.Step{dependency}, false, 5*time.Second)
	waitForStage(t, tracker, "codex", "installing")
	tracker.Start("claude-code", []agent.Step{dependency}, false, 5*time.Second)
	if status := waitForStage(t, tracker, "claude-code", "waiting"); !status.Running || status.Current != "node" {
		t.Fatalf("second harness should report its actual wait: %+v", status)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if status := waitForSetup(t, tracker, "codex"); !status.OK {
		t.Fatalf("first harness: %+v", status)
	}
	if status := waitForSetup(t, tracker, "claude-code"); !status.OK || status.Steps[0].Status != "satisfied" {
		t.Fatalf("second harness should reuse the installed dependency: %+v", status)
	}
	if data, err := os.ReadFile(count); err != nil || string(data) != "x" {
		t.Fatalf("shared prerequisite ran twice: %q %v", data, err)
	}
}

func TestTimedOutInstallStopsChildWritesBeforeReleasingLock(t *testing.T) {
	leak := filepath.Join(t.TempDir(), "late-write")
	tracker := NewTracker()
	tracker.Start("codex", []agent.Step{{Name: "cli", Install: "sleep 1; touch " + shellPath(leak)}}, true, 250*time.Millisecond)
	if status := waitForSetup(t, tracker, "codex"); status.OK || !strings.Contains(status.Steps[0].Output, "deadline exceeded") {
		t.Fatalf("timeout: %+v", status)
	}
	tracker.Start("claude-code", []agent.Step{{Name: "cli", Install: "true"}}, true, time.Second)
	if status := waitForSetup(t, tracker, "claude-code"); !status.OK {
		t.Fatalf("timeout must release package-manager lock: %+v", status)
	}
	time.Sleep(time.Second)
	if _, err := os.Stat(leak); !os.IsNotExist(err) {
		t.Fatalf("an installer child continued writing after timeout: %v", err)
	}
}
