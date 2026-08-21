package claude

import "github.com/oblien/mindwire/daemon/internal/agent"

// Notifications is Claude's inline notification adapter: the conditions it produces
// and how each reads. v1 emits Finished/Errored (Claude runs to completion under
// bypassPermissions). WaitingApproval/WaitingFeedback are declared with their UX so
// the client and receiver know the shape — they're emitted once the interactive
// approval-pause flow lands (run without bypassPermissions).
func (adapter) Notifications() agent.NotificationSpec {
	return agent.NotificationSpec{Conditions: []agent.ConditionUX{
		{Condition: agent.Finished, Title: "Claude finished"},
		{Condition: agent.Errored, Title: "Claude hit an error"},
		{Condition: agent.WaitingApproval, Title: "Claude needs your approval", Actions: []agent.Action{
			{ID: "approve", Label: "Approve"},
			{ID: "reject", Label: "Reject"},
		}},
		{Condition: agent.WaitingFeedback, Title: "Claude is waiting for your reply"},
	}}
}
