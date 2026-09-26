package projectsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/workspacepath"
	"github.com/shirou/gopsutil/v4/process"
)

// Native CLI work started outside Mindwire does not hold the daemon's admission
// mutex. Detect visible harness processes as well as verifying two stable scans.
// This never signals or terminates the user's programs.
func nativeIdle(ctx context.Context, cwd string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	processes, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not check native coding processes before synchronization: %w", err)
	}
	for _, p := range processes {
		if p.Pid == int32(os.Getpid()) {
			continue
		}
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		} // exited or belongs to another account
		name = strings.ToLower(strings.TrimSuffix(name, ".exe"))
		isHarness := name == "codex" || name == "claude"
		if !isHarness && (name == "node" || name == "bun") {
			args, _ := p.CmdlineSliceWithContext(ctx)
			if len(args) > 1 {
				executable := filepath.ToSlash(args[1])
				isHarness = strings.Contains(executable, "@openai/codex/") || strings.Contains(executable, "@anthropic-ai/claude-code/")
			}
		}
		if !isHarness {
			continue
		}
		path, err := p.CwdWithContext(ctx)
		if err != nil || path == "" {
			continue
		}
		path, err = workspacepath.Canonical(path)
		if err == nil && workspacepath.Contains(cwd, path) {
			return fmt.Errorf("close the native coding session in this project before synchronizing; its work has been preserved")
		}
	}
	if err = ctx.Err(); err != nil {
		return fmt.Errorf("native process check timed out; retry synchronization")
	}
	return nil
}
