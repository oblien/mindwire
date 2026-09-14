package codex

import "github.com/oblien/mindwire/daemon/internal/agent"

// Notifications declares Codex's inline notification conditions and how each reads. Finished/Errored
// describe terminal runs. WaitingApproval/WaitingFeedback carry their UX so the client and receiver
// can present a paused app-server turn's approval or question (see appserver.go).
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
