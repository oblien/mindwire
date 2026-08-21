package mindwire

import (
	"context"
	"net/http"
)

// Auth is the step-flow authentication sub-API, reachable as Client.Auth. It drives the same generic
// methods → begin → step → status flow the HTTP /auth/* routes expose: list the ways to authenticate,
// begin one, feed it inputs until it completes, and read the resting status. It is scoped to its
// client's default agent and rebinds when you take a WithAgent view; override per call with ForAgent.
type Auth struct{ c *Client }

// Methods lists the ways to authenticate the scoped agent (API key, interactive login, …).
func (a *Auth) Methods(opts ...ScopedOption) ([]AuthMethod, error) {
	ag, err := a.c.resolve(opts)
	if err != nil {
		return nil, err
	}
	return ag.Auth.Methods(), nil
}

// Begin starts the named auth method and returns the first step (inputs to collect, a URL to open,
// or completion). An empty method or an error from the module surfaces as APIError{400}, matching
// POST /auth/begin.
func (a *Auth) Begin(ctx context.Context, method string, opts ...ScopedOption) (AuthState, error) {
	ag, err := a.c.resolve(opts)
	if err != nil {
		return AuthState{}, err
	}
	if method == "" {
		return AuthState{}, &APIError{Message: "method is required", Status: http.StatusBadRequest, Op: "Auth.Begin"}
	}
	st, berr := ag.Auth.Begin(ctx, method)
	if berr != nil {
		return AuthState{}, &APIError{Message: berr.Error(), Status: http.StatusBadRequest, Op: "Auth.Begin", Cause: berr}
	}
	return st, nil
}

// Step advances an in-progress auth flow with the collected inputs and returns the next state. A
// module error surfaces as APIError{400}, matching POST /auth/step.
func (a *Auth) Step(ctx context.Context, input map[string]string, opts ...ScopedOption) (AuthState, error) {
	ag, err := a.c.resolve(opts)
	if err != nil {
		return AuthState{}, err
	}
	st, serr := ag.Auth.Step(ctx, input)
	if serr != nil {
		return AuthState{}, &APIError{Message: serr.Error(), Status: http.StatusBadRequest, Op: "Auth.Step", Cause: serr}
	}
	return st, nil
}

// Status reports the scoped agent's resting auth state: whether it's configured, and via which method.
func (a *Auth) Status(ctx context.Context, opts ...ScopedOption) (AuthStatus, error) {
	ag, err := a.c.resolve(opts)
	if err != nil {
		return AuthStatus{}, err
	}
	return ag.Auth.Status(ctx), nil
}
