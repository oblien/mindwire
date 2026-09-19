package codex

import (
	"fmt"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Async input has no native request ID. Both app-server items and native rollout
// tool calls use this same form; the shared daemon delivers its answer as a message.
func asyncQuestionInteraction(id string, native []asyncQuestion) *agent.Interaction {
	var questions []agent.Question
	for i, question := range native {
		if strings.TrimSpace(question.Title) == "" {
			continue
		}
		q := agent.Question{ID: fmt.Sprintf("question-%d", i+1), Title: question.Title, AllowOther: true}
		for _, label := range question.Options {
			q.Options = append(q.Options, agent.Action{ID: label, Label: label})
		}
		questions = append(questions, q)
	}
	if len(questions) == 0 {
		return nil
	}
	title := "Your input"
	if len(questions) == 1 {
		title = questions[0].Title
	}
	blocking := false
	return &agent.Interaction{ID: id + ":questions", Kind: "form", Title: title, Questions: questions,
		Blocking: &blocking, NeedsResponse: true, ResponseMode: "message",
		Meta: map[string]any{"source": "codex", "asyncQuestion": true}}
}
