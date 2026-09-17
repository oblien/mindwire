// Package setup runs an agent's install toolchain inside the sandbox. The daemon
// owns this — the app just asks it to ensure/update the agent; new agents or CLI
// versions ship with the daemon binary, not an app release.
//
// Toolchain steps form a small dependency graph. A shared catalog holds reusable,
// cross-stack tools (git, node, …) that any agent references by name via Step.Requires;
// `Plan` expands an agent's steps against that catalog into a topologically-ordered,
// deduplicated install plan (dependencies first), so "who installs before who" is
// declarative and tools are never installed twice.
package setup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
)

// StepResult reports the outcome of one toolchain step.
type StepResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "satisfied" | "installed" | "failed"
	Output string `json:"output,omitempty"`
}

// baseRequirements are catalog tools the daemon itself always wants, independent of which
// agent runs — currently git, which powers source control.
var baseRequirements = []string{"git"}

// catalog holds reusable, cross-stack toolchain tools agents reference by name. Defining
// them once here (rather than duplicating per agent) is what makes the toolchain composable:
// any agent that needs node just lists "node" in a step's Requires. The Check short-circuits
// when a tool is already present (the common case on a real dev image).
func catalog() map[string]agent.Step {
	return map[string]agent.Step{
		"git":  {Name: "git", Check: "git --version", Install: gitInstall},
		"node": {Name: "node", Check: "node --version", Install: nodeInstall},
	}
}

// Plan builds the ordered, deduplicated install plan: base prerequisites + the agent's own
// steps, with any reusable catalog tools they (transitively) require pulled in and ordered
// dependencies-first. Errors on an unknown dependency or a cycle.
func Plan(agentSteps []agent.Step) ([]agent.Step, error) {
	cat := catalog()
	seed := make([]agent.Step, 0, len(baseRequirements)+len(agentSteps))
	for _, name := range baseRequirements {
		if s, ok := cat[name]; ok {
			seed = append(seed, s)
		}
	}
	seed = append(seed, agentSteps...)
	return resolve(cat, seed)
}

// resolve expands `seed` into an ordered, deduped plan: it pulls in any catalog tools the
// steps require (transitively), orders dependencies before dependents (topological sort),
// and dedups by name. Agent-supplied steps override a catalog entry of the same name.
func resolve(cat map[string]agent.Step, seed []agent.Step) ([]agent.Step, error) {
	index := map[string]agent.Step{}
	for name, s := range cat {
		index[name] = s
	}
	order := make([]string, 0, len(seed))
	queued := map[string]bool{}
	for _, s := range seed {
		index[s.Name] = s // an agent step wins over a catalog tool of the same name
		if !queued[s.Name] {
			queued[s.Name] = true
			order = append(order, s.Name)
		}
	}

	const (
		white = iota // unvisited
		gray         // on the current DFS path (cycle marker)
		black        // fully resolved
	)
	color := map[string]int{}
	out := make([]agent.Step, 0, len(index))

	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch color[name] {
		case black:
			return nil
		case gray:
			return fmt.Errorf("toolchain dependency cycle: %s", strings.Join(append(path, name), " → "))
		}
		step, ok := index[name]
		if !ok {
			return fmt.Errorf("unknown toolchain dependency %q (required by %s)", name, requiredBy(path))
		}
		color[name] = gray
		child := append(append([]string{}, path...), name) // own copy — no slice aliasing across siblings
		for _, dep := range step.Requires {
			if err := visit(dep, child); err != nil {
				return err
			}
		}
		color[name] = black
		out = append(out, step)
		return nil
	}

	for _, name := range order {
		if err := visit(name, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func requiredBy(path []string) string {
	if len(path) == 0 {
		return "base"
	}
	return path[len(path)-1]
}

// Run executes the steps. Unless force is set, a passing Check short-circuits the install
// (idempotent "ensure"); force=true re-installs installable steps (update). `onStep` (optional) is
// called as each step finishes, so a caller can report live progress while the run is in flight
// (the API serves it over GET /setup while the install runs in the background).
func Run(ctx context.Context, steps []agent.Step, force bool, onStart func(name string), onStep func(StepResult)) []StepResult {
	return run(ctx, steps, force, nil, func(name, stage string) {
		if stage == "checking" && onStart != nil {
			onStart(name)
		}
	}, onStep)
}

// A tracker shares mutation across harnesses: package managers and shared prerequisites must not
// write over each other. Probes remain concurrent; checks are repeated after acquiring the lock.
func run(ctx context.Context, steps []agent.Step, force bool, mutation chan struct{}, onProgress func(name, stage string), onStep func(StepResult)) []StepResult {
	results := make([]StepResult, 0, len(steps))
	for _, s := range steps {
		progress := func(stage string) {
			if onProgress != nil {
				onProgress(s.Name, stage)
			}
		}
		progress("checking")
		r := runStep(ctx, s, force, mutation, progress)
		results = append(results, r)
		if onStep != nil {
			onStep(r)
		}
		if r.Status == "failed" {
			break // never run dependents after a failed prerequisite
		}
	}
	return results
}

// runStep runs one toolchain step to a result.
func runStep(ctx context.Context, s agent.Step, force bool, mutation chan struct{}, progress func(string)) StepResult {
	fail := func(output string, err error) StepResult {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			output = strings.TrimSpace(output + "\n" + err.Error())
		}
		return StepResult{Name: s.Name, Status: "failed", Output: output}
	}
	check := func() (string, error) {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return command(checkCtx, s.Check)
	}
	// Check-only step (no installer): the Check IS the step. Verify it — even under force there's
	// nothing to reinstall — and report satisfied/failed. (This is why force must not treat an
	// empty Install as a failure.)
	if s.Install == "" {
		if err := ctx.Err(); err != nil {
			return fail("", err)
		}
		if s.Check == "" {
			return StepResult{Name: s.Name, Status: "satisfied"}
		}
		out, err := check()
		if err != nil {
			return fail("check failed and no installer\n"+out, err)
		}
		return StepResult{Name: s.Name, Status: "satisfied"}
	}
	// Installable step: unless forced, a passing Check short-circuits (idempotent ensure).
	if !force && s.Check != "" {
		if _, err := check(); err == nil {
			return StepResult{Name: s.Name, Status: "satisfied"}
		}
	}
	if mutation != nil {
		progress("waiting")
		select {
		case mutation <- struct{}{}:
			defer func() { <-mutation }()
		case <-ctx.Done():
			return fail("", ctx.Err())
		}
		if !force && s.Check != "" {
			if _, err := check(); err == nil {
				return StepResult{Name: s.Name, Status: "satisfied"}
			}
		}
	}
	progress("installing")
	out, err := command(ctx, s.Install)
	if err != nil {
		return fail(out, err)
	}
	if s.Check != "" {
		progress("verifying")
		out, err := check()
		if err != nil {
			return fail("installer finished but verification failed\n"+out, err)
		}
	}
	return StepResult{Name: s.Name, Status: "installed"}
}

func command(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", script)
	proc.Group(cmd) // timeout must stop npm/its children too, so they cannot keep writing after unlock
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// OK reports whether every step ended satisfied or installed.
func OK(results []StepResult) bool {
	for _, r := range results {
		if r.Status == "failed" {
			return false
		}
	}
	return true
}

// ---- catalog install scripts (best-effort, cross-distro; Check gates them) ------------

const gitInstall = `if command -v git >/dev/null 2>&1; then exit 0; fi
SUDO=""; if [ "$(id -u)" != "0" ] && command -v sudo >/dev/null 2>&1; then SUDO="sudo -n"; fi
if command -v apt-get >/dev/null 2>&1; then $SUDO apt-get update -y && $SUDO apt-get install -y git
elif command -v dnf >/dev/null 2>&1; then $SUDO dnf install -y git
elif command -v yum >/dev/null 2>&1; then $SUDO yum install -y git
elif command -v apk >/dev/null 2>&1; then $SUDO apk add --no-cache git
elif command -v pacman >/dev/null 2>&1; then $SUDO pacman -Sy --noconfirm git
elif command -v brew >/dev/null 2>&1; then brew install git
else echo "no supported package manager to install git"; exit 1
fi`

const nodeInstall = `if command -v node >/dev/null 2>&1; then exit 0; fi
SUDO=""; if [ "$(id -u)" != "0" ] && command -v sudo >/dev/null 2>&1; then SUDO="sudo -n"; fi
if command -v apt-get >/dev/null 2>&1; then $SUDO apt-get update -y && $SUDO apt-get install -y nodejs npm
elif command -v dnf >/dev/null 2>&1; then $SUDO dnf install -y nodejs npm
elif command -v apk >/dev/null 2>&1; then $SUDO apk add --no-cache nodejs npm
elif command -v pacman >/dev/null 2>&1; then $SUDO pacman -Sy --noconfirm nodejs npm
elif command -v brew >/dev/null 2>&1; then brew install node
else echo "no supported package manager to install node"; exit 1
fi`
