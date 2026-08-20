package agent

import "context"

// AuthMethod is one way to authenticate an agent — the client shows these as an
// options list. A field-based method collects Fields; an interactive one drives a
// begin→step→status flow (e.g. a login command that surfaces a URL).
//
// Scope carries the same taxonomy as a settings Field: a UNIFIED method is a cross-agent
// authentication concept (an API key, an interactive login, a gateway bearer token — one every
// agent has some form of); a CUSTOM method is agent-specific (e.g. Claude's Bedrock/Vertex/Foundry
// cloud providers), declared + typed in the adapter, never open passthrough. Empty scope reads as
// unified for backward compatibility.
type AuthMethod struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Scope       Scope   `json:"scope,omitempty"` // unified | custom (taxonomy; empty = unified)
	Help        string  `json:"help,omitempty"`
	Interactive bool    `json:"interactive,omitempty"`
	Fields      []Field `json:"fields,omitempty"` // reuses the settings Field shape
}

// AuthState is the current step of an in-progress auth flow.
type AuthState struct {
	Method  string  `json:"method"`
	Status  string  `json:"status"` // "needs_input" | "pending" | "complete" | "error"
	URL     string  `json:"url,omitempty"`
	Code    string  `json:"code,omitempty"`
	Message string  `json:"message,omitempty"`
	Fields  []Field `json:"fields,omitempty"` // inputs the client should collect next
}

// AuthStatus is the resting state: is the agent authenticated, and via which method.
type AuthStatus struct {
	Configured bool   `json:"configured"`
	Method     string `json:"method,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// AuthModule is an agent's auth logic. The client drives a generic
// methods → begin → step → status flow; the module (bound to a CredStore at
// construction) handles each method's specifics and persists credentials.
type AuthModule interface {
	Methods() []AuthMethod
	Begin(ctx context.Context, methodID string) (AuthState, error)
	Step(ctx context.Context, input map[string]string) (AuthState, error)
	Status(ctx context.Context) AuthStatus
	EnvForRun() map[string]string
}
