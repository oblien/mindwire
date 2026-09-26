package session

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// PortableChat is deliberately selected data, not a state-file backup. Config,
// credentials, notifications, pending interactions and turn idempotency receipts
// belong to the original daemon and must never be installed on another machine.
type PortableChat struct {
	Title       string    `json:"title,omitempty"`
	Messages    []Message `json:"messages"`
	Runs        []Run     `json:"runs"`
	ForkPending bool      `json:"forkPending,omitempty"`
}

func (st *Store) ExportChat(agentType, chatID string) (PortableChat, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := PortableChat{Title: st.s.Titles[chatID], ForkPending: st.s.ForkPending[sessionKey(agentType, chatID)], Messages: []Message{}, Runs: []Run{}}
	for _, m := range st.s.Messages {
		if m.ChatID == chatID {
			m.Parts = st.overlayInteractions(chatID, m.Parts)
			m.ChatID = ""
			out.Messages = append(out.Messages, m)
		}
	}
	for _, r := range st.s.Runs {
		if r.ChatID == chatID {
			if r.Status == "running" {
				return out, fmt.Errorf("wait for this chat to finish or stop it before synchronizing")
			}
			r.ChatID = ""
			out.Runs = append(out.Runs, r)
		}
	}
	// Return an owned deep copy; ongoing unrelated chats can mutate nested parts.
	b, err := json.Marshal(out)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	return out, err
}

type ChatImport struct {
	ChatID, Agent, SessionID, CWD string
	Data                          PortableChat
	// Expected is the selected local history captured before a sync journal was
	// committed. It permits a verified merge/enrichment, never a blind replacement.
	Expected *PortableChat
}

// ImportChats is one atomic JSON-store write. Replaying a recovered transaction
// cannot duplicate a message/run, and ID collisions fail rather than overwrite.
func (st *Store) ImportChats(chats []ChatImport) error {
	return st.importChats(chats, true)
}

func (st *Store) ValidateChatImports(chats []ChatImport) error { return st.importChats(chats, false) }

func (st *Store) importChats(chats []ChatImport, write bool) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	backup, err := json.Marshal(st.s)
	if err != nil {
		return err
	}
	apply := func() error {
		messageIndex, runIndex := map[[2]string]int{}, map[[2]string]int{}
		busy := map[string]bool{}
		for i, m := range st.s.Messages {
			messageIndex[[2]string{m.ChatID, m.ID}] = i
		}
		for i, r := range st.s.Runs {
			runIndex[[2]string{r.ChatID, r.ID}] = i
			if r.Status == "running" {
				busy[r.ChatID] = true
			}
		}
		for _, c := range chats {
			if busy[c.ChatID] {
				return fmt.Errorf("chat is running")
			}
			messages, runs := map[string]Message{}, map[string]Run{}
			if c.Expected != nil {
				for _, m := range c.Expected.Messages {
					m.ChatID = c.ChatID
					messages[m.ID] = m
				}
				for _, r := range c.Expected.Runs {
					r.ChatID = c.ChatID
					runs[r.ID] = r
				}
			}
			for _, m := range c.Data.Messages {
				if m.ID == "" {
					return fmt.Errorf("imported message has no identity")
				}
				m.ChatID = c.ChatID
				key := [2]string{c.ChatID, m.ID}
				if index, found := messageIndex[key]; found {
					old := st.s.Messages[index]
					old.Parts = st.overlayInteractions(c.ChatID, old.Parts)
					if !reflect.DeepEqual(old, m) {
						expected, ok := messages[m.ID]
						if !ok || !reflect.DeepEqual(old, expected) {
							return fmt.Errorf("recorded message changed independently")
						}
						st.s.Messages[index] = m
					}
				} else {
					messageIndex[key] = len(st.s.Messages)
					st.s.Messages = append(st.s.Messages, m)
				}
			}
			for _, r := range c.Data.Runs {
				if r.Status == "running" || r.ID == "" {
					return fmt.Errorf("cannot import a running process")
				}
				r.ChatID = c.ChatID
				key := [2]string{c.ChatID, r.ID}
				if index, found := runIndex[key]; found {
					old := st.s.Runs[index]
					if !reflect.DeepEqual(old, r) {
						expected, ok := runs[r.ID]
						if !ok || !reflect.DeepEqual(old, expected) {
							return fmt.Errorf("recorded turn changed independently")
						}
						st.s.Runs[index] = r
					}
				} else {
					runIndex[key] = len(st.s.Runs)
					st.s.Runs = append(st.s.Runs, r)
				}
			}
			st.s.Cwds[c.ChatID] = c.CWD
			st.s.Sessions[sessionKey(c.Agent, c.ChatID)] = c.SessionID
			if c.Data.Title != "" {
				st.s.Titles[c.ChatID] = c.Data.Title
			}
			st.s.ForkPending[sessionKey(c.Agent, c.ChatID)] = c.Data.ForkPending
		}
		if write {
			return st.save()
		}
		return nil
	}
	if err = apply(); err != nil || !write {
		st.s = newState()
		_ = json.Unmarshal(backup, &st.s)
	}
	return err
}
