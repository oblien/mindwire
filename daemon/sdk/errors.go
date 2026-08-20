package mindwire

import "fmt"

// errors.go mirrors the TypeScript SDK's error classes (MindwireError / ApiError / RunFailedError) as
// idiomatic Go error types. Each is a distinct type with its own Error() so callers branch with
// errors.As; Unwrap exposes an underlying cause for errors.Is.
//
// Note on shape: the plan sketched APIError as embedding a base named Error. Go can't spell that safely
// — a struct field literally named Error shadows the promoted Error() method and the type stops
// implementing the error interface. So each type is standalone instead, which reads the same at the
// call site (errors.As(err, &apiErr); apiErr.Status == 409).

// Error is the SDK's base error, returned for failures that don't carry an HTTP-style status — chiefly
// New() failing to open the state file. Cause is the wrapped underlying error, if any.
type Error struct {
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

// APIError is returned by operations whose HTTP-daemon equivalent would answer with a non-2xx status,
// so an in-process caller can branch on Status exactly as an HTTP client would:
//
//	400 — bad input or an option the target agent doesn't support
//	404 — no such run (or no running turn accepting that operation)
//	409 — a turn is already running for the chat
//	500 — a persistence failure
//
// Op names the SDK operation that failed (e.g. "Turn", "Run.Cancel"); Message is the same human string
// the HTTP layer returns; Cause wraps the underlying error when there is one.
type APIError struct {
	Message string
	Status  int
	Op      string
	Cause   error
}

func (e *APIError) Error() string {
	if e.Op != "" {
		return fmt.Sprintf("mindwire: %s: %s (status %d)", e.Op, e.Message, e.Status)
	}
	return fmt.Sprintf("mindwire: %s (status %d)", e.Message, e.Status)
}

func (e *APIError) Unwrap() error { return e.Cause }

// RunFailedError is returned by Run.Wait when a run reaches a terminal state other than "done"
// (i.e. "error" or "cancelled"), unless the caller passed NoErrorOnFailure. Wait still returns the
// WaitResult alongside it, so a caller that wants the record can read both.
type RunFailedError struct {
	RunID  string
	Status string // "error" | "cancelled"
	Detail string
}

func (e *RunFailedError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("mindwire: run %s %s: %s", e.RunID, e.Status, e.Detail)
	}
	return fmt.Sprintf("mindwire: run %s %s", e.RunID, e.Status)
}
