package setup

import (
	"context"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Status is the wire-shaped snapshot of an agent's toolchain install job: whether an install is in
// flight, its outcome so far, the step currently running (for a live "Installing X…" label), and the
// per-step results appended as the install progresses. JSON tags match the shape the HTTP layer emits
// so callers marshal it unchanged; the Go SDK returns it directly.
type Status struct {
	Running bool         `json:"running"`
	OK      bool         `json:"ok"`
	Started bool         `json:"started"`
	Current string       `json:"current"`
	Steps   []StepResult `json:"steps"`
}

// job is one agent's in-flight install state, mutated under Tracker.mu.
type job struct {
	running bool
	ok      bool
	started bool
	current string
	steps   []StepResult
}

// Tracker runs per-agent toolchain installs in the BACKGROUND (an `npm i -g` can take minutes) so a
// caller's request never hangs while it installs; the caller polls Status for progress + completion.
// Only one install runs at a time per agent — a concurrent Start re-attaches to the in-flight job
// (idempotent, atomic) rather than starting a second run. Both the HTTP surface and the in-process Go
// SDK hold one Tracker; the semantics are identical.
type Tracker struct {
	mu   sync.Mutex
	jobs map[string]*job
}

// NewTracker builds an empty tracker.
func NewTracker() *Tracker { return &Tracker{jobs: map[string]*job{}} }

// Start plans have already been resolved by the caller (via Plan). It launches the install for
// agentID in the background and returns immediately with a snapshot: a fresh {running:true,…} for a
// new run, or the live progress of the job already in flight (idempotent re-attach). The install runs
// on a DETACHED context — a caller/client disconnect can't abort it mid-write — bounded by timeout.
func (t *Tracker) Start(agentID string, steps []agent.Step, force bool, timeout time.Duration) Status {
	t.mu.Lock()
	j := t.jobs[agentID]
	if j == nil {
		j = &job{}
		t.jobs[agentID] = j
	}
	if j.running {
		st := snapshot(j) // already installing — report progress, don't start a second run
		t.mu.Unlock()
		return st
	}
	j.running = true
	j.started = true
	j.ok = false
	j.steps = nil
	j.current = ""
	st := snapshot(j) // taken under the lock, before the goroutine below mutates j
	t.mu.Unlock()

	go func() {
		// Detached context: a client disconnect can't abort the install mid-write; bounded by timeout.
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		results := Run(ctx, steps, force,
			func(name string) { // step started → live "Installing X…"
				t.mu.Lock()
				j.current = name
				t.mu.Unlock()
			},
			func(sr StepResult) { // step finished → live checklist
				t.mu.Lock()
				j.steps = append(j.steps, sr)
				t.mu.Unlock()
			})
		t.mu.Lock()
		j.running = false
		j.current = ""
		j.ok = OK(results)
		j.steps = results
		t.mu.Unlock()
	}()

	return st
}

// Status snapshots agentID's install job. A never-started agent reports the zero Status
// (running/ok/started=false, empty steps).
func (t *Tracker) Status(agentID string) Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	j := t.jobs[agentID]
	if j == nil {
		return Status{Steps: []StepResult{}}
	}
	return snapshot(j)
}

// snapshot copies a job to the wire shape (steps never nil). Caller holds t.mu.
func snapshot(j *job) Status {
	steps := j.steps
	if steps == nil {
		steps = []StepResult{}
	}
	return Status{Running: j.running, OK: j.ok, Started: j.started, Current: j.current, Steps: steps}
}
