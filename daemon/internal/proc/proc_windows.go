//go:build windows

package proc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Cancel the process tree on Windows as well as the immediate shell. taskkill is
// a Windows system utility; arguments are passed directly without a command shell.
func Group(cmd *exec.Cmd) {
	cmd.WaitDelay = killGrace
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		command := "taskkill.exe"
		if root := os.Getenv("SystemRoot"); root != "" {
			command = filepath.Join(root, "System32", command)
		}
		if err := exec.CommandContext(ctx, command, "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run(); err != nil {
			_ = cmd.Process.Kill()
		}
		return nil
	}
}
