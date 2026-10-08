package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func (a *API) historyRevision(adapter agent.Adapter, chatID string) string {
	before := a.store.HistoryRevision(chatID)
	cwd := a.store.ChatCWD(chatID)
	if cwd == "" {
		cwd = a.sup.CWD()
	}
	query := agent.HistoryQuery{ChatID: chatID, SessionID: a.store.Session(adapter.ID(), chatID),
		CWD: cwd, Fork: a.store.PendingFork(adapter.ID(), chatID)}
	native := "recorded"
	if adapter.Capabilities().History == agent.SupportNative {
		provider, ok := adapter.(agent.HistoryRevisionProvider)
		if !ok {
			return ""
		}
		var err error
		native, err = provider.HistoryRevision(query)
		if err != nil || native == "" {
			return ""
		}
	}
	if before != a.store.HistoryRevision(chatID) {
		return ""
	}
	fork, _ := json.Marshal(query.Fork)
	stamp := fmt.Sprintf("history-v1\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		adapter.ID(), chatID, before, query.SessionID, cwd, fork, native, agent.Version)
	hash := sha256.Sum256([]byte(stamp))
	return hex.EncodeToString(hash[:])
}
