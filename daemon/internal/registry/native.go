package registry

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

// SessionDiscoveryIssue means cached membership was retained because a native
// listing could not be completed. It is independent of workspace availability.
type SessionDiscoveryIssue struct {
	ProjectID string `json:"projectId"`
	Agent     string `json:"agent"`
	Message   string `json:"message"`
}

type NativeInventory struct {
	ProjectID, CWD, AgentType, AgentName string
	Sessions                             []agent.NativeSession
}

type nativeLink struct {
	ChatID string
	Hidden bool
}

// Link before deletion, in the same transaction, including a session created
// since the last discovery pass. Failed deletes roll back the suppression too.
func rememberRemovedSessions(tx *sql.Tx, kind, id string, refs []session.ChatSession) error {
	if kind == "projects" {
		return nil
	} // its links are deliberately forgotten
	column := "id"
	if kind == "agents" {
		column = "agent_id"
	}
	rows, err := tx.Query("SELECT data FROM chats WHERE "+column+"=?", id)
	if err != nil {
		return err
	}
	var chats []Chat
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		var chat Chat
		if err = json.Unmarshal(data, &chat); err != nil {
			rows.Close()
			return err
		}
		chats = append(chats, chat)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, chat := range chats {
		data, _, err := record(tx, "agents", chat.AgentID)
		if err != nil {
			return err
		}
		var profile Agent
		if err = json.Unmarshal(data, &profile); err != nil {
			return err
		}
		sid, pending := chat.SessionID, false
		for _, ref := range refs {
			if ref.ChatID == chat.ID && ref.Agent == profile.AgentType {
				sid, pending = ref.SID, ref.ForkPending
				break
			}
		}
		if sid == "" || pending {
			continue
		}
		if _, err = tx.Exec("INSERT OR IGNORE INTO native_chat_links VALUES (?,?,?,?,0)", chat.ProjectID, profile.AgentType, sid, chat.ID); err != nil {
			return err
		}
	}
	return nil
}

func nativeID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "native-" + hex.EncodeToString(sum[:16])
}

func nativeAliases(row agent.NativeSession) []string {
	out := []string{row.ID}
	for _, alias := range row.Aliases {
		if alias != row.ID && agent.ValidNativeSessionID(alias) {
			out = append(out, alias)
		}
	}
	return out
}

// SyncNativeSessions refreshes only links, titles and activity. The supplied
// inventory must be complete; a failed/partial native listing must never call
// this method. Transcripts remain in the harness and are read/resumed by ID.
// Callers hold their mutation gate so starting/deleting a chat cannot race this
// association commit. The slow native scan happens before acquiring that gate.
func (st *Store) SyncNativeSessions(in NativeInventory, native *session.Store, busy func(string) bool) error {
	refs := native.ChatSessions()
	sort.Slice(refs, func(i, j int) bool { return refs[i].ChatID < refs[j].ChatID })
	current := map[string]session.ChatSession{}
	refsBySession := map[string][]session.ChatSession{}
	for _, ref := range refs {
		if ref.Agent == in.AgentType {
			current[ref.ChatID] = ref
			if !ref.ForkPending {
				refsBySession[ref.SID] = append(refsBySession[ref.SID], ref)
			}
		}
	}
	var restore []session.ChatSession
	err := st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		data, _, err := record(tx, "projects", in.ProjectID)
		if err != nil || data == nil {
			return false, err
		} // removed while listing
		var project Project
		if err = json.Unmarshal(data, &project); err != nil {
			return false, err
		}
		path, err := workspacepath.Canonical(project.Path)
		if err != nil {
			return false, err
		}
		if path != in.CWD {
			return false, nil
		} // moved while listing
		if err = checkProjectRemoval(tx, data); err != nil {
			return false, err
		}

		profiles := map[string]Agent{}
		var defaultProfile Agent
		rows, err := tx.Query("SELECT data FROM agents ORDER BY id")
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var data []byte
			if err = rows.Scan(&data); err != nil {
				rows.Close()
				return false, err
			}
			var profile Agent
			if err = json.Unmarshal(data, &profile); err != nil {
				rows.Close()
				return false, err
			}
			profiles[profile.ID] = profile
			if profile.AgentType == in.AgentType && defaultProfile.ID == "" {
				defaultProfile = profile
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}

		chats := map[string]Chat{}
		bySession := map[string]string{}
		rows, err = tx.Query("SELECT data FROM chats WHERE project_id=? ORDER BY id", project.ID)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var data []byte
			if err = rows.Scan(&data); err != nil {
				rows.Close()
				return false, err
			}
			var chat Chat
			if err = json.Unmarshal(data, &chat); err != nil {
				rows.Close()
				return false, err
			}
			if profiles[chat.AgentID].AgentType != in.AgentType {
				continue
			}
			chats[chat.ID] = chat
			ref := current[chat.ID]
			if ref.ForkPending {
				continue
			} // a fork deliberately shares its parent's session until its first turn
			sid := agent.FirstNonEmpty(ref.SID, chat.SessionID)
			if sid != "" && bySession[sid] == "" {
				bySession[sid] = chat.ID
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		links := map[string]nativeLink{}
		rows, err = tx.Query("SELECT session_id,chat_id,hidden FROM native_chat_links WHERE project_id=? AND agent_type=?", project.ID, in.AgentType)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var sid string
			var link nativeLink
			if err = rows.Scan(&sid, &link.ChatID, &link.Hidden); err != nil {
				rows.Close()
				return false, err
			}
			links[sid] = link
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}

		changed := false
		seenChats := map[string]bool{}
		for _, row := range in.Sessions {
			actual, err := workspacepath.Canonical(row.CWD)
			if err != nil || actual != path || !agent.ValidNativeSessionID(row.ID) {
				continue
			}
			aliases := nativeAliases(row)
			hidden, id := false, ""
			for _, sid := range aliases {
				if links[sid].Hidden {
					hidden = true
				}
				if id == "" {
					id = bySession[sid]
				}
			}
			if hidden {
				continue
			}
			if id == "" {
				for _, sid := range aliases {
					link := links[sid]
					if link.ChatID == "" {
						continue
					}
					ref := current[link.ChatID]
					if ref.ForkPending {
						continue
					}
					// A fork/continuation may have acquired a new native ID. The old
					// session gets its own link, never steals the new continuation.
					if ref.SID == "" || ref.SID == sid || ref.SID == row.ID {
						id = link.ChatID
						break
					}
				}
			}
			if id == "" {
				// Legacy daemon chats may predate workspace metadata. Adopt their
				// existing IDs when the harness/session and exact folder match.
				var candidates []session.ChatSession
				for _, sid := range aliases {
					candidates = append(candidates, refsBySession[sid]...)
				}
				for _, ref := range candidates {
					cwd, _ := workspacepath.Canonical(ref.CWD)
					if cwd != path {
						continue
					}
					other, _, err := record(tx, "chats", ref.ChatID)
					if err != nil {
						return false, err
					}
					gone, err := isDeleted(tx, "chats", ref.ChatID)
					if err != nil {
						return false, err
					}
					if other == nil && !gone {
						id = ref.ChatID
						break
					}
				}
			}
			if id == "" {
				id = nativeID(st.identity, project.ID, in.AgentType, row.ID)
			}
			gone, err := isDeleted(tx, "chats", id)
			if err != nil {
				return false, err
			}
			if gone {
				// Only a previously indexed, externally removed session may return.
				// Explicit user deletions have hidden links and never reach here.
				reappeared := false
				for _, sid := range aliases {
					reappeared = reappeared || links[sid].ChatID == id && !links[sid].Hidden
				}
				if !reappeared {
					continue
				}
				if _, err = tx.Exec("DELETE FROM deleted WHERE kind='chats' AND id=?", id); err != nil {
					return false, err
				}
			}
			chat, exists := chats[id]
			if !exists {
				if defaultProfile.ID == "" {
					defaultProfile = Agent{Record: Record{ID: nativeID(st.identity, in.AgentType, strconv.FormatInt(revision, 10)),
						WorkspaceID: st.identity, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Revision: revision},
						Name: in.AgentName, AgentTypeName: in.AgentName, AgentType: in.AgentType}
					data, _ := json.Marshal(defaultProfile)
					if err = save(tx, "agents", defaultProfile.ID, data, revision, "", ""); err != nil {
						return false, err
					}
					profiles[defaultProfile.ID] = defaultProfile
				}
				created := row.CreatedAt
				if _, err := time.Parse(time.RFC3339Nano, created); err != nil {
					created = time.Now().UTC().Format(time.RFC3339Nano)
				}
				chat = Chat{Record: Record{ID: id, WorkspaceID: st.identity, CreatedAt: created}, ProjectID: project.ID, AgentID: defaultProfile.ID}
			}
			previous, _ := json.Marshal(chat)
			chat.SessionID = agent.FirstNonEmpty(current[id].SID, row.ID)
			chat.NativeTitle = agent.FirstNonEmpty(agent.SessionTitle(row.Title), "Conversation")
			if title := native.Title(id); title != "" {
				chat.Title, chat.TitleIsUserSet = title, true
			} else if !chat.TitleIsUserSet {
				chat.Title = chat.NativeTitle
			}
			if _, err := time.Parse(time.RFC3339Nano, row.UpdatedAt); err == nil {
				chat.UpdatedAt = row.UpdatedAt
			}
			updated, _ := json.Marshal(chat)
			if gone || !exists || string(previous) != string(updated) {
				chat.Revision = revision
				data, _ := json.Marshal(chat)
				if err = save(tx, "chats", id, data, revision, chat.AgentID, project.ID); err != nil {
					return false, err
				}
				changed = true
			}
			chats[id], seenChats[id] = chat, true
			for _, sid := range aliases {
				if links[sid].ChatID == id {
					continue
				}
				if _, err = tx.Exec(`INSERT INTO native_chat_links VALUES (?,?,?,?,0)
 ON CONFLICT(project_id,agent_type,session_id) DO UPDATE SET chat_id=excluded.chat_id`, project.ID, in.AgentType, sid, id); err != nil {
					return false, err
				}
				links[sid] = nativeLink{ChatID: id}
			}
			restore = append(restore, session.ChatSession{ChatID: id, Agent: in.AgentType, SID: chat.SessionID, CWD: project.Path})
		}
		// A complete native listing is authoritative for previously discovered
		// conversations. Never prune drafts, pending forks or an in-flight turn.
		for sid, link := range links {
			chat, exists := chats[link.ChatID]
			ref := current[link.ChatID]
			if link.Hidden || !exists || seenChats[link.ChatID] || ref.ForkPending || busy != nil && busy(link.ChatID) {
				continue
			}
			if actual := agent.FirstNonEmpty(ref.SID, chat.SessionID); actual != sid {
				continue
			}
			gone, err := isDeleted(tx, "chats", chat.ID)
			if err != nil {
				return false, err
			}
			if gone {
				continue
			}
			// Keep app preferences in their existing metadata row while the
			// native session is absent/archived; only its public link disappears.
			if _, err = tx.Exec("INSERT OR REPLACE INTO deleted VALUES ('chats',?,?)", chat.ID, revision); err != nil {
				return false, err
			}
			delete(chats, chat.ID)
			changed = true
		}
		return changed, nil
	})
	if err != nil {
		return err
	}
	return native.RestoreChatSessions(restore)
}
