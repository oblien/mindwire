package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestLogoutPreservesActiveRunAndRequiresNewAuthentication(t *testing.T) {
	s, a, _ := newSetupSup(t, nil)
	run, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat", Message: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Logout(context.Background(), a); !errors.Is(err, ErrAuthBusy) {
		t.Fatalf("active sign-out: %v", err)
	}
	if !s.Cancel(run.ID) {
		t.Fatal("sign-out canceled the active chat")
	}
	s.Wait()
	status, err := s.Logout(context.Background(), a)
	if err != nil || status.Configured {
		t.Fatalf("idle sign-out: %+v %v", status, err)
	}
	if _, err = s.StartTurnChecked(a, StartTurnInput{ChatID: "next"}); !errors.Is(err, ErrAuthDisconnected) {
		t.Fatalf("signed-out turn admitted: %v", err)
	}
	if _, err = s.StartResolveChecked(a, StartTurnInput{ChatID: "resolve"}, ResolveOptions{}); !errors.Is(err, ErrAuthDisconnected) {
		t.Fatalf("signed-out resolve admitted: %v", err)
	}
	if _, ok := s.StartCompact(a, StartTurnInput{ChatID: "compact"}); ok {
		t.Fatal("signed-out compaction admitted")
	}
}

type blockingLogout struct {
	fakeAuth
	entered, release chan struct{}
}

func (a *blockingLogout) Logout(context.Context) error { close(a.entered); <-a.release; return nil }

func TestNativeLogoutHoldsTurnAndSetupAdmission(t *testing.T) {
	s, a, _ := newSetupSup(t, nil)
	native := &blockingLogout{entered: make(chan struct{}), release: make(chan struct{})}
	a.Auth = agent.ManageAuth(native, a.Creds)
	done := make(chan error, 1)
	go func() { _, err := s.Logout(context.Background(), a); done <- err }()
	<-native.entered
	if _, err := s.StartTurnChecked(a, StartTurnInput{ChatID: "chat"}); !errors.Is(err, ErrAuthBusy) {
		t.Errorf("turn admitted: %v", err)
	}
	if _, err := s.Setup(a, false); !errors.Is(err, ErrAuthBusy) {
		t.Errorf("setup admitted: %v", err)
	}
	if _, err := s.Logout(context.Background(), a); !errors.Is(err, ErrAuthBusy) {
		t.Errorf("duplicate logout: %v", err)
	}
	if _, err := s.AcquireServiceUpdate(); !errors.Is(err, ErrServiceBusy) {
		t.Errorf("service update admitted: %v", err)
	}
	close(native.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
