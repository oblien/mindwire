package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// DoctorReport is the daemon-level health snapshot for one agent: daemon-generic checks (workspace,
// auth) plus the adapter's own checks (Adapter.Doctor), with ok=false if any check FAILs. JSON tags
// match the shape the HTTP layer emits so api.doctor can marshal it unchanged, and the Go SDK returns
// it directly.
type DoctorReport struct {
	OK     bool          `json:"ok"`
	Checks []agent.Check `json:"checks"`
}

// Doctor reports daemon-level health (workspace, auth) plus the selected agent's own checks
// (Adapter.Doctor) — one report, generic at the daemon, extended per agent. Shared by the HTTP
// surface (api.doctor) and the in-process Go SDK so both report identically.
func (s *Supervisor) Doctor(ctx context.Context, ag *Agent) DoctorReport {
	cwd := s.cwd
	checks := []agent.Check{}

	switch {
	case cwd == "":
		checks = append(checks, agent.Check{Name: "Workspace", Status: agent.CheckWarn, Detail: "AGENT_CWD not set; using the daemon's directory"})
	case workspaceWritable(cwd):
		checks = append(checks, agent.Check{Name: "Workspace", Status: agent.CheckOK, Detail: cwd})
	default:
		checks = append(checks, agent.Check{Name: "Workspace", Status: agent.CheckFail, Detail: cwd + " is missing or not writable"})
	}

	if st := ag.Auth.Status(ctx); st.Configured {
		checks = append(checks, agent.Check{Name: "Authentication", Status: agent.CheckOK, Detail: "method: " + st.Method})
	} else {
		checks = append(checks, agent.Check{Name: "Authentication", Status: agent.CheckFail, Detail: "not configured — set an API key or sign in"})
	}

	checks = append(checks, ag.Adapter.Doctor(ctx)...)

	ok := true
	for _, c := range checks {
		if c.Status == agent.CheckFail {
			ok = false
		}
	}
	return DoctorReport{OK: ok, Checks: checks}
}

// CLIVersion runs the agent's version command and returns the trimmed output ("" on any error). Used
// to report the installed CLI version alongside agent info.
func (s *Supervisor) CLIVersion(ctx context.Context, ag *Agent) string {
	out, err := exec.CommandContext(ctx, "bash", "-lc", ag.Adapter.VersionCommand()).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// workspaceWritable reports whether dir exists, is a directory, and accepts a temp-file write.
func workspaceWritable(dir string) bool {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return false
	}
	f, err := os.CreateTemp(dir, ".pa-doctor-*")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}
