//go:build !darwin && !linux && !windows

package projects

import (
	"fmt"
	"os/exec"
)

func renameExclusive(from, to string) error {
	return fmt.Errorf("atomic project creation is unsupported on this platform")
}
func bindParent(cmd *exec.Cmd) {}
