package session

import (
	"errors"
	"reflect"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// MessageQuestion is durable daemon bookkeeping for a native async question.
// Native control requests stay in the live supervisor and expire with their run.
type MessageQuestion struct {
	ChatID        string            `json:"chatId"`
	RunID         string            `json:"runId"`
	Interaction   agent.Interaction `json:"interaction"`
	ResponseRunID string            `json:"responseRunId,omitempty"`
}

type InteractionReply struct {
	RunID    string
	Response agent.InteractionResponse
}

func interactionKey(runID, id string) string { return runID + "\x1f" + id }

// RecordMessageQuestion runs before publishing the question. Replayed native
// items cannot reopen an answered question. Save failures never claim persistence.
func (st *Store) RecordMessageQuestion(chatID, runID string, it agent.Interaction) (agent.Interaction, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	it.RunID = runID
	key := interactionKey(runID, it.ID)
	if old, ok := st.s.Interactions[key]; ok {
		if old.Interaction.Response != nil || reflect.DeepEqual(old.Interaction, it) {
			return old.Interaction, nil
		}
	}
	if st.s.Interactions == nil {
		st.s.Interactions = map[string]MessageQuestion{}
	}
	old, exists := st.s.Interactions[key]
	st.s.Interactions[key] = MessageQuestion{ChatID: chatID, RunID: runID, Interaction: it}
	if err := st.save(); err != nil {
		if exists {
			st.s.Interactions[key] = old
		} else {
			delete(st.s.Interactions, key)
		}
		return it, err
	}
	return it, nil
}

func (st *Store) MessageQuestion(runID, id string) (MessageQuestion, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	q, ok := st.s.Interactions[interactionKey(runID, id)]
	return q, ok
}

func (st *Store) HasPendingMessageQuestion(runID string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, q := range st.s.Interactions {
		if q.RunID == runID && q.Interaction.NeedsResponse {
			return true
		}
	}
	return false
}

// CommitInteractionReply atomically records the answer and its user message. A
// resumed turn also writes its Run in this transaction, before execution starts.
// The reply is never marked answered merely because the user tapped Send.
func (st *Store) CommitInteractionReply(ref InteractionReply, responseRunID string, message Message, run *Run) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	key := interactionKey(ref.RunID, ref.Response.InteractionID)
	old, ok := st.s.Interactions[key]
	if !ok || old.Interaction.Response != nil {
		return errors.New("question is no longer waiting for an answer")
	}
	q := old
	q.Interaction.NeedsResponse = false
	q.Interaction.Response = &ref.Response
	q.ResponseRunID = responseRunID
	st.s.Interactions[key] = q
	nm, nr := len(st.s.Messages), len(st.s.Runs)
	st.s.Messages = append(st.s.Messages, message)
	if run != nil {
		st.s.Runs = append(st.s.Runs, *run)
	}
	if err := st.save(); err != nil {
		st.s.Interactions[key] = old
		st.s.Messages, st.s.Runs = st.s.Messages[:nm], st.s.Runs[:nr]
		return err
	}
	return nil
}

// OverlayInteractions applies the same daemon-owned answer state to native
// history, recorded history and run snapshots. Forked/imported native questions
// without a record in this chat are read-only; they cannot answer another chat.
func (st *Store) OverlayInteractions(chatID string, parts []agent.Part) []agent.Part {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.overlayInteractions(chatID, parts)
}

func (st *Store) overlayInteractions(chatID string, parts []agent.Part) []agent.Part {
	if parts == nil {
		return nil
	}
	out := make([]agent.Part, len(parts))
	copy(out, parts)
	for i, p := range out {
		if p.Interaction == nil || !p.Interaction.IsMessageQuestion() {
			continue
		}
		it := *p.Interaction
		st.recoverLegacyQuestion(chatID, it)
		it.NeedsResponse = false
		it.RunID = ""
		for _, q := range st.s.Interactions {
			if q.ChatID == chatID && q.Interaction.ID == it.ID && (p.Interaction.RunID == "" || p.Interaction.RunID == q.RunID) {
				it = q.Interaction
				break
			}
		}
		out[i].Interaction = &it
	}
	return out
}

// Recover only the last daemon-recorded async question, using its newly decoded
// native form. Earlier conversations may already have answered via plain text;
// imported/forked native prompts have no matching daemon reply and remain read-only.
func (st *Store) recoverLegacyQuestion(chatID string, it agent.Interaction) {
	if it.RunID == "" {
		return
	}
	key := interactionKey(it.RunID, it.ID)
	if _, exists := st.s.Interactions[key]; exists {
		return
	}
	var latest Run
	for i := len(st.s.Runs) - 1; i >= 0; i-- {
		if st.s.Runs[i].ChatID == chatID && st.s.Runs[i].ParentID == "" {
			latest = st.s.Runs[i]
			break
		}
	}
	if latest.ID != it.RunID || latest.Status == "running" || latest.ReplyID == "" {
		return
	}
	for i, message := range st.s.Messages {
		if message.ChatID != chatID || message.ID != latest.ReplyID {
			continue
		}
		matched := false
		for _, part := range message.Parts {
			matched = matched || agent.LegacyAsyncFormID(part.Interaction) == it.ID
		}
		if !matched {
			return
		}
		it.NeedsResponse = true
		it.Response = nil
		if st.s.Interactions == nil {
			st.s.Interactions = map[string]MessageQuestion{}
		}
		st.s.Interactions[key] = MessageQuestion{ChatID: chatID, RunID: it.RunID, Interaction: it}
		updated := message
		updated.Parts = agent.UpgradeAsyncQuestionParts(message.Parts, map[string]agent.Interaction{it.ID: it})
		st.s.Messages[i] = updated
		if err := st.save(); err != nil {
			delete(st.s.Interactions, key)
			st.s.Messages[i] = message
		}
		return
	}
}
