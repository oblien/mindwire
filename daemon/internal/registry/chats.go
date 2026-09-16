package registry

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/oblien/mindwire/daemon/internal/session"
)

// RestoreSessions preserves migration hints without replacing newer native session mappings.
// HTTP and the embedded Go SDK use the same bridge between membership and native history.
func (st *Store) RestoreSessions(chats []Chat, native *session.Store) error {
	for _, row := range chats {
		chat, profile, project, err := st.ChatContext(row.ID)
		if errors.Is(err, ErrDeleted) {
			continue
		}
		if err != nil {
			return err
		}
		if chat == nil {
			continue
		}
		if native.Session(profile.AgentType, row.ID) == "" && chat.SessionID != "" {
			if err := native.SetSession(profile.AgentType, row.ID, chat.SessionID); err != nil {
				return err
			}
		}
		if native.ChatCWD(row.ID) == "" {
			if err := native.SetChatCWD(row.ID, project.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

// MergeSummaries includes saved empty chats and removes tombstoned native listings.
func (st *Store) MergeSummaries(summaries []session.ChatSummary) ([]session.ChatSummary, error) {
	snapshot, err := st.Snapshot(nil)
	if err != nil {
		return nil, err
	}
	deleted := map[string]bool{}
	for _, row := range snapshot.Deleted {
		if row.Kind == "chats" {
			deleted[row.ID] = true
		}
	}
	profiles := map[string]string{}
	for _, row := range snapshot.Agents {
		profiles[row.ID] = row.AgentType
	}
	seen := map[string]bool{}
	kept := make([]session.ChatSummary, 0, len(summaries)+len(snapshot.Chats))
	for _, row := range summaries {
		if !deleted[row.ChatID] {
			seen[row.ChatID] = true
			kept = append(kept, row)
		}
	}
	for _, row := range snapshot.Chats {
		if !seen[row.ID] {
			kept = append(kept, session.ChatSummary{ChatID: row.ID, Agent: profiles[row.AgentID], Title: row.Title, UpdatedAt: row.CreatedAt})
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].UpdatedAt > kept[j].UpdatedAt })
	return kept, nil
}

// OverlaySummary fills metadata absent from native state. The return value says the title is pinned.
func (st *Store) OverlaySummary(summary *session.ChatSummary) bool {
	chat, profile, _, err := st.ChatContext(summary.ChatID)
	if err != nil || chat == nil {
		return false
	}
	if summary.Agent == "" {
		summary.Agent = profile.AgentType
	}
	if summary.UpdatedAt == "" {
		summary.UpdatedAt = chat.CreatedAt
	}
	if summary.Title == "" || summary.Title == "New chat" || chat.TitleIsUserSet {
		summary.Title = chat.Title
	}
	return chat.TitleIsUserSet
}

// ForkChat forks registered membership and native mappings together. false means an unregistered
// legacy chat, which the caller handles in the native store. The caller serializes chat mutations.
func (st *Store) ForkChat(src, newID string, native *session.Store) (bool, error) {
	chat, _, _, err := st.ChatContext(src)
	if err != nil || chat == nil {
		return false, err
	}
	if !validID(newID) || src == newID {
		return true, invalid("fork target must be a valid, different chat ID")
	}
	target, _, _, err := st.ChatContext(newID)
	if err != nil {
		return true, err
	}
	if target != nil || native.ChatExists(newID) {
		return true, invalid("target chat already exists")
	}
	hasHistory := native.ChatExists(src)
	if hasHistory {
		if err := native.ForkChat(src, newID); err != nil {
			_, _ = native.DeleteChat(newID)
			return true, err
		}
	}
	chat.ID, chat.CreatedAt, chat.Revision, chat.SessionID = newID, "", 0, ""
	data, _ := json.Marshal(chat)
	if err := st.Put("chats", newID, data, nil); err != nil {
		if hasHistory {
			_, _ = native.DeleteChat(newID) // only this new mapping, never its native transcript
		}
		return true, err
	}
	return true, nil
}
