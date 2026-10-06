package session

// ArtifactOwners preserves a fork's inherited inputs when its source is deleted.
// Called only during deletion, under the workspace mutation lock; no idle scan.
func (st *Store) ArtifactOwners(excluding string) map[string]string {
	st.mu.Lock()
	defer st.mu.Unlock()
	owners := map[string]string{}
	for _, message := range st.s.Messages {
		if message.ChatID == excluding {
			continue
		}
		for _, attachment := range message.Attachments {
			if attachment.ArtifactID != "" {
				owners[attachment.ArtifactID] = message.ChatID
			}
		}
		for _, part := range message.Parts {
			if part.Tool != nil && part.Tool.Action != nil && part.Tool.Action.Surface != nil && part.Tool.Action.Surface.Capture != nil {
				owners[part.Tool.Action.Surface.Capture.ArtifactID] = message.ChatID
			}
			if part.Input != nil {
				for _, attachment := range part.Input.Attachments {
					if attachment.ArtifactID != "" {
						owners[attachment.ArtifactID] = message.ChatID
					}
				}
			}
		}
	}
	return owners
}
