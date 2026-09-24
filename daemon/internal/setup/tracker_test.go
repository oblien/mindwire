package setup

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func waitForSetup(t *testing.T, tracker *Tracker, id string) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status := tracker.Status(id); !status.Running {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("setup did not finish")
	return Status{}
}

func TestTrackerConcurrentSetupAndUpdateJoinSameHarnessJob(t *testing.T) {
	dir := t.TempDir()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	count, release := filepath.Join(dir, "count"), filepath.Join(dir, "release")
	steps := []agent.Step{{Name: "cli", Install: "printf x >> " + quote(count) +
		"; while [ ! -f " + quote(release) + " ]; do sleep 0.01; done"}}
	tracker := NewTracker()
	first := tracker.Start("codex", steps, true, 5*time.Second)
	if !first.Running || first.Operation != "update" {
		t.Fatalf("initial update: %+v", first)
	}
	if first.StartedAt == nil || first.StartedAt.IsZero() || len(first.Plan) != 1 || first.Plan[0] != "cli" {
		t.Fatalf("initial progress metadata: %+v", first)
	}

	var callers sync.WaitGroup
	for range 40 {
		callers.Go(func() {
			// A setup request from another client must join the existing update, retaining its mode.
			status := tracker.Start("codex", steps, false, 5*time.Second)
			if !status.Running || status.Operation != "update" {
				t.Errorf("reattached status: %+v", status)
			}
			if !status.StartedAt.Equal(*first.StartedAt) || len(status.Plan) != 1 || status.Plan[0] != "cli" {
				t.Errorf("reattaching lost the plan or restarted elapsed time: %+v", status)
			}
		})
	}
	callers.Wait()
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	status := waitForSetup(t, tracker, "codex")
	if !status.OK || status.Operation != "update" || len(status.Steps) != 1 {
		t.Fatalf("completed update: %+v", status)
	}
	data, err := os.ReadFile(count)
	if err != nil || string(data) != "x" {
		t.Fatalf("installer must run once, count=%q err=%v", data, err)
	}
	status.Steps[0].Name = "changed by caller"
	status.Plan[0] = "changed by caller"
	*status.StartedAt = time.Time{}
	if tracker.Status("codex").Steps[0].Name != "cli" {
		t.Fatal("mutating a snapshot changed the shared job")
	}
	if snapshot := tracker.Status("codex"); snapshot.Plan[0] != "cli" || snapshot.StartedAt.IsZero() {
		t.Fatal("mutating progress metadata changed the shared job")
	}
}

func TestTrackerFailedJobReleasesLockForRetry(t *testing.T) {
	tracker := NewTracker()
	tracker.Start("claude-code", []agent.Step{{Name: "cli", Install: "exit 1"}}, true, time.Second)
	if status := waitForSetup(t, tracker, "claude-code"); status.OK || status.Operation != "update" {
		t.Fatalf("failed update: %+v", status)
	}
	status := tracker.Start("claude-code", []agent.Step{{Name: "cli", Check: "true"}}, false, time.Second)
	if !status.Running || status.Operation != "setup" {
		t.Fatalf("retry: %+v", status)
	}
	if status := waitForSetup(t, tracker, "claude-code"); !status.OK {
		t.Fatalf("retry did not succeed: %+v", status)
	}
}
