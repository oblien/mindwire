package session

import (
	"fmt"
	"github.com/oblien/mindwire/daemon/internal/agent"
)

type ForkOptions struct {
	Agent string
	Point agent.ForkPoint
}

// Origin survives completion, so replaying a lost HTTP acknowledgement returns
// the existing branch even after its first turn has created the native session.
type ChatFork struct {
	SourceChatID string          `json:"sourceChatId"`
	Agent        string          `json:"agent,omitempty"`
	Point        agent.ForkPoint `json:"point"`
}

func (st *Store) ForkMatches(source, target, before string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	origin, ok := st.s.Forks[target]
	return ok && origin.SourceChatID == source && origin.Point.BeforeMessageID == before && st.chatExistsLocked(target)
}

// Shared by HTTP and the embedded SDK; adapters own every native boundary.
func (st *Store) PrepareFork(adapter agent.Adapter, source, before, defaultCWD string) (ForkOptions, error) {
	if st.PendingFork(adapter.ID(), source) != nil {
		return ForkOptions{}, fmt.Errorf("send the pending branch's first message before forking it again")
	}
	cwd := agent.FirstNonEmpty(st.ChatCWD(source), defaultCWD)
	point, err := agent.PrepareFork(adapter, agent.HistoryQuery{
		ChatID: source, SessionID: st.Session(adapter.ID(), source), CWD: cwd, Recorded: st.Messages(source),
	}, before)
	return ForkOptions{Agent: adapter.ID(), Point: point}, err
}
