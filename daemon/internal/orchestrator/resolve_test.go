package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// scriptedTurn is one fake RunStream outcome: the adapter emits an EventText + an EventResult
// (mirroring a real adapter's terminal result on the merged stream), then returns the TurnResult the
// resolve loop decides on.
type scriptedTurn struct {
	text    string
	subtype string
	isError bool
}

// fakeAdapter is a CLI-free agent.Adapter whose RunStream replays a fixed script, one entry per turn.
// Past the script it keeps returning a bare "working" success (no sentinel) so a probe loop caps out
// deterministically rather than hanging.
type fakeAdapter struct {
	id    string
	turns []scriptedTurn
	calls int
}

func (f *fakeAdapter) RunStream(_ context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	i := f.calls
	f.calls++
	t := scriptedTurn{text: "working"}
	if i < len(f.turns) {
		t = f.turns[i]
	}
	if t.text != "" {
		emit(agent.Event{Type: agent.EventText, Text: t.text})
	}
	emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: t.text, IsError: t.isError, Subtype: t.subtype}})
	return agent.TurnResult{Text: t.text, IsError: t.isError, Subtype: t.subtype}, nil
}

func (f *fakeAdapter) ID() string                       { return f.id }
func (f *fakeAdapter) Meta() agent.CatalogEntry         { return agent.CatalogEntry{ID: f.id, Name: "Fake"} }
func (f *fakeAdapter) Capabilities() agent.Capabilities { return agent.Capabilities{Resolve: true} }
func (f *fakeAdapter) Settings() agent.SettingsSchema   { return agent.SettingsSchema{} }
func (f *fakeAdapter) InstallSteps() []agent.Step       { return nil }
func (f *fakeAdapter) VersionCommand() string           { return "" }
func (f *fakeAdapter) ConfigPath() string               { return "" }
func (f *fakeAdapter) Auth(agent.CredStore) agent.AuthModule {
	return fakeAuth{}
}
func (f *fakeAdapter) History(agent.HistoryQuery) ([]agent.Message, error) {
	return nil, nil
}
func (f *fakeAdapter) Notifications() agent.NotificationSpec { return agent.NotificationSpec{} }
func (f *fakeAdapter) Doctor(context.Context) []agent.Check  { return nil }

type fakeAuth struct{}

func (fakeAuth) Methods() []agent.AuthMethod { return nil }
func (fakeAuth) Begin(context.Context, string) (agent.AuthState, error) {
	return agent.AuthState{}, nil
}
func (fakeAuth) Step(context.Context, map[string]string) (agent.AuthState, error) {
	return agent.AuthState{}, nil
}
func (fakeAuth) Status(context.Context) agent.AuthStatus { return agent.AuthStatus{} }
func (fakeAuth) EnvForRun() map[string]string            { return nil }

// newResolveSup registers the fake adapter and builds a supervisor defaulting to it. Registration is
// keyed by ID; each test uses a distinct id so a fresh fake (with a reset call counter) backs each run.
func newResolveSup(t *testing.T, fake *fakeAdapter) *Supervisor {
	t.Helper()
	agent.Register(fake)
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return New(store, stream.New(), notify.Fanout(nil), t.TempDir(), fake.id)
}

// drainResolve subscribes to the parent topic and drains it to close, returning every event on the
// merged stream. The resolve goroutine closes the topic as its last act AFTER persisting the parent
// run, so a full drain is the synchronization point for reading final store state.
func drainResolve(t *testing.T, s *Supervisor, runID string) []agent.Event {
	t.Helper()
	replay, ch, done, cancel := s.hub.Subscribe(runID)
	defer cancel()
	evs := append([]agent.Event(nil), replay...)
	if done {
		return evs
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, open := <-ch:
			if !open {
				return evs
			}
			evs = append(evs, ev)
		case <-deadline:
			t.Fatal("timeout draining resolve stream")
		}
	}
}

func lastResult(evs []agent.Event) *agent.ResultInfo {
	var ri *agent.ResultInfo
	for _, e := range evs {
		if e.Type == agent.EventResult {
			ri = e.Result
		}
	}
	return ri
}

func continuations(evs []agent.Event) []*agent.ContinuationInfo {
	var out []*agent.ContinuationInfo
	for _, e := range evs {
		if e.Type == agent.EventContinuation && e.Continuation != nil {
			out = append(out, e.Continuation)
		}
	}
	return out
}

// A continuable stop (error_max_turns) auto-resumes; the next turn emits the completion sentinel, so
// the loop ends done in two iterations. The sentinel is stripped from the aggregate and the aggregate
// EventResult is published LAST (so a waiter reads it, not a sub-turn's result).
func TestResolveContinueThenDone(t *testing.T) {
	fake := &fakeAdapter{id: "fake-continue-done", turns: []scriptedTurn{
		{text: "working on it", subtype: "error_max_turns"},
		{text: "all finished <<RESOLVE_DONE>>"},
	}}
	s := newResolveSup(t, fake)
	ag, ok := s.Resolve(fake.id)
	if !ok {
		t.Fatal("fake agent not resolvable")
	}

	parent, ok := s.StartResolve(ag, StartTurnInput{ChatID: "c1", Message: "do the thing"}, ResolveOptions{})
	if !ok {
		t.Fatal("StartResolve returned ok=false")
	}
	if parent.Kind != "resolve" {
		t.Errorf("parent.Kind = %q, want resolve", parent.Kind)
	}
	evs := drainResolve(t, s, parent.ID)

	rec, ok := s.store.GetRun(parent.ID)
	if !ok {
		t.Fatal("parent run not in store")
	}
	if rec.Status != "done" || rec.StopReason != "done" || rec.Iterations != 2 {
		t.Errorf("parent = %+v, want status=done stopReason=done iterations=2", rec)
	}
	if ri := lastResult(evs); ri == nil || ri.Text != "all finished" {
		t.Errorf("aggregate result = %+v, want text=%q (sentinel stripped)", ri, "all finished")
	}

	children := s.store.Children(parent.ID)
	if len(children) != 2 {
		t.Fatalf("children = %d, want 2", len(children))
	}
	for i, c := range children {
		if c.ParentID != parent.ID {
			t.Errorf("child[%d].ParentID = %q, want %q", i, c.ParentID, parent.ID)
		}
		if c.Status != "done" {
			t.Errorf("child[%d].Status = %q, want done", i, c.Status)
		}
	}

	// The continuation boundaries delimit the sub-turns: iteration 0 starts, iteration 1 resumes a
	// max_turns stop, and a final boundary carries the terminal stopReason.
	cs := continuations(evs)
	if len(cs) < 3 {
		t.Fatalf("continuations = %d, want >=3 (start, max_turns, final)", len(cs))
	}
	if cs[0].Reason != "start" || cs[1].Reason != "max_turns" {
		t.Errorf("continuation reasons = %q, %q; want start, max_turns", cs[0].Reason, cs[1].Reason)
	}
	if cs[len(cs)-1].StopReason != "done" {
		t.Errorf("final continuation stopReason = %q, want done", cs[len(cs)-1].StopReason)
	}
}

// An agent that never emits the sentinel is bounded by maxIterations: the loop probes up to the cap,
// then ends with stopReason "capped" (never a silent stop) while the parent status is still "done".
func TestResolveCapped(t *testing.T) {
	fake := &fakeAdapter{id: "fake-capped"} // empty script → always "working", no sentinel
	s := newResolveSup(t, fake)
	ag, _ := s.Resolve(fake.id)

	parent, ok := s.StartResolve(ag, StartTurnInput{ChatID: "c1", Message: "loop forever"}, ResolveOptions{MaxIterations: 3})
	if !ok {
		t.Fatal("StartResolve returned ok=false")
	}
	drainResolve(t, s, parent.ID)

	rec, _ := s.store.GetRun(parent.ID)
	if rec.Status != "done" || rec.StopReason != "capped" || rec.Iterations != 3 {
		t.Errorf("parent = %+v, want status=done stopReason=capped iterations=3", rec)
	}
	if got := len(s.store.Children(parent.ID)); got != 3 {
		t.Errorf("children = %d, want 3", got)
	}
}

// A genuine error on the first turn stops the resolve immediately: parent errored, stopReason "error",
// one iteration, and the child recorded as errored.
func TestResolveError(t *testing.T) {
	fake := &fakeAdapter{id: "fake-error", turns: []scriptedTurn{
		{text: "boom", isError: true},
	}}
	s := newResolveSup(t, fake)
	ag, _ := s.Resolve(fake.id)

	parent, ok := s.StartResolve(ag, StartTurnInput{ChatID: "c1", Message: "explode"}, ResolveOptions{})
	if !ok {
		t.Fatal("StartResolve returned ok=false")
	}
	drainResolve(t, s, parent.ID)

	rec, _ := s.store.GetRun(parent.ID)
	if rec.Status != "error" || rec.StopReason != "error" || rec.Iterations != 1 {
		t.Errorf("parent = %+v, want status=error stopReason=error iterations=1", rec)
	}
	if rec.Error != "boom" {
		t.Errorf("parent.Error = %q, want boom", rec.Error)
	}
	children := s.store.Children(parent.ID)
	if len(children) != 1 || children[0].Status != "error" {
		t.Errorf("children = %+v, want 1 errored child", children)
	}
}

// A second resolve (or turn) for a chat with a live resolve is rejected — one turn per chat.
func TestResolveBusyGuard(t *testing.T) {
	// A script long enough that the first resolve is still running when the second StartResolve races
	// in. Each "working" success triggers a probe continue, so 50 iterations keeps it busy.
	fake := &fakeAdapter{id: "fake-busy"}
	s := newResolveSup(t, fake)
	ag, _ := s.Resolve(fake.id)

	parent, ok := s.StartResolve(ag, StartTurnInput{ChatID: "c1", Message: "long task"}, ResolveOptions{MaxIterations: 50})
	if !ok {
		t.Fatal("first StartResolve returned ok=false")
	}
	if _, ok := s.StartResolve(ag, StartTurnInput{ChatID: "c1", Message: "again"}, ResolveOptions{}); ok {
		t.Error("second StartResolve should be rejected while a resolve is live")
	}
	if _, ok := s.StartTurn(ag, StartTurnInput{ChatID: "c1", Message: "again"}); ok {
		t.Error("StartTurn should be rejected while a resolve is live for the chat")
	}
	s.Cancel(parent.ID)
	drainResolve(t, s, parent.ID)
}
