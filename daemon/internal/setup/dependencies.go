package setup

import (
	"context"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

type dependencyProbe struct {
	ok bool
	at time.Time
}

// MissingDependencies is a read-only probe of shared tools in the declared setup plan. An
// installed CLI can still need setup after an upgrade adds a dependency. Personas share these
// short-lived results; manual refresh and a completed setup invalidate them.
func (t *Tracker) MissingDependencies(ctx context.Context, steps []agent.Step, force bool) []string {
	plan, err := Plan(steps)
	if err != nil {
		return []string{err.Error()}
	}
	shared := catalog()
	t.dependencyMu.Lock()
	defer t.dependencyMu.Unlock()
	if t.dependencies == nil {
		t.dependencies = map[string]dependencyProbe{}
	}
	var missing []string
	for _, step := range plan {
		if _, ok := shared[step.Name]; !ok {
			continue
		}
		cached, ok := t.dependencies[step.Name]
		if force || !ok || time.Since(cached.at) >= 30*time.Second {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err := command(probeCtx, step.Check)
			cancel()
			cached = dependencyProbe{ok: err == nil, at: time.Now()}
			// A canceled request is not evidence that an installed dependency disappeared.
			if ctx.Err() == nil {
				t.dependencies[step.Name] = cached
			}
		}
		if !cached.ok {
			missing = append(missing, step.Name)
		}
	}
	return missing
}
