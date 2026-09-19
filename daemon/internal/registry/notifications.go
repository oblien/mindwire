package registry

import "errors"

// NotificationsMuted reads the current chat and profile in one snapshot. Preferences
// belong to saved profile IDs, not harness types: two Codex profiles remain independent.
// Unregistered native/legacy chats retain the default delivery behavior.
func (st *Store) NotificationsMuted(chatID string) (bool, error) {
	chat, profile, _, err := st.ChatContext(chatID)
	if errors.Is(err, ErrDeleted) {
		return true, nil
	}
	if err != nil || chat == nil {
		return false, err
	}
	return (chat.NotificationsMuted != nil && *chat.NotificationsMuted) ||
		(profile.NotificationsMuted != nil && *profile.NotificationsMuted), nil
}
