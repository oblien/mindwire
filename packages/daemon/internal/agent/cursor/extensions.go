package cursor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

type questions struct {
	Title     string `json:"title"`
	Questions []struct {
		ID            string                       `json:"id"`
		Prompt        string                       `json:"prompt"`
		Options       []struct{ ID, Label string } `json:"options"`
		AllowMultiple bool                         `json:"allowMultiple"`
	} `json:"questions"`
}

func interaction(m acp.Message) *agent.Interaction {
	id := string(m.ID)
	switch strings.TrimPrefix(m.Method, "_") {
	case "cursor/ask_question":
		var q questions
		if json.Unmarshal(m.Params, &q) != nil || len(q.Questions) == 0 {
			return nil
		}
		it := &agent.Interaction{ID: id, Kind: "form", Title: agent.FirstNonEmpty(q.Title, "Cursor has a question"), NeedsResponse: true}
		for _, question := range q.Questions {
			item := agent.Question{ID: question.ID, Title: question.Prompt, MultiSelect: question.AllowMultiple}
			for _, o := range question.Options {
				item.Options = append(item.Options, agent.Action{ID: o.ID, Label: o.Label})
			}
			it.Questions = append(it.Questions, item)
		}
		return it
	case "cursor/create_plan":
		var plan struct{ Name, Overview, Plan string }
		if json.Unmarshal(m.Params, &plan) != nil {
			return nil
		}
		return &agent.Interaction{ID: id, Kind: "plan", Title: agent.FirstNonEmpty(plan.Name, "Review Cursor's plan"), Detail: strings.TrimSpace(plan.Overview + "\n\n" + plan.Plan), NeedsResponse: true, Options: []agent.Action{{ID: "approve", Label: "Accept plan"}, {ID: "reject", Label: "Request changes"}}}
	}
	return nil
}

func reply(m acp.Message, in agent.Inbound) (any, error) {
	switch strings.TrimPrefix(m.Method, "_") {
	case "cursor/ask_question":
		if in.Decision == "deny" || in.Decision == "reject" {
			return map[string]any{"outcome": map[string]string{"outcome": "skipped", "reason": in.Text}}, nil
		}
		var q questions
		if err := json.Unmarshal(m.Params, &q); err != nil {
			return nil, err
		}
		answers := []map[string]any{}
		for _, question := range q.Questions {
			answer := in.Answers[question.ID]
			if len(q.Questions) == 1 && len(answer.Options) == 0 {
				answer.Options = in.Options
				if len(answer.Options) == 0 && in.Decision != "" {
					answer.Options = []string{in.Decision}
				}
			}
			if answer.Text != "" {
				return nil, fmt.Errorf("Cursor question %s accepts option IDs only", question.ID)
			}
			if !question.AllowMultiple && len(answer.Options) > 1 {
				return nil, fmt.Errorf("Cursor question %s accepts one answer", question.ID)
			}
			for _, id := range answer.Options {
				valid := false
				for _, o := range question.Options {
					valid = valid || id == o.ID
				}
				if !valid {
					return nil, fmt.Errorf("unknown Cursor answer option")
				}
			}
			answers = append(answers, map[string]any{"questionId": question.ID, "selectedOptionIds": append([]string{}, answer.Options...)})
		}
		return map[string]any{"outcome": map[string]any{"outcome": "answered", "answers": answers}}, nil
	case "cursor/create_plan":
		if in.Decision == "approve" || in.Decision == "allow" {
			return map[string]any{"outcome": map[string]string{"outcome": "accepted"}}, nil
		}
		return map[string]any{"outcome": map[string]string{"outcome": "rejected", "reason": in.Text}}, nil
	}
	return nil, fmt.Errorf("unsupported Cursor interaction")
}

func update(m acp.Message, s *acp.Stream) {
	switch strings.TrimPrefix(m.Method, "_") {
	case "cursor/update_todos":
		var payload struct {
			Todos []struct{ ID, Content, Status string }
		}
		if json.Unmarshal(m.Params, &payload) != nil {
			return
		}
		it := &agent.Interaction{ID: "cursor-todos", Kind: "todos", Title: "Plan"}
		for _, t := range payload.Todos {
			it.Items = append(it.Items, agent.TodoItem{Content: t.Content, Status: t.Status})
		}
		s.Emit(agent.Event{Type: agent.EventInteraction, SessionID: s.SessionID, Interaction: it})
	case "cursor/task", "cursor/generate_image":
		// Preserve the extension payload without confusing a subagent/artifact
		// update with completion of the foreground prompt.
		s.Emit(agent.Event{Type: agent.EventStatus, SessionID: s.SessionID, Meta: map[string]any{"nativeMethod": m.Method, "nativeUpdate": m.Params}})
	}
}
