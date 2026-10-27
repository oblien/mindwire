package grok

import (
	"encoding/json"
	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"strings"
)

func grokTask(m acp.Message, session string) *agent.TaskInfo {
	method := strings.TrimPrefix(m.Method, "_")
	if method != "x.ai/task_backgrounded" && method != "x.ai/task_completed" {
		return nil
	}
	var p struct {
		SessionID string
		Update    struct {
			ID                   string `json:"task_id"`
			ToolID               string `json:"tool_call_id"`
			Command, Description string
			Snapshot             struct {
				ID                                 string `json:"task_id"`
				Output, Kind, Command, Description string
				ExitCode                           *int `json:"exit_code"`
			} `json:"task_snapshot"`
		}
	}
	if json.Unmarshal(m.Params, &p) != nil || p.SessionID != "" && p.SessionID != session {
		return nil
	}
	u := p.Update
	task := &agent.TaskInfo{ID: agent.FirstNonEmpty(u.Snapshot.ID, u.ID, u.ToolID), Kind: agent.FirstNonEmpty(u.Snapshot.Kind, "command"), Title: agent.FirstNonEmpty(u.Description, u.Snapshot.Description, u.Command, u.Snapshot.Command), Status: "running", Output: u.Snapshot.Output}
	if method == "x.ai/task_completed" {
		task.Status = "completed"
		if u.Snapshot.ExitCode != nil && *u.Snapshot.ExitCode != 0 {
			task.Status = "failed"
		}
	}
	return task
}
