package toolchain

import (
	"context"
	"strings"
)

type installProgressKey struct{}

// WithInstallProgress observes installer activity without exposing package-manager logs or
// credentials. The setup tracker owns this context, so all clients see the same live stage.
func WithInstallProgress(ctx context.Context, report func(stage string)) context.Context {
	return context.WithValue(ctx, installProgressKey{}, report)
}

func reportInstallProgress(ctx context.Context, stage string) {
	if report, _ := ctx.Value(installProgressKey{}).(func(string)); report != nil {
		report(stage)
	}
}

// npm emits activity as it fetches packages and runs installation scripts. Keep only a
// bounded diagnostic tail for failures; report known phases, never raw output, to the UI.
// os/exec serializes writes because stdout and stderr share this same writer.
type installOutput struct {
	ctx     context.Context
	tail    []byte
	pending string
}

func (w *installOutput) Write(p []byte) (int, error) {
	const tailLimit = 64 * 1024
	w.tail = append(w.tail, p...)
	if len(w.tail) > tailLimit {
		w.tail = append([]byte(nil), w.tail[len(w.tail)-tailLimit:]...)
	}
	lines := strings.Split(w.pending+string(p), "\n")
	w.pending = lines[len(lines)-1]
	if len(w.pending) > 4096 {
		w.pending = ""
	}
	for _, line := range lines[:len(lines)-1] {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "npm http fetch"), strings.HasPrefix(line, "npm http cache"):
			reportInstallProgress(w.ctx, "downloading")
		case strings.HasPrefix(line, "npm info run "), strings.HasPrefix(line, "added "), strings.HasPrefix(line, "changed "):
			reportInstallProgress(w.ctx, "configuring")
		}
	}
	return len(p), nil
}
