//go:build darwin

package projects

import (
	"golang.org/x/sys/unix"
	"os/exec"
)

func renameExclusive(from, to string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_EXCL)
}
func bindParent(cmd *exec.Cmd) {}
