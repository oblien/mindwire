//go:build windows

package proc

import "os/exec"

// Group is a reduced best-effort on Windows: the daemon drives agents through
// `bash -lc`, which isn't part of a standard Windows environment, so the Unix
// process-group semantics don't apply. We still set WaitDelay so an inherited pipe
// can't wedge Wait(); CommandContext's default Cancel (kill the child) still runs.
// A first-class Windows target would need a Job Object to kill the whole tree.
func Group(cmd *exec.Cmd) {
	cmd.WaitDelay = killGrace
}
