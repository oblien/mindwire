//go:build windows

package projects

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

func renameExclusive(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	// No MOVEFILE_REPLACE_EXISTING: another writer's destination is preserved.
	return windows.MoveFileEx(source, destination, 0)
}
func bindParent(cmd *exec.Cmd) {}
