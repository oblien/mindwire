package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/oblien/mindwire/daemon/internal/proc"
)

// InspectInstallation checks the executable selected for native execution without
// launching the harness, authenticating, or contacting a package/catalog server.
// A damaged managed selection remains visible for repair instead of falling back
// to a different installation. An interrupted shell lookup is not proof of absence.
func InspectInstallation(ctx context.Context, spec Spec) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if spec.Binary == "" || filepath.Base(spec.Binary) != spec.Binary {
		return "", fmt.Errorf("invalid harness executable")
	}
	if version := selected(Root(), spec.Binary); version != "" {
		info, err := os.Stat(versionBinary(Root(), spec.Binary, version))
		if errors.Is(err, os.ErrNotExist) {
			return "repair", nil
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
			return "repair", nil
		}
		return "installed", nil
	}
	if _, err := exec.LookPath(spec.Binary); err == nil {
		return "installed", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// type -P ignores shell aliases/functions and checks the same inherited + login
	// PATH as native turns. Do not execute --version: some CLIs initialize or hang.
	script := "mindwire_detected_binary=$(type -P -- " + quote(spec.Binary) + ") && " +
		"test -f \"$mindwire_detected_binary\" && test -x \"$mindwire_detected_binary\" || exit 42"
	cmd := exec.CommandContext(ctx, "bash", "-lc", Shell(script))
	cmd.Env = Environment()
	proc.Group(cmd)
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err == nil {
		return "installed", nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == 42 {
		return "missing", nil
	}
	return "", err
}
