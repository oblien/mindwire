package orchestrator

import (
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// RunSnapshot is shared by the HTTP API and embedded SDK. Sequence belongs to Parts;
// subscribing after that cursor cannot replay or skip any of the materialized output.
type RunSnapshot struct {
	Run session.Run `json:"run"`
	stream.Snapshot
}

// SubscribeRun keeps the producer's lifetime authoritative for stream completion.
// A terminal record can be saved before its final events/notification are sent;
// subscribers must drain through hub.Close, never close that topic themselves.
// The start/teardown lock also prevents a new run from being mistaken for an old
// run whose replay buffer expired or disappeared after a daemon restart.
func (s *Supervisor) SubscribeRun(id string, after int64) (replay []agent.Event, ch <-chan agent.Event, done bool, cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, active := s.cancels[id]; active {
		return s.hub.SubscribeAfter(id, after)
	}
	return s.hub.SubscribeRetainedAfter(id, after)
}

func (s *Supervisor) Snapshot(id string) (RunSnapshot, bool) {
	run, ok := s.store.GetRun(id)
	if !ok {
		return RunSnapshot{}, false
	}
	snapshot, retained := s.hub.Snapshot(id)
	if !retained || len(snapshot.Parts) == 0 && run.Status != "running" {
		// A restart or buffer expiry does not erase a stopped/finished turn.
		for _, message := range s.store.Messages(run.ChatID) {
			if run.ReplyID != "" && message.ID == run.ReplyID {
				snapshot.Parts = message.Parts
				if len(snapshot.Parts) == 0 && message.Text != "" {
					snapshot.Parts = []agent.Part{{Type: "text", Text: message.Text}}
				}
				break
			}
		}
	}
	if snapshot.Parts == nil {
		snapshot.Parts = []agent.Part{}
	}
	snapshot.Parts = s.store.OverlayInteractions(run.ChatID, snapshot.Parts)
	return RunSnapshot{Run: run, Snapshot: snapshot}, true
}

// Persist the assistant's components even when the turn fails after producing output.
// A run error alone is not a chat transcript: it otherwise disappears on the next reload.
func (s *Supervisor) saveReply(run *session.Run, text string, parts []agent.Part, failure string) {
	parts = s.store.OverlayInteractions(run.ChatID, parts)
	if text == "" || failure != "" {
		var partial []string
		for _, part := range parts {
			if part.Type == "text" && part.Text != "" {
				partial = append(partial, part.Text)
			}
		}
		text = strings.Join(partial, "\n\n")
	}
	if failure != "" {
		found := false
		for _, part := range parts {
			found = found || part.Interaction != nil && part.Interaction.Kind == "error" && part.Interaction.Detail == failure
		}
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
