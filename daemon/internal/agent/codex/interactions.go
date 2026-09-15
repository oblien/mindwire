package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// --- server-request → interaction --------------------------------------------------------------

// serverRequestInteraction maps a server-request (an approval or a user-input ask) to a unified
// interaction plus the correlator needed to answer it. The interaction id is the raw JSON-RPC id (a
// string or number) rendered as text; the approval carries that raw id back for the reply. Unknown
// requests return nil and the reader sends an explicit unsupported-method RPC error.
func serverRequestInteraction(msg rpcIn) (*agent.Interaction, approval) {
	id := string(msg.ID)
	allowDeny := []agent.Action{{ID: "allow", Label: "Approve"}, {ID: "deny", Label: "Reject"}}

	switch msg.Method {
	case "execCommandApproval", "applyPatchApproval":
		var p struct {
			Command string   `json:"command"`
			Cwd     string   `json:"cwd"`
			Reason  string   `json:"reason"`
			Parsed  []string `json:"parsedCmd"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		title := "Approve command?"
		if msg.Method == "applyPatchApproval" {
			title = "Apply file changes?"
		} else if p.Command != "" {
			title = "Run: " + p.Command
		}
		return &agent.Interaction{
			ID: id, Kind: "approval", Title: title, Detail: p.Reason,
			Options: allowDeny, NeedsResponse: true, Feedback: "rejection",
			Meta: map[string]any{"method": msg.Method},
		}, approval{rawID: msg.ID, family: famReviewDecision}

	case "item/commandExecution/requestApproval":
		return commandApproval(msg)

	case "item/fileChange/requestApproval":
		var p struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		inter := &agent.Interaction{
			ID: id, Kind: "approval", Title: "Apply file changes?", Detail: p.Reason,
			NeedsResponse: true, Feedback: "always",
			Meta: map[string]any{"method": msg.Method},
		}
		ap := approval{rawID: msg.ID, family: famV2Decision}
		setDecisions(inter, &ap, []any{"accept", "acceptForSession", "decline", "cancel"})
		return inter, ap
	case "item/permissions/requestApproval":
		var p struct {
			Reason      string          `json:"reason"`
			Permissions json.RawMessage `json:"permissions"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		return &agent.Interaction{ID: id, Kind: "approval", Title: "Allow additional access?", Detail: accessDetail(p.Reason, p.Permissions),
				Options: []agent.Action{{ID: "allow", Label: "Allow for this turn"}, {ID: "allow_session", Label: "Allow for this session"}, {ID: "deny", Label: "Reject"}}, NeedsResponse: true, Feedback: "always", Meta: map[string]any{"method": msg.Method, "permissions": p.Permissions}},
			approval{rawID: msg.ID, family: famPermissions, permissions: p.Permissions}

	case "item/tool/requestUserInput":
		prompts := questionPrompts(msg)
		if len(prompts) > 0 {
			return prompts[0].interaction, prompts[0]
		}
		return nil, approval{}
	case "mcpServer/elicitation/request":
		var p struct {
			Mode    string `json:"mode"`
			Message string `json:"message"`
			URL     string `json:"url"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if p.Mode != "url" {
			return nil, approval{}
		}
		return &agent.Interaction{ID: id, Kind: "approval", Title: p.Message, Detail: p.URL,
			Options: allowDeny, NeedsResponse: true}, approval{rawID: msg.ID, family: famElicitation}

	default:
		return nil, approval{}
	}
}

func serverRequestPrompts(msg rpcIn) []approval {
	if msg.Method == "item/tool/requestUserInput" {
		return questionPrompts(msg)
	}
	inter, ap := serverRequestInteraction(msg)
	if inter == nil {
		return nil
	}
	ap.interaction = inter
	return []approval{ap}
}

func questionPrompts(msg rpcIn) []approval {
	var p struct {
		IsBlocking *bool `json:"isBlocking"`
		Questions  []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			IsOther  bool   `json:"isOther"`
			IsSecret bool   `json:"isSecret"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(msg.Params, &p) != nil {
		return nil
	}
	inter := &agent.Interaction{ID: string(msg.ID), Kind: "form", Title: "Your input", NeedsResponse: true,
		Blocking: p.IsBlocking, Meta: map[string]any{"method": msg.Method}}
	for _, q := range p.Questions {
		question := agent.Question{ID: q.ID, Title: q.Question, Header: q.Header, AllowOther: q.IsOther, IsSecret: q.IsSecret}
		for _, option := range q.Options {
			question.Options = append(question.Options, agent.Action{ID: option.Label, Label: option.Label, Description: option.Description})
		}
		inter.Questions = append(inter.Questions, question)
	}
	if len(inter.Questions) == 0 {
		return nil
	}
	// Mirror a single question for older clients; grouped forms require the answers map.
	if len(inter.Questions) == 1 {
		q := inter.Questions[0]
		inter.Title, inter.Detail, inter.Options = q.Title, q.Header, q.Options
		inter.Kind = "input"
		if len(q.Options) > 0 {
			inter.Kind = "choice"
		}
		inter.Meta["allowOther"], inter.Meta["isSecret"] = q.AllowOther, q.IsSecret
	}
	return []approval{{rawID: msg.ID, family: famAnswers, interaction: inter, questionID: inter.Questions[0].ID}}
}

func answerValues(in agent.Inbound) []string {
	values := append([]string{}, in.Options...)
	if len(values) == 0 && in.Decision != "" {
		values = append(values, in.Decision)
	}
	if strings.TrimSpace(in.Text) != "" {
		values = append(values, in.Text)
	}
	return values
}

// decisionResult encodes an approval answer into the native decision shape for its family.
func decisionResult(ap approval, in agent.Inbound) any {
	if decision, ok := ap.decisions[in.Decision]; ok {
		return map[string]any{"decision": decision}
	}
	// The supervisor validates against offered actions; direct adapter callers must
	// also fail closed instead of interpreting a typo as an approval.
	deny := in.Decision != "allow" && in.Decision != "approve" && in.Decision != "allow_session"
	if ap.decisions != nil {
		return map[string]any{"decision": "cancel"}
	}
	switch ap.family {
	case famReviewDecision:
		if in.Decision == "cancel" {
			return map[string]any{"decision": "abort"}
		}
		if in.Decision == "allow_session" {
			return map[string]any{"decision": "approved_for_session"}
		}
		if deny {
			reason := strings.TrimSpace(in.Text)
			if reason == "" {
				reason = "Denied by user"
			}
			return map[string]any{"decision": map[string]any{"denied": map[string]any{"rejection": reason}}}
		}
		return map[string]any{"decision": "approved"}
	case famAnswers:
		answers := map[string]any{}
		for id, answer := range in.Answers {
			answers[id] = map[string]any{"answers": answerValues(agent.Inbound{Options: answer.Options, Text: answer.Text})}
		}
		if len(answers) == 0 && ap.questionID != "" {
			answers[ap.questionID] = map[string]any{"answers": answerValues(in)}
		}
		return map[string]any{"answers": answers}
	case famPermissions:
		permissions := ap.permissions
		if deny || len(permissions) == 0 || string(permissions) == "null" {
			permissions = json.RawMessage(`{}`)
		}
		scope := "turn"
		if in.Decision == "allow_session" {
			scope = "session"
		}
		return map[string]any{"permissions": permissions, "scope": scope}
	case famElicitation:
		if deny {
			return map[string]any{"action": "decline"}
		}
		return map[string]any{"action": "accept"}
	default: // famV2Decision
		if in.Decision == "cancel" {
			return map[string]any{"decision": "cancel"}
		}
		if in.Decision == "allow_session" {
			return map[string]any{"decision": "acceptForSession"}
		}
		if deny {
			return map[string]any{"decision": "decline"}
		}
		return map[string]any{"decision": "accept"}
	}
}

func commandApproval(msg rpcIn) (*agent.Interaction, approval) {
	var p struct {
		Command   string   `json:"command"`
		CWD       string   `json:"cwd"`
		Reason    string   `json:"reason"`
		Available []any    `json:"availableDecisions"`
		Amendment []string `json:"proposedExecpolicyAmendment"`
		Network   *struct {
			Host     string `json:"host"`
			Protocol string `json:"protocol"`
		} `json:"networkApprovalContext"`
	}
	if json.Unmarshal(msg.Params, &p) != nil {
		return nil, approval{}
	}
	title := "Approve command?"
	if p.Command != "" {
		title = "Run: " + p.Command
	}
	if p.Network != nil {
		title = "Allow network access to " + p.Network.Host + "?"
	}
	detail := p.Reason
	if p.CWD != "" {
		detail = strings.TrimSpace(detail + "\nDirectory: " + p.CWD)
	}
	inter := &agent.Interaction{ID: string(msg.ID), Kind: "approval", Title: title, Detail: detail,
		NeedsResponse: true, Feedback: "always", Meta: map[string]any{"method": msg.Method}}
	ap := approval{rawID: msg.ID, family: famV2Decision}
	if p.Available == nil {
		p.Available = []any{"accept", "acceptForSession", "decline", "cancel"}
		if len(p.Amendment) > 0 {
			p.Available = append(p.Available, map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": p.Amendment}})
		}
	}
	setDecisions(inter, &ap, p.Available)
	return inter, ap
}

func setDecisions(inter *agent.Interaction, ap *approval, decisions []any) {
	ap.decisions = map[string]any{}
	for i, value := range decisions {
		action := agent.Action{}
		switch native := value.(type) {
		case string:
			switch native {
			case "accept":
				action.ID, action.Label = "allow", "Allow once"
			case "acceptForSession":
				action.ID, action.Label = "allow_session", "Allow for this session"
			case "decline":
				action.ID, action.Label = "deny", "Reject"
			case "cancel":
				action.ID, action.Label = "cancel", "Reject and stop"
			}
		case map[string]any:
			if amendment, ok := native["acceptWithExecpolicyAmendment"].(map[string]any); ok {
				action.ID, action.Label = "allow_rule", "Always allow matching commands"
				if values, ok := amendment["execpolicy_amendment"].([]any); ok {
					var prefix []string
					for _, v := range values {
						prefix = append(prefix, fmt.Sprint(v))
					}
					action.Description = strings.Join(prefix, " ")
				} else if values, ok := amendment["execpolicy_amendment"].([]string); ok {
					action.Description = strings.Join(values, " ")
				}
			} else if amendment, ok := native["applyNetworkPolicyAmendment"].(map[string]any); ok {
				if policy, ok := amendment["network_policy_amendment"].(map[string]any); ok {
					action.ID = fmt.Sprintf("network_policy_%d", i)
					action.Label = "Always " + fmt.Sprint(policy["action"]) + " " + fmt.Sprint(policy["host"])
				}
			}
		}
		if action.ID != "" {
			inter.Options = append(inter.Options, action)
			ap.decisions[action.ID] = value
		}
	}
}

func accessDetail(reason string, permissions json.RawMessage) string {
	var profile struct {
		Network *struct {
			Enabled bool `json:"enabled"`
		} `json:"network"`
		FileSystem *struct {
			Read    []string `json:"read"`
			Write   []string `json:"write"`
			Entries []struct {
				Access string `json:"access"`
				Path   struct {
					Path    string `json:"path"`
					Pattern string `json:"pattern"`
				} `json:"path"`
			} `json:"entries"`
		} `json:"fileSystem"`
	}
	_ = json.Unmarshal(permissions, &profile)
	lines := []string{reason}
	if profile.Network != nil && profile.Network.Enabled {
		lines = append(lines, "Network access")
	}
	if fs := profile.FileSystem; fs != nil {
		for _, path := range fs.Read {
			lines = append(lines, "Read: "+path)
		}
		for _, path := range fs.Write {
			lines = append(lines, "Write: "+path)
		}
		for _, entry := range fs.Entries {
			lines = append(lines, entry.Access+": "+agent.FirstNonEmpty(entry.Path.Path, entry.Path.Pattern))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
