package session

import (
	"fmt"
	"github.com/oblien/mindwire/daemon/internal/agent"
	"strings"
)

type ForkOptions struct {
	Agent string
	Point agent.ForkPoint
	// Only durable input metadata is copied; the harness still owns history.
	Attachments []Message
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
	query := agent.HistoryQuery{
		ChatID: source, SessionID: st.Session(adapter.ID(), source), CWD: cwd, Recorded: st.Messages(source),
	}
	point, err := agent.PrepareFork(adapter, query, before)
	options := ForkOptions{Agent: adapter.ID(), Point: point}
	if err != nil || len(durableInputs(query.Recorded, source)) == 0 {
		return options, err
	}
	history, err := adapter.History(query)
	if err != nil {
		return options, err
	}
	prefix, err := agent.ForkHistory(history, &point)
	if err != nil {
		return options, err
	}
	options.Attachments = durableInputs(prefix, source)
	return options, nil
}

func durableInputs(messages []Message, chatID string) []Message {
	var result []Message
	var queued []Message
	flush := func() {
		for _, message := range queued {
			if len(message.Attachments) != 0 {
				result = append(result, queued...)
				break
			}
		}
		queued = nil
	}
	for _, message := range messages {
		if message.ChatID != chatID {
			continue
		}
		isQueued := message.Role == "user" && strings.HasPrefix(message.ID, "input-")
		if !isQueued {
			flush()
		}
		if message.Role != "user" {
			continue
		}
		var attachments []agent.Attachment
		for _, attachment := range message.Attachments {
			if attachment.ArtifactID != "" {
				attachment.Data = nil
				attachments = append(attachments, attachment)
			}
		}
		input := Message{ID: message.ID, ChatID: chatID, Role: "user", Text: message.Text,
			CreatedAt: message.CreatedAt, Attachments: attachments}
		if isQueued {
			// Native CLIs can coalesce queued inputs. Retain their surrounding text
			// so history can match that batch without inserting duplicate messages.
			queued = append(queued, input)
		} else if len(attachments) != 0 {
			result = append(result, input)
		}
	}
	flush()
	return result
}
