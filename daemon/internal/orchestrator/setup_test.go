package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/setup"
	"github.com/oblien/mindwire/daemon/internal/stream"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

type setupAdapter struct {
	fakeAdapter
	steps    []agent.Step
	turnGate chan struct{}
}

type managedSetupAdapter struct{ setupAdapter }

func (f *managedSetupAdapter) Toolchain() toolchain.Spec {
	return toolchain.Spec{ID: f.ID(), Name: "Managed CLI", Binary: "mindwire-test-cli", VersionArgs: []string{"--version"}}
}

func TestManagedSetupPreservesDeclaredDependenciesAndOtherSteps(t *testing.T) {
	base, _, _ := newSetupSup(t, nil)
	fake := &managedSetupAdapter{setupAdapter: setupAdapter{fakeAdapter: fakeAdapter{id: "managed-dependencies"}, steps: []agent.Step{
		{Name: "sandbox dependency", Check: "false"},
		{Name: "Managed CLI", Requires: []string{"sandbox dependency"}},
	}}}
	plan, err := base.setupPlan(&Agent{Adapter: fake}, false)
	if err != nil {
		t.Fatal(err)
	}
	results := setup.Run(context.Background(), plan, false, nil, nil)
	last := results[len(results)-1]
	if last.Name != "sandbox dependency" || last.Status != "failed" {
		t.Fatalf("managed installation bypassed a failed prerequisite: %+v", results)
	}
}

func (f *setupAdapter) InstallSteps() []agent.Step { return f.steps }
func (f *setupAdapter) RunStream(ctx context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	select {
	case <-ctx.Done():
		return agent.TurnResult{}, ctx.Err()
	case <-f.turnGate:
		result := agent.TurnResult{Text: "done <<RESOLVE_DONE>>"}
		emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: result.Text}})
		return result, nil
	}
}

func newSetupSup(t *testing.T, steps []agent.Step) (*Supervisor, *Agent, chan struct{}) {
	t.Helper()
	gate := make(chan struct{})
	fake := &setupAdapter{fakeAdapter: fakeAdapter{id: "setup-" + t.Name()}, steps: steps, turnGate: gate}
	agent.Register(fake)
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, stream.New(), notify.Fanout(nil), t.TempDir(), fake.ID())
	ag, _ := s.Resolve("")
	t.Cleanup(func() { close(gate); s.Wait() })
	return s, ag, gate
}

func waitSetup(t *testing.T, s *Supervisor, ag *Agent) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !s.SetupStatus(ag).Running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("setup did not finish")
}

func TestSetupRejectsNewTurnCompactAndResolveUntilVerified(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	quote := "'" + strings.ReplaceAll(release, "'", "'\\''") + "'"
	s, ag, _ := newSetupSup(t, []agent.Step{{Name: "cli", Install: "while [ ! -f " + quote + " ]; do sleep 0.01; done", Check: "test -f " + quote}})
	status, err := s.Setup(ag, false)
	if err != nil || !status.Running {
		t.Fatalf("setup: %+v %v", status, err)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600); waitSetup(t, s, ag) })
	for _, start := range []func(*Agent, StartTurnInput) (session.Run, bool){
		s.StartTurn, s.StartCompact,
		func(a *Agent, in StartTurnInput) (session.Run, bool) { return s.StartResolve(a, in, ResolveOptions{}) },
	} {
		if _, ok := start(ag, StartTurnInput{ChatID: "chat", Message: "hello"}); ok {
			t.Fatal("turn admitted while CLI was installing")
		}
	}
	if messages := s.store.Messages("chat"); len(messages) != 0 {
		t.Fatalf("rejected turns must not create history: %+v", messages)
	}
	if !strings.Contains(s.StartConflict(ag), "setup") {
		t.Fatal("client needs an actionable setup conflict")
	}
	if joined, err := s.Setup(ag, true); err != nil || joined.Operation != "setup" {
		t.Fatalf("update must attach to setup: %+v %v", joined, err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitSetup(t, s, ag)
	if !s.SetupStatus(ag).OK {
		t.Fatal("CLI should be verified")
	}
	run, ok := s.StartTurn(ag, StartTurnInput{ChatID: "chat", Message: "hello"})
	if !ok {
		t.Fatal("verified setup must release turn admission")
	}
	defer s.Cancel(run.ID)
	if _, err := s.Setup(ag, true); !errors.Is(err, ErrAgentBusy) {
		t.Fatalf("update replaced a running CLI: %v", err)
	}
}

func TestConcurrentSetupAndTurnHaveOneWinner(t *testing.T) {
	// Both contenders hold their resource until observed, so exactly one can win each race.
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	quote := "'" + strings.ReplaceAll(release, "'", "'\\''") + "'"
	s, ag, _ := newSetupSup(t, []agent.Step{{Name: "cli", Install: "while [ ! -f " + quote + " ]; do sleep 0.01; done"}})
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600); waitSetup(t, s, ag) })
	var wg sync.WaitGroup
	var setupWon, turnWon bool
	var run session.Run
	start := make(chan struct{})
	wg.Go(func() { <-start; _, err := s.Setup(ag, true); setupWon = err == nil })
	wg.Go(func() { <-start; run, turnWon = s.StartTurn(ag, StartTurnInput{ChatID: "chat", Message: "hello"}) })
	close(start)
	wg.Wait()
	if turnWon {
		defer s.Cancel(run.ID)
	}
	if setupWon == turnWon {
		t.Fatalf("exactly one may start: setup=%v turn=%v", setupWon, turnWon)
	}
}
