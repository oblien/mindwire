package session

import (
	"crypto/rand"
	"strconv"
)

// HistoryRevision is a constant-time change token for daemon-owned history and
// interaction overlays. It adds no transcript copy, file read or idle work. A
// fresh epoch after restart conservatively invalidates clients' retained pages.
func (st *Store) HistoryRevision(chatID string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.historyEpoch == "" {
		st.historyEpoch = rand.Text()
	}
	return st.historyEpoch + ":" + strconv.FormatUint(st.historyVersions[chatID], 10)
}

// The caller holds st.mu. Failed mutations may invalidate a token conservatively;
// unrelated configuration changes and activity in other chats do not.
func (st *Store) touchHistory(chatID string) {
	if st.historyVersions == nil {
		st.historyVersions = map[string]uint64{}
	}
	st.historyClock++
	st.historyVersions[chatID] = st.historyClock
}
