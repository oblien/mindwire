package codex

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Notifications that explain tool activity or a problem belong in the transcript.
// Status telemetry alone disappears when a client reloads or reconnects. All these
// notices use the existing passive interaction shape; none grants approval.
func (st *streamState) emitNotice(method string, raw json.RawMessage, emit agent.Emit) {
	var p struct {
		Message, Summary, Details, Path      string
		ThreadID, TurnID, ItemID, ProcessID  string
		Stdin                                string
		Name, Status, Error, FailureReason   string
		FromModel, ToModel, Reason, Provider string
		Verifications                        []string
		ReviewID, TargetItemID               string
		Review                               struct{ Status, Rationale string }
		Run                                  struct {
			ID, EventName, Status, StatusMessage, SourcePath string
			Entries                                          []struct{ Kind, Text string }
		}
	}
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	inter := &agent.Interaction{Kind: "info", Meta: map[string]any{"source": "codex", "method": method}}
	switch method {
	case "warning", "guardianWarning", "configWarning", "deprecationNotice":
		inter.Kind, inter.Title = "warning", "Codex warning"
		if method == "configWarning" {
			inter.Title = "Configuration warning"
		}
		if method == "deprecationNotice" {
			inter.Title = "Compatibility notice"
		}
		inter.Detail = joinDetails(agent.FirstNonEmpty(p.Message, p.Summary), p.Details, p.Path)
	case "hook/started", "hook/completed":
		run := p.Run
		eventName := map[string]string{
			"preToolUse": "Before tool use", "postToolUse": "After tool use", "permissionRequest": "Permission request",
			"preCompact": "Before compaction", "postCompact": "After compaction", "sessionStart": "Session start", "sessionEnd": "Session end",
			"userPromptSubmit": "Message submission", "subagentStart": "Agent start", "subagentStop": "Agent stop", "stop": "Stop", "interrupt": "Interrupt",
		}[run.EventName]
		inter.ID, inter.Title = "codex:hook:"+run.ID, "Hook: "+agent.FirstNonEmpty(eventName, run.EventName)
		inter.Meta["status"], inter.Meta["path"] = run.Status, run.SourcePath
		status := map[string]string{"running": "Running", "completed": "Completed", "failed": "Failed", "blocked": "Blocked", "stopped": "Stopped"}[run.Status]
		inter.Detail = agent.FirstNonEmpty(run.StatusMessage, status, run.Status)
		if run.Status == "failed" || run.Status == "blocked" {
			inter.Kind = "error"
		}
		for _, entry := range run.Entries {
			// Hook context is injected model input, not user-facing hook feedback.
			if entry.Kind == "context" {
				continue
			}
			inter.Detail = joinDetails(inter.Detail, entry.Text)
			if entry.Kind == "error" || entry.Kind == "stop" {
				inter.Kind = "error"
			}
			if entry.Kind == "warning" && inter.Kind == "info" {
				inter.Kind = "warning"
			}
		}
	case "mcpServer/startupStatus/updated":
		inter.ID = "codex:mcp-startup:" + p.Name
		if p.Status != "failed" {
			// Healthy startup is telemetry. Only update a notice if this server had failed.
			if _, shown := st.interactionSnapshots[inter.ID]; !shown {
				return
			}
		}
		status := map[string]string{"starting": "Connecting", "ready": "Connected", "failed": "Connection failed", "cancelled": "Connection cancelled"}[p.Status]
		inter.Title, inter.Detail = "MCP server: "+p.Name, agent.FirstNonEmpty(p.Error, status, p.Status)
		inter.Meta["status"] = p.Status
		if p.Status == "failed" {
			inter.Kind = "error"
		}
		if p.FailureReason == "reauthenticationRequired" {
			inter.Detail = joinDetails(inter.Detail, "Sign in to this MCP server again.")
		}
	case "item/commandExecution/terminalInteraction":
		if p.Stdin == "" {
			return
		} // polling a PTY does not add transcript content
		st.terminalInputs++
		inter.ID = fmt.Sprintf("%s:stdin:%d", p.ItemID, st.terminalInputs)
		inter.Title, inter.Detail = "Terminal input", p.Stdin
		inter.Meta["itemId"], inter.Meta["processId"] = p.ItemID, p.ProcessID
	case "model/rerouted":
		inter.Title = "Model changed"
		inter.Detail = fmt.Sprintf("%s → %s", p.FromModel, p.ToModel)
		inter.Meta["reason"] = p.Reason
	case "model/verification":
		inter.Kind, inter.Title = "warning", "Account verification required"
		inter.Detail = "Complete the required account verification to continue with this model."
		inter.Meta["verifications"] = p.Verifications
	case "modelProvider/authRecoveryStarted", "modelProvider/authRecoveryCompleted":
		inter.ID = "codex:auth-recovery:" + p.TurnID + ":" + p.Provider
		inter.Title, inter.Detail = "Connection: "+p.Provider, p.Message
	case "item/autoApprovalReview/started", "item/autoApprovalReview/completed":
		inter.ID = "codex:approval-review:" + p.ReviewID
		status := map[string]string{"inProgress": "Reviewing the requested action.", "approved": "The action was approved.", "denied": "The action was not approved.", "timedOut": "The review timed out.", "aborted": "The review was stopped."}[p.Review.Status]
		inter.Title, inter.Detail = "Automatic approval review", joinDetails(agent.FirstNonEmpty(status, p.Review.Status), p.Review.Rationale)
		inter.Meta["status"], inter.Meta["itemId"] = p.Review.Status, p.TargetItemID
		if p.Review.Status == "denied" || p.Review.Status == "timedOut" || p.Review.Status == "aborted" {
			inter.Kind = "warning"
		}
	case "autoApprovalReview/strictReviewRequired":
		inter.Title, inter.Detail = "Automatic approval review", "Additional review is required."
	default:
		return
	}
	if strings.TrimSpace(inter.Detail) == "" {
		return
	}
	if inter.ID == "" {
		// Stable within a turn, including repeated warning notifications.
		sum := sha256.Sum256([]byte(method + "\x00" + inter.Detail))
		inter.ID = fmt.Sprintf("codex:notice:%x", sum[:12])
	}
	st.emitInteraction(inter, emit)
}

func joinDetails(parts ...string) string {
	var result string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !strings.Contains(result, part) {
			if result != "" {
				result += "\n"
			}
			result += part
		}
	}
	return result
}
