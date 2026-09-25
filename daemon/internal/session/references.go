package session

// ChatSession is a reference into a harness's native store. It contains no
// messages; discovery and registry migration use it to preserve resume identity.
type ChatSession struct {
	ChatID, Agent, SID, CWD string
	ForkPending             bool
}

func (st *Store) ChatSessions() []ChatSession {
	st.mu.Lock()
	defer st.mu.Unlock()
	refs := make([]ChatSession, 0, len(st.s.Sessions))
	for key, sid := range st.s.Sessions {
		for i := 0; i < len(key); i++ {
			if key[i] == '\x1f' {
				id := key[i+1:]
				refs = append(refs, ChatSession{ChatID: id, Agent: key[:i], SID: sid, CWD: st.s.Cwds[id], ForkPending: st.s.ForkPending[key]})
				break
			}
		}
	}
	return refs
}

// RestoreChatSessions fills missing references atomically, with a single state
// write for an entire native inventory. A newer running session always wins.
func (st *Store) RestoreChatSessions(refs []ChatSession) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	addedSessions, addedCWDs := []string{}, []string{}
	for _, ref := range refs {
		key := sessionKey(ref.Agent, ref.ChatID)
		if ref.SID != "" && st.s.Sessions[key] == "" {
			st.s.Sessions[key] = ref.SID
			addedSessions = append(addedSessions, key)
		}
		if ref.CWD != "" && st.s.Cwds[ref.ChatID] == "" {
			st.s.Cwds[ref.ChatID] = ref.CWD
			addedCWDs = append(addedCWDs, ref.ChatID)
		}
	}
	if len(addedSessions)+len(addedCWDs) == 0 {
		return nil
	}
	if err := st.save(); err != nil {
		for _, key := range addedSessions {
			delete(st.s.Sessions, key)
		}
		for _, id := range addedCWDs {
			delete(st.s.Cwds, id)
		}
		return err
	}
	return nil
}
