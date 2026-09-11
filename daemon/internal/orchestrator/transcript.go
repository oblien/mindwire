package orchestrator

import (
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
)

// Persist the assistant's components even when the turn fails after producing output.
// A run error alone is not a chat transcript: it otherwise disappears on the next reload.
func (s *Supervisor) saveReply(run *session.Run, text string, parts []agent.Part, failure string) {
	parts = append([]agent.Part(nil), parts...)
	if failure != "" {
		var partial []string
		found := false
		for _, part := range parts {
			if part.Type == "text" && part.Text != "" {
				partial = append(partial, part.Text)
			}
			found = found || part.Interaction != nil && part.Interaction.Kind == "error" && part.Interaction.Detail == failure
		}
		text = strings.Join(partial, "\n\n")
		if !found {
			parts = append(parts, agent.Part{Type: "interaction", At: run.EndedAt,
				Interaction: &agent.Interaction{ID: "run-" + run.ID + "-error", Kind: "error", Title: "Turn failed", Detail: failure,
					Meta: map[string]any{"source": run.Agent}}})
		}
	}
	reply := session.Message{ID: newID(), ChatID: run.ChatID, Role: "assistant", Text: text, Parts: parts, CreatedAt: run.EndedAt}
	if s.store.AddMessage(reply) == nil {
		run.ReplyID = reply.ID
	}
}
