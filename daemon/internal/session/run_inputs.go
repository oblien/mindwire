package session

import (
	"errors"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// SaveRunInput reserves the retry receipt before native delivery. A lost HTTP
// response, client reconnect, or daemon restart cannot submit it a second time.
func (st *Store) SaveRunInput(runID string, input agent.UserInput, digest string) (Run, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, exists := st.s.TurnRequests[input.ID]; exists {
		return Run{}, ErrTurnRequestConflict
	}
	for i, run := range st.s.Runs {
		if run.ID != runID || run.Status != "running" {
			continue
		}
		if st.s.TurnRequests == nil {
			st.s.TurnRequests = map[string]turnReceipt{}
		}
		updated := run
		updated.Inputs = append(append([]agent.UserInput(nil), run.Inputs...), input)
		st.s.Runs[i] = updated
		st.s.TurnRequests[input.ID] = turnReceipt{RunID: runID, Digest: digest}
		if err := st.save(); err != nil {
			st.s.Runs[i] = run
			delete(st.s.TurnRequests, input.ID)
			return Run{}, err
		}
		return updated, nil
	}
	return Run{}, errors.New("the conversation is no longer running")
}

// UpdateRunInput uses copy-on-write so snapshots and HTTP responses never race
// with native acknowledgements. Accepted messages are also retained for history.
func (st *Store) UpdateRunInput(runID string, input agent.UserInput) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, run := range st.s.Runs {
		if run.ID != runID {
			continue
		}
		for j, previous := range run.Inputs {
			if previous.ID != input.ID || previous.Status != "queued" {
				continue
			}
			inputs := append([]agent.UserInput(nil), run.Inputs...)
			inputs[j] = input
			st.s.Runs[i].Inputs = inputs
			n := len(st.s.Messages)
			if input.Status == "accepted" {
				st.s.Messages = append(st.s.Messages, Message{ID: "input-" + input.ID, ChatID: run.ChatID,
					Role: "user", Text: input.Text, Attachments: input.Attachments, CreatedAt: input.CreatedAt})
			}
			if err := st.save(); err != nil {
				st.s.Runs[i].Inputs = run.Inputs
				st.s.Messages = st.s.Messages[:n]
				return err
			}
			return nil
		}
	}
	return nil
}

// SaveTurnMessages replaces the acknowledged input placeholders with the
// completed, interleaved transcript in one write. Other chats are untouched.
func (st *Store) SaveTurnMessages(messages []Message) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	ids := make(map[string]bool, len(messages))
	for _, message := range messages {
		ids[message.ID] = true
	}
	old := st.s.Messages
	updated := make([]Message, 0, len(old)+len(messages))
	for _, message := range old {
		if !ids[message.ID] {
			updated = append(updated, message)
		}
	}
	st.s.Messages = append(updated, messages...)
	if err := st.save(); err != nil {
		st.s.Messages = old
		return err
	}
	return nil
}
