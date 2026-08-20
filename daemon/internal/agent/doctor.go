package agent

import (
	"context"
	"os/exec"
	"strings"
)

// Check is one diagnostic result. The daemon's doctor aggregates generic daemon-level
// checks with each agent adapter's own checks (Adapter.Doctor) into one health report —
// so "doctor" is generic at the daemon and EXTENDED per agent.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail,omitempty"`
}

const (
	CheckOK   = "ok"
	CheckWarn = "warn"
	CheckFail = "fail"
)

// CLIDoctor is the health check shared by every npm-distributed CLI adapter: is the CLI installed
// (and which version, via versionCmd), and is Node present (needed to install/update it via the
// toolchain). name is the CLI's display label ("Claude CLI"); npmPkg is the global package the setup
// hint names. Adapters whose Doctor differs beyond these two checks append their own.
func CLIDoctor(ctx context.Context, name, versionCmd, npmPkg string) []Check {
	checks := []Check{}
	if out, err := exec.CommandContext(ctx, "bash", "-lc", versionCmd).CombinedOutput(); err != nil {
		checks = append(checks, Check{Name: name, Status: CheckFail,
			Detail: "not installed — run setup (npm i -g " + npmPkg + ")"})
	} else {
		checks = append(checks, Check{Name: name, Status: CheckOK, Detail: strings.TrimSpace(string(out))})
	}
	if exec.CommandContext(ctx, "bash", "-lc", "command -v node >/dev/null 2>&1").Run() != nil {
		checks = append(checks, Check{Name: "Node.js", Status: CheckWarn,
			Detail: "not found — required to install/update the CLI"})
	} else {
		checks = append(checks, Check{Name: "Node.js", Status: CheckOK})
	}
	return checks
}
