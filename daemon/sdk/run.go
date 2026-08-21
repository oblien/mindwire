package mindwire

import (
	"context"
	"iter"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/session"
)

// Run is a handle to one turn: its durable record plus the control + streaming operations bound to it.
// Turn, Run, and LatestRun return one. It carries a snapshot of the record (Refresh re-reads it); the
// control methods and Stream always act on the live supervisor state for the run id.
type Run struct {
	core *core
	data session.Run
}

// ID is the run id.
func (r *Run) ID() string { return r.data.ID }

// ChatID is the chat this run belongs to.
func (r *Run) ChatID() string { return r.data.ChatID }

// Agent is the agent type that executed the run.
func (r *Run) Agent() string { return r.data.Agent }

// Status is the run's status as of the last snapshot ("running" | "done" | "error" | "cancelled").
// Call Refresh to re-read it.
func (r *Run) Status() string { return r.data.Status }

// Value returns the run's durable record as of the last snapshot.
func (r *Run) Value() RunRecord { return r.data }

// ---- streaming -------------------------------------------------------------

// StreamOption tunes Stream.
type StreamOption func(*streamConfig)

type streamConfig struct{ openSentinel bool }

// WithOpenSentinel prepends a synthetic {type:"status", meta:{"stream":"open"}} event, reproducing the
// immediate open frame the SSE endpoint flushes before replay. Off by default (the raw hub stream has
// no such frame), matching the TS SDK, which filters it out unless asked.
func WithOpenSentinel() StreamOption {
	return func(c *streamConfig) { c.openSentinel = true }
}

// Stream yields the run's unified events as an iter.Seq: the replay buffer first, then live events
// until the run's topic closes (turn end), ctx is cancelled, or the consumer breaks. The underlying
// subscription is ALWAYS released when the range ends — the "must call cancel" footgun is hidden here.
//
// ctx cancels the READ, not the turn: turns run on the supervisor's detached context, so breaking or
// cancelling stops your consumption while the turn keeps running (identical to the TS signal). Use
// Cancel to actually stop the turn.
func (r *Run) Stream(ctx context.Context, opts ...StreamOption) iter.Seq[Event] {
	cfg := streamConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return func(yield func(Event) bool) {
		replay, ch, done, cancel := r.core.hub.Subscribe(r.data.ID)
		defer cancel()

		if cfg.openSentinel {
			if !yield(Event{Type: EventStatus, Meta: map[string]any{"stream": "open"}}) {
				return
			}
		}
		for _, ev := range replay {
			if !yield(ev) {
				return
			}
		}
		if done {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ev, open := <-ch:
				if !open {
					return
				}
				if !yield(ev) {
					return
				}
			}
		}
	}
}

// Subscribe is the low-level primitive Stream is built on: the replay buffer, a live channel, a done
// flag (true when the run already finished — just drain replay), and a cancel func the caller MUST
// invoke. Prefer Stream; reach for this only to multiplex a run's events with other channels in a
// select.
func (r *Run) Subscribe() (replay []Event, ch <-chan Event, done bool, cancel func()) {
	return r.core.hub.Subscribe(r.data.ID)
}

// ---- control ---------------------------------------------------------------

// RespondInput answers a mid-turn interaction: the interaction id, the chosen decision
// (allow/deny, approve/reject, or a single option id), any multi-select option ids, and free text.
type RespondInput struct {
	InteractionID string
	Decision      string
	Options       []string
	Text          string
}

// Cancel hard-stops the turn (kills the CLI). APIError{400} if the agent doesn't support cancellation;
// APIError{404} if no turn is currently running for this id.
func (r *Run) Cancel() error {
	if err := r.capGate(func(c Capabilities) bool { return c.Cancel }, "this agent does not support cancellation", "Run.Cancel"); err != nil {
		return err
	}
	if !r.core.sup.Cancel(r.data.ID) {
		return notRunning("Run.Cancel")
	}
	return nil
}

// Respond delivers the user's answer to a mid-turn interaction (a permission approval, a question, a
// plan). APIError{400} if the agent takes no interaction responses; APIError{404} if no turn is
// accepting input for this id.
func (r *Run) Respond(in RespondInput) error {
	if err := r.capGate(func(c Capabilities) bool { return c.Respond }, "this agent does not support responding to interactions", "Run.Respond"); err != nil {
		return err
	}
	if !r.core.sup.Respond(r.data.ID, in.InteractionID, in.Decision, in.Options, in.Text) {
		return notAccepting("Run.Respond")
	}
	return nil
}

// SendInput injects a follow-up user message into a running turn (steer without cancelling).
// APIError{400} if unsupported or text is empty; APIError{404} if no turn is accepting input.
func (r *Run) SendInput(text string) error {
	if err := r.capGate(func(c Capabilities) bool { return c.Input }, "this agent does not support mid-turn input", "Run.SendInput"); err != nil {
		return err
	}
	if text == "" {
		return &APIError{Message: "text is required", Status: http.StatusBadRequest, Op: "Run.SendInput"}
	}
	if !r.core.sup.SendInput(r.data.ID, text) {
		return notAccepting("Run.SendInput")
	}
	return nil
}

// Interrupt soft-stops the current action while keeping the turn open for a follow-up (softer than
// Cancel). APIError{400} if unsupported; APIError{404} if no turn is accepting input.
func (r *Run) Interrupt() error {
	if err := r.capGate(func(c Capabilities) bool { return c.Interrupt }, "this agent does not support interrupts", "Run.Interrupt"); err != nil {
		return err
	}
	if !r.core.sup.Interrupt(r.data.ID) {
		return notAccepting("Run.Interrupt")
	}
	return nil
}

// SetModel switches the model of a live turn (empty resets to the agent/CLI default). Best-effort:
// only a persistent transport consumes it. APIError{400} if unsupported; APIError{404} if no turn is
// accepting input.
func (r *Run) SetModel(model string) error {
	if err := r.capGate(func(c Capabilities) bool { return c.SetModel }, "this agent does not support switching the model mid-turn", "Run.SetModel"); err != nil {
		return err
	}
	if !r.core.sup.SetModel(r.data.ID, model) {
		return notAccepting("Run.SetModel")
	}
	return nil
}

// SetPermissionMode switches the permission mode of a live turn (mode required). Same best-effort
// semantics as SetModel. APIError{400} if unsupported or mode is empty; APIError{404} if no turn is
// accepting input.
func (r *Run) SetPermissionMode(mode string) error {
	if err := r.capGate(func(c Capabilities) bool { return c.SetPermissionMode }, "this agent does not support switching the permission mode mid-turn", "Run.SetPermissionMode"); err != nil {
		return err
	}
	if mode == "" {
		return &APIError{Message: "mode is required", Status: http.StatusBadRequest, Op: "Run.SetPermissionMode"}
	}
	if !r.core.sup.SetPermissionMode(r.data.ID, mode) {
		return notAccepting("Run.SetPermissionMode")
	}
	return nil
}

// Children returns handles to this run's child iterations, oldest→newest — the per-iteration turns of a
// global-resolve run (see Client.Resolve). An ordinary turn returns an empty slice. Convenience for
// Client.Children(r.ID()).
func (r *Run) Children() ([]*Run, error) {
	return (&Client{core: r.core}).Children(r.data.ID)
}

// Refresh re-reads the run's durable record from the store, updates this handle's snapshot, and
// returns it. APIError{404} if the run no longer exists.
func (r *Run) Refresh() (RunRecord, error) {
	rec, ok := r.core.store.GetRun(r.data.ID)
	if !ok {
		return RunRecord{}, &APIError{Message: "not found", Status: http.StatusNotFound, Op: "Run.Refresh"}
	}
	r.data = rec
	return rec, nil
}

// capGate returns APIError{400 msg} when the run's agent declares the capability false. An
// unresolvable agent is treated as "allowed" — mirroring api.go, where the gate is skipped when
// Resolve fails and the sup call's false result yields the 404 instead.
func (r *Run) capGate(has func(Capabilities) bool, msg, op string) error {
	if ag, ok := r.core.sup.Resolve(r.data.Agent); ok && !has(ag.Adapter.Capabilities()) {
		return &APIError{Message: msg, Status: http.StatusBadRequest, Op: op}
	}
	return nil
}

func notRunning(op string) error {
	return &APIError{Message: "no running turn for that id", Status: http.StatusNotFound, Op: op}
}

func notAccepting(op string) error {
	return &APIError{Message: "no running turn accepting input for that id", Status: http.StatusNotFound, Op: op}
}

// ---- wait ------------------------------------------------------------------

// WaitResult is the outcome of Wait: the run's final record and the turn's result summary (nil if the
// turn produced no result event, e.g. it was cancelled or errored before finishing).
type WaitResult struct {
	Run    RunRecord
	Result *ResultInfo
}

// WaitOption tunes Wait.
type WaitOption func(*waitConfig)

type waitConfig struct{ noErrOnFail bool }

// NoErrorOnFailure makes Wait return the WaitResult with a nil error even when the run ends in a
// non-"done" terminal state, instead of a *RunFailedError. Use it when you'd rather branch on
// WaitResult.Run.Status yourself.
func NoErrorOnFailure() WaitOption {
	return func(c *waitConfig) { c.noErrOnFail = true }
}

// Wait drains the run's stream to completion (capturing the final result and any error event),
// refreshes the record, and returns it. By default a run that ends in a state other than "done"
// yields a *RunFailedError (with the WaitResult still populated); NoErrorOnFailure suppresses that.
// If ctx is cancelled first, Wait returns the current record and ctx.Err() — the turn keeps running.
func (r *Run) Wait(ctx context.Context, opts ...WaitOption) (WaitResult, error) {
	cfg := waitConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	var result *ResultInfo
	var streamErr string
	for ev := range r.Stream(ctx) {
		switch ev.Type {
		case EventResult:
			result = ev.Result
		case EventError:
			streamErr = ev.Error
		}
	}

	if err := ctx.Err(); err != nil {
		rec, _ := r.Refresh()
		return WaitResult{Run: rec, Result: result}, err
	}

	rec, err := r.Refresh()
	if err != nil {
		return WaitResult{}, err
	}
	if !cfg.noErrOnFail && rec.Status != "done" && isTerminal(rec.Status) {
		detail := rec.Error
		if detail == "" {
			detail = streamErr
		}
		return WaitResult{Run: rec, Result: result}, &RunFailedError{RunID: rec.ID, Status: rec.Status, Detail: detail}
	}
	return WaitResult{Run: rec, Result: result}, nil
}

// isTerminal reports whether a run status is final (no further events will arrive).
func isTerminal(status string) bool {
	switch status {
	case "done", "error", "cancelled":
		return true
	}
	return false
}
