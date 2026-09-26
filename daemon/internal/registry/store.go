// Package registry owns the workspace's saved agent profiles, projects and chat links.
// Harness transcripts and configuration stay in their native stores. Only the daemon
// writes this SQLite database; clients synchronize snapshots/revisions over HTTP.
package registry

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/projecticon"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"

	_ "modernc.org/sqlite"
)

const Version = 1
const NotificationPreferencesVersion = 1
const schemaVersion = 5

var (
	ErrConflict = errors.New("record changed on another client; refresh and try again")
	ErrDeleted  = errors.New("record was deleted from this workspace")
	ErrInvalid  = errors.New("invalid workspace record")
)

type Record struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	CreatedAt   string `json:"createdAt"`
	Revision    int64  `json:"revision"`
}

type Agent struct {
	Record
	Name          string `json:"name"`
	AgentType     string `json:"agentType"`
	AgentTypeName string `json:"agentTypeName,omitempty"`
	// A profile-level mute applies to all of its chats, including future ones.
	NotificationsMuted *bool `json:"notificationsMuted,omitempty"`
}

type Project struct {
	Record
	Name          string                `json:"name"`
	Path          string                `json:"path"`
	RepoURL       string                `json:"repoUrl,omitempty"`
	GitConnection *gitaccess.Connection `json:"gitConnection,omitempty"`
	IconPath      *string               `json:"iconPath,omitempty"`
}

type Chat struct {
	Record
	AgentID        string `json:"agentId"`
	ProjectID      string `json:"projectId"`
	Title          string `json:"title"`
	NativeTitle    string `json:"nativeTitle,omitempty"` // cached harness title, retained underneath a user's override
	TitleIsUserSet bool   `json:"titleIsUserSet,omitempty"`
	SessionID      string `json:"sessionId,omitempty"` // native reference; a newer active mapping in the session store wins
	UpdatedAt      string `json:"updatedAt,omitempty"` // activity reported by the native harness, not an app-open timestamp
	// Explicit false clears this chat's mute; it does not override its agent's mute.
	NotificationsMuted *bool `json:"notificationsMuted,omitempty"`
}

type Deletion struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

type Import struct {
	Agents   []Agent   `json:"agents"`
	Projects []Project `json:"projects"`
	Chats    []Chat    `json:"chats"`
}

type Snapshot struct {
	Import
	Version                int                     `json:"version"`
	WorkspaceID            string                  `json:"workspaceId"` // identity of this registry, independent of a cloud/SSH provider
	Revision               int64                   `json:"revision"`
	Full                   bool                    `json:"full"`
	Deleted                []Deletion              `json:"deleted"`
	SessionDiscoveryIssues []SessionDiscoveryIssue `json:"sessionDiscoveryIssues,omitempty"`
}

type Store struct {
	db       *sql.DB
	identity string
	path     string
}

func Open(path string) (*Store, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)"} {
		q.Add("_pragma", pragma)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	st := &Store{db: db, path: path}
	if err = st.initialize(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func (st *Store) Close() error      { return st.db.Close() }
func (st *Store) Directory() string { return filepath.Dir(st.path) }

func (st *Store) initialize() error {
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("workspace database schema %d requires a newer daemon", version)
	}
	if _, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS meta (id INTEGER PRIMARY KEY CHECK(id=1), identity TEXT NOT NULL, revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS agents (id TEXT PRIMARY KEY, data BLOB NOT NULL, revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, data BLOB NOT NULL, revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS chats (id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE, data BLOB NOT NULL, revision INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS chats_agent ON chats(agent_id);
CREATE INDEX IF NOT EXISTS chats_project ON chats(project_id);
CREATE TABLE IF NOT EXISTS deleted (kind TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, PRIMARY KEY(kind,id));
CREATE TABLE IF NOT EXISTS project_operations (id TEXT PRIMARY KEY, path TEXT NOT NULL, status TEXT NOT NULL,
 updated_at TEXT NOT NULL, data BLOB NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS project_operation_destination ON project_operations(path)
 WHERE status IN ('queued','running','cancelling');
CREATE TABLE IF NOT EXISTS surface_records (kind TEXT NOT NULL, id TEXT NOT NULL, data BLOB NOT NULL,
 updated_at TEXT NOT NULL, PRIMARY KEY(kind,id));
CREATE TABLE IF NOT EXISTS git_operations (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, path TEXT NOT NULL,
 status TEXT NOT NULL, updated_at TEXT NOT NULL, data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS git_operations_project ON git_operations(project_id,updated_at);
CREATE TABLE IF NOT EXISTS native_chat_links (project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 agent_type TEXT NOT NULL, session_id TEXT NOT NULL, chat_id TEXT NOT NULL, hidden INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(project_id,agent_type,session_id));
CREATE INDEX IF NOT EXISTS native_chat_links_chat ON native_chat_links(chat_id);
PRAGMA user_version=5;`); err != nil {
		return err
	}
	identity := make([]byte, 16)
	if _, err = rand.Read(identity); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO meta VALUES (1,?,0)", hex.EncodeToString(identity)); err != nil {
		return err
	}
	if err = tx.QueryRow("SELECT identity FROM meta WHERE id=1").Scan(&st.identity); err != nil {
		return err
	}
	return tx.Commit()
}

func validKind(kind string) bool { return kind == "agents" || kind == "projects" || kind == "chats" }
func validID(id string) bool {
	if strings.TrimSpace(id) == "" || len(id) > 512 {
		return false
	}
	for _, c := range id {
		if c < 32 || c == 127 {
			return false
		}
	}
	return id != "." && id != ".."
}

func invalid(reason string) error { return fmt.Errorf("%w: %s", ErrInvalid, reason) }

// normalize validates before any write, and gives all records the database's canonical identity.
func (st *Store) normalize(kind, id string, data []byte, revision int64) ([]byte, string, string, error) {
	if !validKind(kind) || !validID(id) {
		return nil, "", "", invalid("kind or id")
	}
	var base Record
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, "", "", invalid("JSON body")
	}
	if base.ID != "" && base.ID != id {
		return nil, "", "", invalid("body id differs from path")
	}
	base.ID, base.WorkspaceID, base.Revision = id, st.identity, revision
	if base.CreatedAt == "" {
		base.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if _, err := time.Parse(time.RFC3339Nano, base.CreatedAt); err != nil {
		return nil, "", "", invalid("createdAt must be an ISO timestamp")
	}
	var value any
	var agentID, projectID string
	switch kind {
	case "agents":
		var row Agent
		if err := json.Unmarshal(data, &row); err != nil {
			return nil, "", "", invalid("agent")
		}
		row.Record = base
		row.Name = strings.TrimSpace(row.Name)
		if row.Name == "" || len(row.Name) > 512 || !validID(row.AgentType) || len(row.AgentTypeName) > 512 {
			return nil, "", "", invalid("agent name or harness")
		}
		value = row
	case "projects":
		var row Project
		if err := json.Unmarshal(data, &row); err != nil {
			return nil, "", "", invalid("project")
		}
		row.Record = base
		row.Name = strings.TrimSpace(row.Name)
		row.Path = strings.TrimSpace(row.Path)
		if row.IconPath != nil {
			if err := projecticon.ValidatePath(*row.IconPath); err != nil {
				return nil, "", "", invalid(err.Error())
			}
		}
		if row.GitConnection != nil {
			if err := row.GitConnection.Validate(); err != nil {
				return nil, "", "", invalid(err.Error())
			}
		}
		if row.Name == "" || len(row.Name) > 512 || row.Path == "" || len(row.Path) > 4096 || strings.ContainsRune(row.Path, 0) || len(row.RepoURL) > 4096 {
			return nil, "", "", invalid("project name, path or repository")
		}
		value = row
	case "chats":
		var row Chat
		if err := json.Unmarshal(data, &row); err != nil {
			return nil, "", "", invalid("chat")
		}
		row.Record = base
		row.Title = strings.TrimSpace(row.Title)
		if row.Title == "" {
			row.Title = "New chat"
		}
		if !validID(row.AgentID) || !validID(row.ProjectID) || len(row.Title) > 4096 || len(row.NativeTitle) > 4096 || len(row.SessionID) > 512 {
			return nil, "", "", invalid("chat parents or title")
		}
		if row.UpdatedAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, row.UpdatedAt); err != nil {
				return nil, "", "", invalid("updatedAt must be an ISO timestamp")
			}
		}
		agentID, projectID, value = row.AgentID, row.ProjectID, row
	}
	out, err := json.Marshal(value)
	return out, agentID, projectID, err
}

func record(tx *sql.Tx, kind, id string) ([]byte, int64, error) {
	if !validKind(kind) {
		return nil, 0, invalid("kind")
	}
	var data []byte
	var revision int64
	err := tx.QueryRow("SELECT data,revision FROM "+kind+" WHERE id=?", id).Scan(&data, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, nil
	}
	return data, revision, err
}

func isDeleted(tx *sql.Tx, kind, id string) (bool, error) {
	var n int
	err := tx.QueryRow("SELECT count(*) FROM deleted WHERE kind=? AND id=?", kind, id).Scan(&n)
	return n != 0, err
}

func (st *Store) write(fn func(*sql.Tx, int64) (bool, error)) error {
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRow("SELECT revision FROM meta WHERE id=1").Scan(&revision); err != nil {
		return err
	}
	changed, err := fn(tx, revision+1)
	if err != nil {
		return err
	}
	if changed {
		if _, err = tx.Exec("UPDATE meta SET revision=? WHERE id=1", revision+1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func save(tx *sql.Tx, kind, id string, data []byte, revision int64, agentID, projectID string) error {
	if kind == "chats" {
		_, err := tx.Exec("INSERT INTO chats VALUES (?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data,revision=excluded.revision", id, agentID, projectID, data, revision)
		return err
	}
	_, err := tx.Exec("INSERT INTO "+kind+" VALUES (?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data,revision=excluded.revision", id, data, revision)
	return err
}

// Put is create-only without expectedRevision, or a compare-and-swap update with it.
// A request retried after losing its acknowledgement is harmless when its value is unchanged.
func (st *Store) Put(kind, id string, data []byte, expected *int64) error {
	return st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		gone, err := isDeleted(tx, kind, id)
		if err != nil {
			return false, err
		}
		if gone {
			return false, ErrDeleted
		}
		old, oldRevision, err := record(tx, kind, id)
		if err != nil {
			return false, err
		}
		value, agentID, projectID, err := st.normalize(kind, id, data, revision)
		if err != nil {
			return false, err
		}
		if old != nil {
			// createdAt is immutable; compare values independently of their wire revision.
			var previous, incoming map[string]any
			_ = json.Unmarshal(old, &previous)
			_ = json.Unmarshal(value, &incoming)
			incoming["createdAt"] = previous["createdAt"]
			incoming["revision"] = previous["revision"]
			// Older clients do not know this optional field. Their unrelated metadata
			// edits must preserve a mute; clearing one requires an explicit false.
			if kind == "agents" || kind == "chats" {
				if _, present := incoming["notificationsMuted"]; !present {
					if muted, exists := previous["notificationsMuted"]; exists {
						incoming["notificationsMuted"] = muted
					}
				}
			}
			if kind == "chats" {
				// Native references/activity are daemon-owned. A client's rename or
				// notification edit must not clear them or roll them back.
				for _, key := range []string{"sessionId", "updatedAt", "nativeTitle"} {
					if v, exists := previous[key]; exists {
						incoming[key] = v
					}
				}
			}
			if kind == "projects" {
				// Old clients omit this field while renaming a project. Only an
				// explicit null clears the override and restores the workspace default.
				var raw map[string]json.RawMessage
				_ = json.Unmarshal(data, &raw)
				for _, key := range []string{"gitConnection", "iconPath"} {
					if _, supplied := raw[key]; !supplied {
						if value, exists := previous[key]; exists {
							incoming[key] = value
						}
					}
				}
			}
			same, _ := json.Marshal(incoming)
			canonical, _ := json.Marshal(previous)
			if string(same) == string(canonical) {
				return false, nil
			}
			if expected == nil || *expected != oldRevision {
				return false, ErrConflict
			}
			if kind == "agents" && incoming["agentType"] != previous["agentType"] {
				return false, invalid("an agent profile's harness cannot change")
			}
			if kind == "chats" && (incoming["agentId"] != previous["agentId"] || incoming["projectId"] != previous["projectId"]) {
				return false, invalid("chat parents cannot change")
			}
			incoming["revision"] = revision
			value, _ = json.Marshal(incoming)
		} else if expected != nil && *expected != 0 {
			return false, ErrConflict
		}
		if kind == "projects" {
			var p Project
			_ = json.Unmarshal(value, &p)
			canonical, err := workspacepath.Canonical(p.Path)
			if err != nil {
				return false, invalid(err.Error())
			}
			if _, err := activeAtPath(tx, canonical); err == nil {
				return false, fmt.Errorf("%w: a project operation owns this directory", ErrConflict)
			} else if err != nil && !errors.Is(err, ErrNotFound) {
				return false, err
			}
			// Legacy aliases retain their IDs; an unchanged directory can still be renamed.
			var previous Project
			_ = json.Unmarshal(old, &previous)
			oldPath, _ := workspacepath.Canonical(previous.Path)
			if other, err := projectAtPath(tx, canonical, id); err != nil {
				return false, err
			} else if other != nil && oldPath != canonical {
				return false, fmt.Errorf("%w: directory already belongs to project %s", ErrConflict, other.ID)
			}
		}
		if kind == "chats" {
			for parent, parentID := range map[string]string{"agents": agentID, "projects": projectID} {
				row, _, err := record(tx, parent, parentID)
				if err != nil {
					return false, err
				}
				if row == nil {
					return false, invalid("chat parent does not exist")
				}
				if parent == "projects" {
					if err := checkProjectRemoval(tx, row); err != nil {
						return false, err
					}
				}
			}
		}
		return true, save(tx, kind, id, value, revision, agentID, projectID)
	})
}

// Import only inserts missing legacy records. Existing values and tombstones always win.
// Parent-before-child insertion and the revision commit happen in one transaction.
func (st *Store) Import(batch Import) error {
	return st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		changed := false
		insert := func(kind, id string, value any) error {
			data, err := json.Marshal(value)
			if err != nil {
				return err
			}
			data, agentID, projectID, err := st.normalize(kind, id, data, revision)
			if err != nil {
				return err
			}
			old, _, err := record(tx, kind, id)
			if err != nil {
				return err
			}
			if old != nil {
				return nil
			}
			gone, err := isDeleted(tx, kind, id)
			if err != nil {
				return err
			}
			if gone {
				return nil
			}
			if kind == "projects" {
				if err := checkProjectRemoval(tx, data); err != nil {
					return err
				}
			}
			if kind == "chats" {
				for parent, parentID := range map[string]string{"agents": agentID, "projects": projectID} {
					row, _, err := record(tx, parent, parentID)
					if err != nil {
						return err
					}
					if row == nil {
						gone, err := isDeleted(tx, parent, parentID)
						if err != nil {
							return err
						}
						if gone {
							return nil
						}
						return invalid("legacy chat parent is missing")
					}
					if parent == "projects" {
						if err := checkProjectRemoval(tx, row); err != nil {
							return err
						}
					}
				}
			}
			if err = save(tx, kind, id, data, revision, agentID, projectID); err != nil {
				return err
			}
			changed = true
			return nil
		}
		for _, row := range batch.Agents {
			if err := insert("agents", row.ID, row); err != nil {
				return false, err
			}
		}
		for _, row := range batch.Projects {
			if err := insert("projects", row.ID, row); err != nil {
				return false, err
			}
		}
		for _, row := range batch.Chats {
			if err := insert("chats", row.ID, row); err != nil {
				return false, err
			}
		}
		return changed, nil
	})
}

// Delete removes registry membership (not files/native transcripts). Child chat links get durable
// tombstones in the same transaction. force is reserved for the existing DELETE /chats API.
func (st *Store) Delete(kind, id string, expected *int64, force bool, native ...*session.Store) error {
	if !validKind(kind) || !validID(id) {
		return invalid("kind or id")
	}
	var refs []session.ChatSession
	if len(native) > 0 && native[0] != nil {
		refs = native[0].ChatSessions()
	}
	return st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		if kind == "projects" {
			if op, err := operation(tx, id); err == nil && op.Active() {
				return false, ErrConflict
			} else if err != nil && !errors.Is(err, ErrNotFound) {
				return false, err
			}
			if data, _, err := record(tx, kind, id); err != nil {
				return false, err
			} else if data != nil {
				if err := checkProjectRemoval(tx, data); err != nil {
					return false, err
				}
			}
		}
		if err := rememberRemovedSessions(tx, kind, id, refs); err != nil {
			return false, err
		}
		return deleteRecord(tx, kind, id, expected, force, revision)
	})
}

// deleteRecord is shared by metadata deletion and the filesystem operation's commit.
func deleteRecord(tx *sql.Tx, kind, id string, expected *int64, force bool, revision int64) (bool, error) {
	gone, err := isDeleted(tx, kind, id)
	if err != nil {
		return false, err
	}
	if gone {
		if kind == "chats" {
			// A stale client can explicitly remove a conversation that a native
			// refresh already marked absent. Preserve that intent if it returns.
			_, err = tx.Exec("UPDATE native_chat_links SET hidden=1 WHERE chat_id=?", id)
			if err == nil {
				_, err = tx.Exec("DELETE FROM chats WHERE id=?", id)
			}
		}
		return false, err
	}
	_, oldRevision, err := record(tx, kind, id)
	if err != nil {
		return false, err
	}
	if oldRevision != 0 && !force && (expected == nil || *expected != oldRevision) {
		return false, ErrConflict
	}
	// Retain a suppression link when a chat/profile is explicitly removed, so a
	// native inventory refresh cannot resurrect it under another app ID. Removing
	// a project cascades its links; re-adding the folder can find its native chats.
	if kind == "chats" {
		if _, err = tx.Exec("UPDATE native_chat_links SET hidden=1 WHERE chat_id=?", id); err != nil {
			return false, err
		}
	} else if kind == "agents" {
		if _, err = tx.Exec("UPDATE native_chat_links SET hidden=1 WHERE chat_id IN (SELECT id FROM chats WHERE agent_id=?)", id); err != nil {
			return false, err
		}
	}
	if kind != "chats" {
		column := "agent_id"
		if kind == "projects" {
			column = "project_id"
		}
		if _, err = tx.Exec("INSERT OR REPLACE INTO deleted SELECT 'chats',id,? FROM chats WHERE "+column+"=?", revision, id); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec("DELETE FROM "+kind+" WHERE id=?", id); err != nil {
		return false, err
	}
	_, err = tx.Exec("INSERT INTO deleted VALUES (?,?,?)", kind, id, revision)
	return true, err
}

func (st *Store) Snapshot(since *int64) (Snapshot, error) {
	s := Snapshot{Version: Version, WorkspaceID: st.identity, Full: since == nil,
		Import: Import{Agents: []Agent{}, Projects: []Project{}, Chats: []Chat{}}, Deleted: []Deletion{}}
	tx, err := st.db.Begin()
	if err != nil {
		return s, err
	}
	defer tx.Rollback()
	if err = tx.QueryRow("SELECT revision FROM meta WHERE id=1").Scan(&s.Revision); err != nil {
		return s, err
	}
	minimum := int64(-1)
	if since != nil {
		minimum = *since
		if minimum < 0 || minimum > s.Revision {
			return s, ErrConflict
		}
	}
	for _, kind := range []string{"agents", "projects", "chats"} {
		query := "SELECT data FROM " + kind + " WHERE revision>?"
		if kind == "chats" {
			// A native session that disappeared may retain only its app
			// preferences for a later unarchive. Tombstones keep it out of every
			// public snapshot. Explicit deletion removes this cached row too.
			query += " AND NOT EXISTS (SELECT 1 FROM deleted WHERE kind='chats' AND deleted.id=chats.id)"
		}
		rows, err := tx.Query(query+" ORDER BY id", minimum)
		if err != nil {
			return s, err
		}
		for rows.Next() {
			var data []byte
			if err = rows.Scan(&data); err != nil {
				rows.Close()
				return s, err
			}
			switch kind {
			case "agents":
				var row Agent
				err = json.Unmarshal(data, &row)
				s.Agents = append(s.Agents, row)
			case "projects":
				var row Project
				err = json.Unmarshal(data, &row)
				s.Projects = append(s.Projects, row)
			case "chats":
				var row Chat
				err = json.Unmarshal(data, &row)
				s.Chats = append(s.Chats, row)
			}
			if err != nil {
				rows.Close()
				return s, err
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return s, err
		}
		rows.Close()
	}
	rows, err := tx.Query("SELECT kind,id,revision FROM deleted WHERE revision>? ORDER BY revision,kind,id", minimum)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var row Deletion
		if err = rows.Scan(&row.Kind, &row.ID, &row.Revision); err != nil {
			rows.Close()
			return s, err
		}
		s.Deleted = append(s.Deleted, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	return s, tx.Commit()
}

func (st *Store) ChatContext(id string) (*Chat, *Agent, *Project, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return nil, nil, nil, err
	}
	defer tx.Rollback()
	gone, err := isDeleted(tx, "chats", id)
	if err != nil {
		return nil, nil, nil, err
	}
	if gone {
		return nil, nil, nil, ErrDeleted
	}
	data, _, err := record(tx, "chats", id)
	if err != nil || data == nil {
		return nil, nil, nil, err
	}
	var chat Chat
	if err = json.Unmarshal(data, &chat); err != nil {
		return nil, nil, nil, err
	}
	data, _, err = record(tx, "agents", chat.AgentID)
	if err != nil {
		return nil, nil, nil, err
	}
	var agent Agent
	if err = json.Unmarshal(data, &agent); err != nil {
		return nil, nil, nil, err
	}
	data, _, err = record(tx, "projects", chat.ProjectID)
	if err != nil {
		return nil, nil, nil, err
	}
	var project Project
	if err = json.Unmarshal(data, &project); err != nil {
		return nil, nil, nil, err
	}
	if err = checkProjectRemoval(tx, data); err != nil {
		return nil, nil, nil, err
	}
	return &chat, &agent, &project, tx.Commit()
}

func (st *Store) RenameChat(id, title string) error {
	chat, _, _, err := st.ChatContext(id)
	if err != nil || chat == nil {
		return err
	}
	chat.Title, chat.TitleIsUserSet = strings.TrimSpace(title), strings.TrimSpace(title) != ""
	if !chat.TitleIsUserSet {
		chat.Title = chat.NativeTitle
	}
	data, err := json.Marshal(chat)
	if err != nil {
		return err
	}
	return st.Put("chats", id, data, &chat.Revision)
}
