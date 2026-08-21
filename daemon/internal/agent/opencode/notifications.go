package opencode

import "github.com/oblien/mindwire/daemon/internal/agent"

// Notifications declares opencode's inline notification conditions. Finished/Errored fire on every turn
// (a turn always ends in session.idle or session.error). WaitingApproval carries its UX so the client
// and receiver know the shape; it fires when a permission-mode="ask" turn pauses on a permission ask
// (see server.go). opencode has no mid-turn free-text feedback request, so WaitingFeedback is omitted.
func (adapter) Notifications() agent.NotificationSpec {
	return agent.NotificationSpec{Conditions: []agent.ConditionUX{
		{Condition: agent.Finished, Title: "opencode finished"},
		{Condition: agent.Errored, Title: "opencode hit an error"},
		{Condition: agent.WaitingApproval, Title: "opencode needs your approval", Actions: []agent.Action{
			{ID: "allow", Label: "Approve"},
			{ID: "deny", Label: "Reject"},
		}},
	}}
}
