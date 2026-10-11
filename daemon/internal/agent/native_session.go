package agent

import (
	"context"
	"fmt"
)

const NativeSessionVersion = 1

// NativeSessionModule attaches to an existing harness owner. Opening or closing a
// viewer must never start a turn, replace a process, or change its configuration.
// History continues through the existing native History/HistoryPager interfaces.
type NativeSessionModule interface {
	SessionActivity(context.Context, NativeSessionQuery) (SessionActivity, error)
	OpenNativeSession(context.Context, NativeSessionQuery) (NativeSessionConnection, error)
}

type NativeSessionQuery struct {
	SessionID string
	CWD       string
}

type NativeSessionCapabilities struct {
	Status    bool `json:"status"`
	Observe   bool `json:"observe"`
	Input     bool `json:"input"`
	Interrupt bool `json:"interrupt"`
	Respond   bool `json:"respond"`
}

type SessionActivity struct {
	SessionID    string                    `json:"sessionId"`
	CWD          string                    `json:"cwd"`
	Owner        string                    `json:"owner"` // native | none | unknown
	State        string                    `json:"state"` // running | waiting | idle | stopped | unavailable
	TurnID       string                    `json:"turnId,omitempty"`
	StartedAt    string                    `json:"startedAt,omitempty"`
	Reason       string                    `json:"reason,omitempty"`
	Capabilities NativeSessionCapabilities `json:"capabilities"`
}

// NativeSessionUpdate is ordered by the native connection. Initial contains a
// bounded recent-turn seed; Updates begins strictly after that native snapshot.
// Reset starts a new turn. HistoryChanged asks a client to use the ordinary
// paginated history API (e.g. a read-only harness has appended its transcript).
type NativeSessionUpdate struct {
	Activity       *SessionActivity `json:"activity,omitempty"`
	Event          *Event           `json:"event,omitempty"`
	TurnID         string           `json:"turnId,omitempty"`
	Reset          bool             `json:"reset,omitempty"`
	HistoryChanged bool             `json:"historyChanged,omitempty"`
	Partial        bool             `json:"partial,omitempty"` // projection omits earlier/oversized items; merge native history
}

type NativeSessionInitial struct {
	Activity        SessionActivity
	Events          []Event
	TurnID          string
	HistoryRequired bool
}

type NativeSessionConnection interface {
	Initial() NativeSessionInitial
	Updates() <-chan NativeSessionUpdate
	Action(context.Context, NativeSessionAction) (NativeSessionReceipt, error)
	Close()
}

// ConnectionID is supplied by the daemon and makes retries fail closed after an
// attachment expires/restarts. RequestID deduplicates commands on that connection.
// ExpectedTurnID prevents Stop/steer from targeting a newer turn after a race.
type NativeSessionAction struct {
	ConnectionID   string               `json:"connectionId"`
	RequestID      string               `json:"requestId"`
	Kind           string               `json:"kind"` // input | interrupt | response
	ExpectedTurnID string               `json:"expectedTurnId,omitempty"`
	Text           string               `json:"text,omitempty"`
	Attachments    []Attachment         `json:"attachments,omitempty"`
	Response       *InteractionResponse `json:"response,omitempty"`
}

type NativeSessionReceipt struct {
	RequestID string `json:"requestId"`
	Status    string `json:"status"` // accepted | submitted (answer written; await native resolution)
	TurnID    string `json:"turnId,omitempty"`
}

// NativeSessionError is safe for clients. Adapters must not include raw logs or
// credentials. An unknown outcome must never be silently retried as a new send.
type NativeSessionError struct {
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *NativeSessionError) Error() string { return e.Message }

func NativeError(code, message string) error {
	return &NativeSessionError{Code: code, Message: message}
}

func ValidateNativeSessionQuery(q NativeSessionQuery) error {
	if !ValidNativeSessionID(q.SessionID) || q.CWD == "" {
		return NativeError("native_session_invalid", "A native conversation and its project directory are required.")
	}
	return nil
}

func NativeSessionUnavailable(q NativeSessionQuery, reason string) SessionActivity {
	return SessionActivity{SessionID: q.SessionID, CWD: q.CWD, Owner: "unknown", State: "unavailable", Reason: reason}
}

func NativeActionUnsupported(kind string) error {
	return NativeError("native_control_unavailable", fmt.Sprintf("This running conversation does not support %s from another client. Continue in its terminal or close it there before resuming here.", kind))
}
