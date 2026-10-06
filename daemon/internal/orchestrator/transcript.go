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
	// Input receipts are persisted before their stream event. Read them after
	// the snapshot cursor so reconnect cannot skip an acknowledgement while
	// keeping that same message queued. Preserve the original lifecycle state:
	// a terminal save may have happened before its final stream event.
	if latest, exists := s.store.GetRun(id); exists {
		run.Inputs = latest.Inputs
	}
	if !retained || len(snapshot.Parts) == 0 && run.Status != "running" {
		// A restart or buffer expiry does not erase a stopped/finished turn.
		messages := s.store.Messages(run.ChatID)
		for _, message := range messages {
			if run.ReplyID != "" && message.ID == run.ReplyID {
				snapshot.Parts = message.Parts
				if len(snapshot.Parts) == 0 && message.Text != "" {
					snapshot.Parts = []agent.Part{{Type: "text", Text: message.Text}}
				}
				break
			}
		}
		if len(run.MessageIDs) > 0 {
			byID := make(map[string]agent.Message, len(messages))
			for _, message := range messages {
				byID[message.ID] = message
			}
			snapshot.Parts = nil
			for _, id := range run.MessageIDs {
				message, ok := byID[id]
				if !ok {
					continue
				}
				if message.Role == "user" {
					input := agent.UserInput{ID: strings.TrimPrefix(message.ID, "input-"), Text: message.Text, Attachments: message.Attachments, CreatedAt: message.CreatedAt, Status: "accepted"}
					snapshot.Parts = append(snapshot.Parts, agent.Part{Type: "user", ID: input.ID, Input: &input})
				} else {
					snapshot.Parts = append(snapshot.Parts, message.Parts...)
				}
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
	var messages []session.Message
	var segment []agent.Part
	flush := func(final bool) {
		if len(segment) == 0 && (!final || len(messages) > 0) {
			return
		}
		body := text
		if len(messages) > 0 || !final {
			var chunks []string
			for _, part := range segment {
				if part.Type == "text" {
					chunks = append(chunks, part.Text)
				}
			}
			body = strings.Join(chunks, "\n\n")
		}
		at := run.EndedAt
		if len(segment) > 0 && segment[0].At != "" {
			at = segment[0].At
		}
		messages = append(messages, session.Message{ID: newID(), ChatID: run.ChatID, Role: "assistant", Text: body, Parts: segment, CreatedAt: at})
		segment = nil
	}
	for _, part := range parts {
		if part.Type == "user" && part.Input != nil {
			flush(false)
			input := part.Input
			messages = append(messages, session.Message{ID: "input-" + input.ID, ChatID: run.ChatID, Role: "user", Text: input.Text,
				Attachments: input.Attachments, CreatedAt: input.CreatedAt})
		} else {
			segment = append(segment, part)
		}
	}
	flush(true)
	if s.store.SaveTurnMessages(messages) == nil {
		for _, message := range messages {
			if message.Role == "assistant" {
				run.ReplyID = message.ID
			}
			if len(messages) > 1 {
				run.MessageIDs = append(run.MessageIDs, message.ID)
			}
		}
	}
}
