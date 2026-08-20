//go:build unix

package proc

import (
	"os/exec"
	"syscall"
)

// Group runs the command in its own process group (Setpgid) and, on ctx cancel,
// SIGKILLs the whole group by signalling the negative pid — so bash AND every
// descendant it spawned (node/claude/codex) die together, not just the shell.
// WaitDelay guarantees Wait() returns even if an orphan still holds an inherited
// pipe. Call immediately after exec.CommandContext, before Start.
func Group(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid → the whole process group (the child is the group leader,
		// since Setpgid made its pgid == its pid). ESRCH means the group already
		// exited between cancel and here — not an error worth surfacing.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
		return nil
	}
	cmd.WaitDelay = killGrace
}
