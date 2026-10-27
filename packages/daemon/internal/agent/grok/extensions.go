package grok

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

type grokQuestions struct {
	Questions []struct {
		ID, Question string
		MultiSelect  bool
		Options      []struct{ ID, Label, Description string }
	}
}

func extensionParams(raw json.RawMessage) json.RawMessage {
	var wrapped struct{ Params json.RawMessage }
	if json.Unmarshal(raw, &wrapped) == nil && len(wrapped.Params) > 0 {
		return wrapped.Params
	}
	return raw
}

func grokInteraction(req acp.Message) *agent.Interaction {
	id := string(bytes.TrimSpace(req.ID))
	switch strings.TrimPrefix(req.Method, "_") {
	case "x.ai/ask_user_question":
		var native grokQuestions
		if json.Unmarshal(extensionParams(req.Params), &native) != nil {
			return nil
		}
		it := &agent.Interaction{ID: id, Kind: "form", Title: "Grok Build has a question", NeedsResponse: true}
		for _, q := range native.Questions {
			question := agent.Question{ID: agent.FirstNonEmpty(q.ID, q.Question), Title: q.Question, MultiSelect: q.MultiSelect, AllowOther: true}
			for _, o := range q.Options {
				question.Options = append(question.Options, agent.Action{ID: agent.FirstNonEmpty(o.ID, o.Label), Label: o.Label, Description: o.Description})
			}
			it.Questions = append(it.Questions, question)
		}
		return it
	case "x.ai/exit_plan_mode":
		var p struct{ PlanContent string }
		if json.Unmarshal(extensionParams(req.Params), &p) != nil {
			return nil
		}
		return &agent.Interaction{ID: id, Kind: "plan", Title: "Review Grok Build's plan", Detail: p.PlanContent, NeedsResponse: true, Options: []agent.Action{{ID: "approve", Label: "Approve"}, {ID: "reject", Label: "Request changes"}}}
	}
	return nil
}

func grokReply(req acp.Message, in agent.Inbound) (any, error) {
	switch strings.TrimPrefix(req.Method, "_") {
	case "x.ai/ask_user_question":
		if agent.Denied(in.Decision) {
			return map[string]string{"outcome": "cancelled"}, nil
		}
		var native grokQuestions
		if err := json.Unmarshal(extensionParams(req.Params), &native); err != nil {
			return nil, err
		}
		answers := map[string][]string{}
		annotations := map[string]any{}
		for _, q := range native.Questions {
			a := in.Answers[agent.FirstNonEmpty(q.ID, q.Question)]
			labels := []string{}
			for _, selected := range a.Options {
				found := false
				for _, option := range q.Options {
					if selected == agent.FirstNonEmpty(option.ID, option.Label) {
						labels = append(labels, option.Label)
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("invalid Grok Build question option")
				}
			}
			if a.Text != "" {
				annotations[q.Question] = map[string]string{"notes": a.Text}
				if len(labels) == 0 {
					labels = append(labels, "Other")
				}
			}
			answers[q.Question] = labels
		}
		return map[string]any{"outcome": "accepted", "answers": answers, "annotations": annotations}, nil
	case "x.ai/exit_plan_mode":
		outcome := "approved"
		if agent.Denied(in.Decision) {
			outcome = "request_changes"
		}
		return map[string]string{"outcome": outcome, "feedback": in.Text}, nil
	}
	return nil, fmt.Errorf("unsupported Grok Build request %s", req.Method)
}

func grokPromptID(raw json.RawMessage) string {
	var response struct {
		StopReason string
		Meta       struct{ PromptID, RequestID string } `json:"_meta"`
	}
	if json.Unmarshal(raw, &response) != nil || response.StopReason != "" {
		return ""
	}
	return agent.FirstNonEmpty(response.Meta.PromptID, response.Meta.RequestID)
}

func grokCompletion(m acp.Message) (string, json.RawMessage) {
	method := strings.TrimPrefix(m.Method, "_")
	direct := method == "x.ai/session/prompt_complete" || method == "x.ai/prompt_complete"
	if method != "x.ai/session_notification" && method != "x.ai/session/update" && !direct {
		return "", nil
	}
	var p struct {
		SessionID, PromptID, StopReason string
		Update                          struct {
			SessionUpdate, PromptID, StopReason string
			PromptIDSnake                       string `json:"prompt_id"`
			StopReasonSnake                     string `json:"stop_reason"`
		}
	}
	if json.Unmarshal(m.Params, &p) != nil {
		return "", nil
	}
	id, reason := p.PromptID, p.StopReason
	if p.Update.SessionUpdate == "turn_completed" {
		id = agent.FirstNonEmpty(p.Update.PromptID, p.Update.PromptIDSnake)
		reason = agent.FirstNonEmpty(p.Update.StopReason, p.Update.StopReasonSnake)
	} else if !direct {
		return "", nil
	}
	if id == "" || strings.HasPrefix(id, "task-completed-") {
		return "", nil
	}
	if reason == "" {
		reason = "end_turn"
	}
	raw, _ := json.Marshal(map[string]string{"stopReason": reason})
	return id, raw
}
