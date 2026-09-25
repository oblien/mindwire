package registry

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/oblien/mindwire/daemon/internal/session"
)

// NativeChatContext also recovers references after a state-file repair/restore.
// The registry stores only this pointer; the harness still owns every message.
func (st *Store) NativeChatContext(id string, native *session.Store) (*Chat, *Agent, *Project, error) {
	chat, profile, project, err := st.ChatContext(id)
	if err != nil || chat == nil {
		return chat, profile, project, err
	}
	err = native.RestoreChatSessions([]session.ChatSession{{ChatID: id, Agent: profile.AgentType, SID: chat.SessionID, CWD: project.Path}})
	return chat, profile, project, err
}

// RestoreSessions preserves migration hints without replacing newer native session mappings.
// HTTP and the embedded Go SDK use the same bridge between membership and native history.
func (st *Store) RestoreSessions(chats []Chat, native *session.Store) error {
	refs := make([]session.ChatSession, 0, len(chats))
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
		refs = append(refs, session.ChatSession{ChatID: row.ID, Agent: profile.AgentType, SID: chat.SessionID, CWD: project.Path})
	}
	return native.RestoreChatSessions(refs)
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
			updated := row.UpdatedAt
			if updated == "" {
				updated = row.CreatedAt
			}
			kept = append(kept, session.ChatSummary{ChatID: row.ID, Agent: profiles[row.AgentID], Title: row.Title, UpdatedAt: updated})
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].UpdatedAt > kept[j].UpdatedAt })
	return kept, nil
}

// OverlaySummary applies saved preferences and native metadata. true means the
// title is already resolved; the caller need not rescan its native transcript.
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
	if chat.UpdatedAt > summary.UpdatedAt {
		summary.UpdatedAt = chat.UpdatedAt
	}
	if summary.Title == "" || summary.Title == "New chat" || chat.TitleIsUserSet || chat.UpdatedAt != "" {
		summary.Title = chat.Title
	}
	// A discovered title is already read from native metadata in a bounded,
	// cached pass. Do not rescan a whole transcript for every sidebar poll.
	return chat.TitleIsUserSet || chat.UpdatedAt != ""
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
