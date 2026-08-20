//go:build unix

package proc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestGroupConfigures locks the three fields Group must set.
func TestGroupConfigures(t *testing.T) {
	cmd := exec.Command("true")
	Group(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid not set")
	}
	if cmd.Cancel == nil {
		t.Error("Cancel not set")
	}
	if cmd.WaitDelay == 0 {
		t.Error("WaitDelay not set")
	}
}

// TestGroupKillsDescendants is the whole point: cancelling the context must kill the
// grandchild bash backgrounded, not just bash. Without the process-group signal that
// grandchild is reparented to init and keeps running (burning budget, mutating files);
// with Group it dies in the same SIGKILL, so the marker file stops growing after cancel.
func TestGroupKillsDescendants(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ticks")

	ctx, cancel := context.WithCancel(context.Background())
	// A backgrounded subshell that appends forever, then bash blocks on a long sleep so the
	// process stays alive until we cancel. All three (bash, sleep, subshell) share the group.
	script := `( while true; do echo tick >> "` + marker + `"; sleep 0.05; done ) & sleep 100`
	cmd := exec.CommandContext(ctx, "bash", "-lc", script)
	Group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Let the grandchild write a few ticks so we know it's actually running.
	time.Sleep(300 * time.Millisecond)
	if fileSize(t, marker) == 0 {
		t.Fatal("grandchild never wrote — test setup broken")
	}

	cancel()
	waited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("cmd.Wait() did not return promptly after cancel")
	}

	// After the SIGKILL has certainly landed, the file must be frozen. Sample twice: a
	// surviving orphan would still be appending every 50ms between the two reads.
	time.Sleep(400 * time.Millisecond)
	a := fileSize(t, marker)
	time.Sleep(400 * time.Millisecond)
	b := fileSize(t, marker)
	if a != b {
		t.Fatalf("descendant survived cancel: marker still growing (%d → %d bytes)", a, b)
	}
}

func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("stat: %v", err)
	}
	return fi.Size()
}
