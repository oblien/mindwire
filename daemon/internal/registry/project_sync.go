package registry

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

// SyncKey identifies a logical record without exposing machine-local paths. Local
// IDs remain workspace-specific, so two replicas cannot collide in client caches.
func SyncKey(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func withoutSyncIdentity(kind string, data []byte) ([]byte, error) {
	if kind != "projects" && kind != "chats" {
		return data, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, invalid("record JSON")
	}
	if _, ok := fields["syncId"]; !ok {
		return data, nil
	}
	delete(fields, "syncId")
	return json.Marshal(fields)
}

func checkSyncAtPath(q queryer, path, except string) error {
	rows, err := q.Query("SELECT id,path FROM project_sync_locks WHERE id<>?", except)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, other string
		if err := rows.Scan(&id, &other); err != nil {
			return err
		}
		if workspacepath.Overlaps(path, other) {
			return fmt.Errorf("%w: project synchronization is protecting this directory (%s)", ErrConflict, id)
		}
	}
	return rows.Err()
}

// CheckSyncPath protects file/terminal execution, including paths that do not
// exist yet. Call under the shared admission mutex before starting a mutation.
func (st *Store) CheckSyncPath(path string) error {
	canonical, err := workspacepath.Canonical(path)
	if err != nil {
		return invalid(err.Error())
	}
	return checkSyncAtPath(st.db, canonical, "")
}

func checkRecordSync(q queryer, kind, id string) error {
	var query string
	switch kind {
	case "chats":
		query = "SELECT p.data FROM projects p JOIN chats c ON c.project_id=p.id WHERE c.id=?"
	case "agents":
		query = "SELECT DISTINCT p.data FROM projects p JOIN chats c ON c.project_id=p.id WHERE c.agent_id=?"
	default:
		return nil
	}
	rows, err := q.Query(query, id)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		var p Project
		if err = json.Unmarshal(data, &p); err != nil {
			rows.Close()
			return err
		}
		path, e := workspacepath.Canonical(p.Path)
		if e != nil {
			rows.Close()
			return e
		}
		paths = append(paths, path)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err = checkSyncAtPath(q, path, ""); err != nil {
			return err
		}
	}
	return nil
}

// ReserveSync participates in the same persistent path reservations as Git,
// project removal and new turns. The caller holds the turn-admission mutex.
func (st *Store) ReserveSync(id, path string) error {
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkSyncAtPath(tx, path, id); err != nil {
		return err
	}
	if err = checkGitAtPath(tx, path); err != nil {
		return err
	}
	if _, err = conflictingOperation(tx, path, true, ""); err == nil {
		return fmt.Errorf("%w: another project operation uses this directory", ErrConflict)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = tx.Exec("INSERT INTO project_sync_locks(id,path) VALUES(?,?) ON CONFLICT(id) DO NOTHING", id, path)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (st *Store) ReleaseSync(id string) error {
	_, err := st.db.Exec("DELETE FROM project_sync_locks WHERE id=?", id)
	return err
}

func (st *Store) SyncPut(kind, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = st.db.Exec("INSERT INTO project_sync_records(kind,id,data) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data", kind, id, data)
	return err
}

func (st *Store) SyncGet(kind, id string, out any) error {
	var data []byte
	if err := st.db.QueryRow("SELECT data FROM project_sync_records WHERE kind=? AND id=?", kind, id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return json.Unmarshal(data, out)
}

func (st *Store) SyncList(kind string) ([]json.RawMessage, error) {
	rows, err := st.db.Query("SELECT data FROM project_sync_records WHERE kind=? ORDER BY id", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// EnsureSyncIdentity changes only membership metadata, never the source files or
// transcripts. IDs survive renames and old-client writes.
func (st *Store) EnsureSyncIdentity(projectID string) error {
	return st.write(func(tx *sql.Tx, rev int64) (bool, error) {
		data, _, err := record(tx, "projects", projectID)
		if err != nil {
			return false, err
		}
		if data == nil {
			return false, ErrNotFound
		}
		var p Project
		if err = json.Unmarshal(data, &p); err != nil {
			return false, err
		}
		changed := false
		if p.SyncID == "" {
			p.SyncID = SyncKey(st.identity, "project", p.ID)
			p.Revision = rev
			b, _ := json.Marshal(p)
			if err = save(tx, "projects", p.ID, b, rev, "", ""); err != nil {
				return false, err
			}
			changed = true
		}
		rows, err := tx.Query("SELECT data FROM chats WHERE project_id=?", projectID)
		if err != nil {
			return false, err
		}
		var chats []Chat
		for rows.Next() {
			var b []byte
			if err = rows.Scan(&b); err != nil {
				rows.Close()
				return false, err
			}
			var c Chat
			if err = json.Unmarshal(b, &c); err != nil {
				rows.Close()
				return false, err
			}
			chats = append(chats, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		for _, c := range chats {
			if c.SyncID != "" {
				continue
			}
			c.SyncID = SyncKey(st.identity, "chat", c.ID)
			c.Revision = rev
			b, _ := json.Marshal(c)
			if err = save(tx, "chats", c.ID, b, rev, c.AgentID, c.ProjectID); err != nil {
				return false, err
			}
			changed = true
		}
		return changed, nil
	})
}

// CompleteSync atomically publishes replica membership and its acknowledged
// checkpoint after the recoverable filesystem transaction finishes. No unrelated
// profile, project, chat, credential, deletion or native link is replaced.
func (st *Store) CompleteSync(batch Import, syncID, checkpoint, operationID string, artifacts map[string]json.RawMessage, forkPending map[string]bool) error {
	return st.write(func(tx *sql.Tx, rev int64) (bool, error) {
		for _, kind := range []string{"agents", "projects", "chats"} {
			var values []any
			switch kind {
			case "agents":
				for _, v := range batch.Agents {
					values = append(values, v)
				}
			case "projects":
				for _, v := range batch.Projects {
					values = append(values, v)
				}
			case "chats":
				for _, v := range batch.Chats {
					values = append(values, v)
				}
			}
			for _, v := range values {
				b, err := json.Marshal(v)
				if err != nil {
					return false, err
				}
				var base Record
				_ = json.Unmarshal(b, &base)
				if gone, err := isDeleted(tx, kind, base.ID); err != nil {
					return false, err
				} else if gone {
					return false, ErrDeleted
				}
				old, _, err := record(tx, kind, base.ID)
				if err != nil {
					return false, err
				}
				if kind == "agents" && old != nil {
					continue
				}
				value, aid, pid, err := st.normalize(kind, base.ID, b, rev)
				if err != nil {
					return false, err
				}
				if err = save(tx, kind, base.ID, value, rev, aid, pid); err != nil {
					return false, err
				}
				if kind == "chats" {
					var c Chat
					_ = json.Unmarshal(value, &c)
					var profile Agent
					ab, _, err := record(tx, "agents", c.AgentID)
					if err != nil {
						return false, err
					}
					_ = json.Unmarshal(ab, &profile)
					if c.SessionID != "" && !forkPending[c.ID] {
						_, err = tx.Exec("INSERT INTO native_chat_links(project_id,agent_type,session_id,chat_id,hidden) VALUES(?,?,?,?,0) ON CONFLICT(project_id,agent_type,session_id) DO UPDATE SET chat_id=excluded.chat_id,hidden=0", c.ProjectID, profile.AgentType, c.SessionID, c.ID)
						if err != nil {
							return false, err
						}
					}
				}
			}
		}
		for id, data := range artifacts {
			if _, err := tx.Exec("INSERT INTO surface_records(kind,id,data,updated_at) VALUES('artifact',?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at", id, []byte(data), operationNow()); err != nil {
				return false, err
			}
		}
		b, _ := json.Marshal(checkpoint)
		if _, err := tx.Exec("INSERT INTO project_sync_records(kind,id,data) VALUES('head',?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data", syncID, b); err != nil {
			return false, err
		}
		b, _ = json.Marshal(true)
		if _, err := tx.Exec("INSERT OR REPLACE INTO project_sync_records(kind,id,data) VALUES('committed',?,?)", operationID, b); err != nil {
			return false, err
		}
		if _, err := tx.Exec("DELETE FROM project_sync_locks WHERE id=?", operationID); err != nil {
			return false, err
		}
		return true, nil
	})
}
