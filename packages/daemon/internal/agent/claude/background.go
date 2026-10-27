package claude

import (
	"encoding/json"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

// Empty background rosters may precede task_notification. Retain a known task
// until its explicit terminal notification, and ignore late progress replays.
func backgroundUpdates(raw []byte) ([]agent.TaskInfo, bool) {
	var event struct {
		Subtype                      string `json:"subtype"`
		ID                           string `json:"task_id"`
		ParentID                     string `json:"parent_task_id"`
		Kind                         string `json:"task_type"`
		Description, Status, Summary string
		Background                   *bool `json:"is_backgrounded"`
		Tasks                        []struct {
			ID                        string `json:"id"`
			TaskID                    string `json:"task_id"`
			Type, Description, Status string
		} `json:"tasks"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return nil, false
	}
	if event.Subtype == "background_tasks_changed" {
		out := []agent.TaskInfo{}
		for _, t := range event.Tasks {
			if t.Status == "running" || t.Status == "" {
				out = append(out, agent.TaskInfo{ID: agent.FirstNonEmpty(t.TaskID, t.ID), Kind: t.Type, Title: t.Description, Status: "running"})
			}
		}
		return out, false
	}
	if event.Subtype != "task_started" && event.Subtype != "task_progress" && event.Subtype != "task_notification" {
		return nil, false
	}
	if event.Background != nil && !*event.Background {
		return nil, false
	}
	status := "running"
	if event.Subtype == "task_notification" {
		status = event.Status
		switch status {
		case "completed", "failed", "cancelled":
		case "stopped", "canceled":
			status = "cancelled"
		default:
			status = "incomplete"
		}
	}
	return []agent.TaskInfo{{ID: event.ID, ParentID: event.ParentID, Kind: event.Kind, Title: event.Description, Status: status, Output: event.Summary}}, event.Subtype == "task_started"
}
