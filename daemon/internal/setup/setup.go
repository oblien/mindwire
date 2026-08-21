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

	"github.com/oblien/mindwire/daemon/internal/agent"
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
	results := make([]StepResult, 0, len(steps))
	for _, s := range steps {
		if onStart != nil {
			onStart(s.Name) // report the step about to run, so the client shows "Installing X…" live
		}
		r := runStep(ctx, s, force)
		results = append(results, r)
		if onStep != nil {
			onStep(r)
		}
	}
	return results
}

// runStep runs one toolchain step to a result.
func runStep(ctx context.Context, s agent.Step, force bool) StepResult {
	// Check-only step (no installer): the Check IS the step. Verify it — even under force there's
	// nothing to reinstall — and report satisfied/failed. (This is why force must not treat an
	// empty Install as a failure.)
	if s.Install == "" {
		if s.Check == "" || exec.CommandContext(ctx, "bash", "-lc", s.Check).Run() == nil {
			return StepResult{Name: s.Name, Status: "satisfied"}
		}
		return StepResult{Name: s.Name, Status: "failed", Output: "check failed and no installer"}
	}
	// Installable step: unless forced, a passing Check short-circuits (idempotent ensure).
	if !force && s.Check != "" {
		if exec.CommandContext(ctx, "bash", "-lc", s.Check).Run() == nil {
			return StepResult{Name: s.Name, Status: "satisfied"}
		}
	}
	out, err := exec.CommandContext(ctx, "bash", "-lc", s.Install).CombinedOutput()
	if err != nil {
		return StepResult{Name: s.Name, Status: "failed", Output: strings.TrimSpace(string(out))}
	}
	return StepResult{Name: s.Name, Status: "installed"}
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
