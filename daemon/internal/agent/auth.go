package agent

import (
	"context"
	"fmt"
	"strings"
)

// AuthSection groups declared field keys in display order. Titles and help belong
// to the daemon; clients render native form sections without provider-specific UI.
type AuthSection struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Help      string   `json:"help,omitempty"`
	FieldKeys []string `json:"fieldKeys"`
}

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
	ID          string        `json:"id"`
	Label       string        `json:"label"`
	Scope       Scope         `json:"scope,omitempty"` // unified | custom (taxonomy; empty = unified)
	Help        string        `json:"help,omitempty"`
	Interactive bool          `json:"interactive,omitempty"`
	Fields      []Field       `json:"fields,omitempty"` // reuses the settings Field shape
	Sections    []AuthSection `json:"sections,omitempty"`
}

// AuthState is the current step of an in-progress auth flow.
type AuthState struct {
	Method   string        `json:"method"`
	Status   string        `json:"status"`           // "needs_input" | "pending" | "complete" | "error"
	FlowID   string        `json:"flowId,omitempty"` // echo as _flowId when polling, submitting, or canceling
	URL      string        `json:"url,omitempty"`
	Code     string        `json:"code,omitempty"`
	Message  string        `json:"message,omitempty"`
	Fields   []Field       `json:"fields,omitempty"` // inputs the client should collect next
	Sections []AuthSection `json:"sections,omitempty"`
}

// Interactive auth uses the same step endpoint as field forms. These reserved
// keys scope a request to one attempt; cancellation must always include its ID.
const (
	AuthFlowIDKey = "_flowId"
	AuthActionKey = "_action"
	AuthCodeKey   = "authorizationCode"
)

// AuthStatus is the resting state: is the agent authenticated, and via which method.
type AuthStatus struct {
	Configured bool   `json:"configured"`
	SignedOut  bool   `json:"signedOut,omitempty"`
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

// NormalizeAuthInput applies the same declared defaults, visibility, and required
// rules as the client. Hidden drafts never take precedence over the chosen mode,
// even when a client submits them. Unknown keys are not forwarded to an adapter.
func NormalizeAuthInput(fields []Field, input map[string]string) (map[string]string, error) {
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		value, provided := input[f.Key]
		if !provided {
			value = f.Default
		}
		values[f.Key] = strings.TrimSpace(value)
	}
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.VisibleWhen == nil || values[f.VisibleWhen.Key] == f.VisibleWhen.Equals {
			out[f.Key] = values[f.Key]
		}
	}
	for _, f := range fields {
		value, visible := out[f.Key]
		if !visible {
			continue
		}
		required := f.Required
		for _, alternative := range f.RequiredUnless {
			if out[alternative] != "" {
				required = false
			}
		}
		if required && value == "" && f.Type != FieldToggle {
			return nil, fmt.Errorf("enter %s", strings.ToLower(f.Label))
		}
		if f.Type == FieldSelect && value != "" {
			valid := false
			for _, option := range f.Options {
				valid = valid || option.Value == value
			}
			if !valid {
				return nil, fmt.Errorf("choose a valid option for %s", strings.ToLower(f.Label))
			}
		}
	}
	return out, nil
}
