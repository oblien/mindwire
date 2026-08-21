// Package proc configures spawned commands so that cancelling their context tears
// down the WHOLE process tree, not just the shell that Go directly forked.
//
// Every agent (and every auth/doctor helper) runs as `bash -lc <cmd>`. On ctx
// cancel or timeout the os/exec default kills only that bash parent, orphaning the
// real grandchild it spawned — node/claude/codex — which then keeps running:
// burning API budget, holding locks, and mutating the workspace long after the turn
// was "cancelled". Worse, the orphan inherits the stdout pipe's write end, so the
// parser may never see EOF and cmd.Wait() can block past cancellation.
//
// Group() addresses both: it puts the child in its own process group and overrides
// the command's Cancel to signal the entire group, and it sets WaitDelay so a
// lingering grandchild that kept the pipe open can't wedge Wait() forever. Call it
// immediately after exec.CommandContext, before Start.
package proc

import (
	"context"
	"time"
)

// killGrace bounds how long Wait() blocks after the process exits (or its context
// is cancelled and Cancel has run): if the I/O pipes still aren't drained — a
// grandchild kept a copy of the write end — Wait force-closes them and returns
// instead of hanging. Generous enough not to truncate a normal final flush.
const killGrace = 5 * time.Second

// Reporter is called with the group-leader pid of a spawned process, right after Start. It is how a
// turn's process group becomes visible to resource monitoring WITHOUT coupling the driver/adapter to
// the monitor: the orchestrator installs one on a turn's context via WithReporter, and each spawn
// site calls Report(ctx, cmd.Process.Pid) after cmd.Start(). Because the child is its own group
// leader (Group set Setpgid), that pid IS the whole process tree's group id.
type Reporter func(pid int)

type reporterKey struct{}

// WithReporter returns a context carrying r, retrievable by Report. A nil r is ignored (returns ctx
// unchanged), so callers can wire it unconditionally.
func WithReporter(ctx context.Context, r Reporter) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterKey{}, r)
}

// Report invokes the context's Reporter (if any) with pid. It is a no-op when no reporter is
// installed — e.g. auth/doctor helpers, or a daemon built without monitoring wired — so every spawn
// site can call it unconditionally. Safe with any context.
func Report(ctx context.Context, pid int) {
	if r, ok := ctx.Value(reporterKey{}).(Reporter); ok && r != nil {
		r(pid)
	}
}
