package orchestrator

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestServiceUpdateLeaseBlocksAllRunKindsAndSetup(t *testing.T) {
	s, a, _ := newSetupSup(t, nil)
	lease, err := s.AcquireServiceUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat"}); !errors.Is(err, ErrServiceUpdating) {
		t.Fatalf("turn: %v", err)
	}
	if _, ok := s.StartCompact(a, StartTurnInput{ChatID: "compact"}); ok {
		t.Fatal("compaction admitted during update")
	}
	if _, err := s.StartResolveChecked(a, StartTurnInput{ChatID: "resolve"}, ResolveOptions{}); !errors.Is(err, ErrServiceUpdating) {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := s.Setup(a, false); !errors.Is(err, ErrServiceUpdating) {
		t.Fatalf("setup: %v", err)
	}
	if _, err := s.BeginServiceOperation(); !errors.Is(err, ErrServiceUpdating) {
		t.Fatalf("mutation: %v", err)
	}
	if _, err := s.AcquireServiceUpdate(); !errors.Is(err, ErrServiceUpdating) {
		t.Fatalf("duplicate installer: %v", err)
	}
	if s.ReleaseServiceUpdate("wrong") {
		t.Fatal("wrong lease released another installer")
	}
	if st := s.ServiceUpdateState(); st.Idle || !st.Updating {
		t.Fatalf("state: %+v", st)
	}
	if !s.ReleaseServiceUpdate(lease.ID) || !s.ServiceUpdateState().Idle {
		t.Fatal("release did not reopen admission")
	}
}

func TestManualServiceUpdateInterruptsWorkButNeverStealsAnInstallerLease(t *testing.T) {
	s, _, _ := newSetupSup(t, nil)
	done, err := s.BeginServiceOperation()
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, err := s.AcquireServiceUpdate(); !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("automatic update: %v", err)
	}
	lease, err := s.AcquireServiceUpdate(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireServiceUpdate(true); !errors.Is(err, ErrServiceUpdating) {
		t.Fatalf("duplicate manual update: %v", err)
	}
	if _, err := s.BeginServiceOperation(); !errors.Is(err, ErrServiceUpdating) {
		t.Fatalf("new work admitted: %v", err)
	}
	if !s.ReleaseServiceUpdate(lease.ID) {
		t.Fatal("release failed")
	}
	if s.ServiceUpdateState().Idle {
		t.Fatal("manual authorization must not pretend running work ended")
	}
}

func TestServiceShutdownCancelsTurnsBeforeFinishingAndClosesAdmission(t *testing.T) {
	s, a, _ := newSetupSup(t, nil)
	run, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "restart", Message: "work"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Cancel(run.ID) {
		t.Fatal("shutdown returned before the turn finalized")
	}
	if _, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "new"}); !errors.Is(err, ErrServiceUpdating) {
		t.Fatalf("shutdown admitted work: %v", err)
	}
}

func TestServiceUpdateRefusesActiveTurnUntilFinalSave(t *testing.T) {
	s, a, _ := newSetupSup(t, nil)
	run, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireServiceUpdate(); !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("active turn: %v", err)
	}
	if !s.Cancel(run.ID) {
		t.Fatal("run cannot be cancelled")
	}
	s.Wait()
	if _, err := s.AcquireServiceUpdate(); err != nil {
		t.Fatalf("completed turn stayed busy: %v", err)
	}
}

func TestServiceUpdateRefusesSetupAndNativeLogin(t *testing.T) {
	gate := make(chan struct{})
	s, a, _ := newSetupSup(t, []agent.Step{{Name: "fixture", CheckFunc: func(context.Context) (string, error) { return "", os.ErrNotExist },
		InstallFunc: func(context.Context) (string, error) { <-gate; return "done", nil }}})
	status, err := s.Setup(a, false)
	if err != nil || !status.Running {
		t.Fatalf("setup: %+v %v", status, err)
	}
	_, updateErr := s.AcquireServiceUpdate()
	close(gate)
	waitSetup(t, s, a)
	if !errors.Is(updateErr, ErrServiceBusy) {
		t.Fatalf("active setup: %v", updateErr)
	}
	a.Auth = &activeUpdateAuth{active: true}
	if _, err := s.AcquireServiceUpdate(); !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("active login: %v", err)
	}
	a.Auth = &activeUpdateAuth{}
	if _, err := s.AcquireServiceUpdate(); err != nil {
		t.Fatalf("completed login: %v", err)
	}
}

type activeUpdateAuth struct {
	fakeAuth
	active bool
}

func (a *activeUpdateAuth) Active() bool { return a.active }

func TestServiceUpdateExpiryAndAdmissionAreAtomic(t *testing.T) {
	s, _, _ := newSetupSup(t, nil)
	lease, err := s.AcquireServiceUpdate()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.updateLease.ExpiresAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	done, err := s.BeginServiceOperation()
	if err != nil {
		t.Fatalf("expired lease blocked admission: %v", err)
	}
	if s.ReleaseServiceUpdate(lease.ID) {
		t.Fatal("expired lease was retained")
	}
	if _, err := s.AcquireServiceUpdate(); !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("request not counted: %v", err)
	}
	done()
	done() // release is idempotent
	for i := 0; i < 100; i++ {
		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})
		var finish func()
		var operationErr, leaseErr error
		var winner ServiceUpdateLease
		go func() { defer wg.Done(); <-start; finish, operationErr = s.BeginServiceOperation() }()
		go func() { defer wg.Done(); <-start; winner, leaseErr = s.AcquireServiceUpdate() }()
		close(start)
		wg.Wait()
		if (operationErr == nil) == (leaseErr == nil) {
			t.Fatalf("both/neither admitted: operation=%v lease=%v", operationErr, leaseErr)
		}
		if finish != nil {
			finish()
		}
		if leaseErr == nil {
			s.ReleaseServiceUpdate(winner.ID)
		}
	}
}
