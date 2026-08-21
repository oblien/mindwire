package codex

import "github.com/oblien/mindwire/daemon/internal/agent"

// Notifications declares Codex's inline notification conditions and how each reads. Finished/Errored
// fire on every turn (the exec hot path runs to completion). WaitingApproval/WaitingFeedback carry
// their UX so the client and receiver know the shape; they fire on the app-server transport, when a
// non-`never` approval turn pauses for the user (see appserver.go).
func (adapter) Notifications() agent.NotificationSpec {
	return agent.NotificationSpec{Conditions: []agent.ConditionUX{
		{Condition: agent.Finished, Title: "Codex finished"},
		{Condition: agent.Errored, Title: "Codex hit an error"},
		{Condition: agent.WaitingApproval, Title: "Codex needs your approval", Actions: []agent.Action{
			{ID: "approve", Label: "Approve"},
			{ID: "reject", Label: "Reject"},
		}},
		{Condition: agent.WaitingFeedback, Title: "Codex is waiting for your reply"},
	}}
}
