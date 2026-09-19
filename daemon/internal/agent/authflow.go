package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"time"
)

// AuthFlow owns a native login's lifetime and generic client state. Both device
// codes and browser/code handoffs use it; credentials stay with the native CLI.
type AuthFlow struct {
	mu     sync.Mutex
	state  AuthState
	ctx    context.Context
	cancel context.CancelFunc
	input  func(string) error
	ready  chan struct{}
	once   sync.Once
}

func NewAuthFlow(method string) *AuthFlow {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	f := &AuthFlow{
		ctx: ctx, cancel: cancel, ready: make(chan struct{}),
		state: AuthState{Method: method, FlowID: rand.Text(), Status: "pending", Message: "Starting sign-in…"},
	}
	go func() {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			f.Fail("Sign-in expired. Start again to get a new sign-in link.")
		}
	}()
	return f
}

func (f *AuthFlow) Context() context.Context { return f.ctx }

func (f *AuthFlow) State() AuthState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *AuthFlow) Active() bool {
	st := f.State().Status
	return st == "pending" || st == "needs_input"
}

// InitialState briefly waits for a URL without binding the native login's
// lifetime to the HTTP request (which ends as soon as this state is returned).
func (f *AuthFlow) InitialState(ctx context.Context) AuthState {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-f.ready:
	case <-ctx.Done():
	case <-timer.C:
	}
	return f.State()
}

func (f *AuthFlow) Update(st AuthState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state.Status == "complete" || f.state.Status == "error" {
		return
	}
	st.Method, st.FlowID = f.state.Method, f.state.FlowID
	f.state = st
	if st.URL != "" || st.Status == "needs_input" {
		f.once.Do(func() { close(f.ready) })
	}
}

func (f *AuthFlow) SetInput(input func(string) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.input = input
}

// Complete commits the selected method only while this attempt is active. A
// canceled or superseded login can never change the daemon's selected account.
func (f *AuthFlow) Complete(commit func() error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state.Status == "complete" || f.state.Status == "error" {
		return
	}
	if f.ctx.Err() != nil {
		f.finishLocked("error", "Sign-in expired. Please start again.")
		return
	}
	if err := commit(); err != nil {
		f.finishLocked("error", "Couldn't save the sign-in. Please try again.")
		return
	}
	f.finishLocked("complete", "Signed in.")
}

func (f *AuthFlow) Fail(message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state.Status != "complete" && f.state.Status != "error" {
		f.finishLocked("error", message)
	}
}

func (f *AuthFlow) Cancel() { f.Fail("Sign-in canceled.") }

func (f *AuthFlow) finishLocked(status, message string) {
	f.state = AuthState{Method: f.state.Method, FlowID: f.state.FlowID, Status: status, Message: message}
	f.input = nil
	f.once.Do(func() { close(f.ready) })
	f.cancel()
}

func (f *AuthFlow) Step(input map[string]string) (AuthState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id := input[AuthFlowIDKey]; id != "" && id != f.state.FlowID {
		return AuthState{}, errors.New("sign-in attempt expired; start again")
	}
	if action := input[AuthActionKey]; action != "" {
		if action != "cancel" || input[AuthFlowIDKey] == "" {
			return AuthState{}, errors.New("cancel requires the sign-in flow ID")
		}
		if f.state.Status != "complete" && f.state.Status != "error" {
			f.finishLocked("error", "Sign-in canceled.")
		}
		return f.state, nil
	}
	if code := strings.TrimSpace(input[AuthCodeKey]); code != "" {
		if len(code) > 4096 || strings.ContainsAny(code, "\r\n\x00") {
			return AuthState{}, errors.New("paste the single authorization code from the sign-in page")
		}
		if f.state.Status != "needs_input" || f.input == nil {
			return AuthState{}, errors.New("this sign-in is not waiting for an authorization code")
		}
		if err := f.input(code); err != nil {
			f.finishLocked("error", "Couldn't send the authorization code. Please start again.")
			return f.state, nil
		}
		f.state.Status, f.state.Message = "pending", "Completing sign-in…"
		f.state.Fields, f.state.Sections = nil, nil
	}
	return f.state, nil
}
