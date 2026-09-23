package agent

import "os"

// WorkspaceIsolationVersion describes the placement contract reported by /healthz.
const WorkspaceIsolationVersion = 1

// WorkspaceIsolation is set by the trusted launcher, not by a profile or a turn.
// Container launchers provide the filesystem/process boundary themselves. In
// particular, Codex must not require a second set of Linux namespaces inside an
// unprivileged Docker container. Approval policy remains independent of placement.
func WorkspaceIsolation() string {
	if os.Getenv("MINDWIRE_ISOLATION") == "container" {
		return "container"
	}
	return "direct"
}
