//go:build linux

package projects

import (
	"golang.org/x/sys/unix"
	"os/exec"
	"syscall"
)

func renameExclusive(from, to string) error {
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
}
func bindParent(cmd *exec.Cmd) { cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL }
