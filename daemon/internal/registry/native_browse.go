package registry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

// NativeSessionLink exposes reference/suppression metadata to the read-only
// global browser. Messages remain exclusively in the harness's own storage.
type NativeSessionLink struct {
	ProjectID, AgentType, SessionID, ChatID string
	Hidden                                  bool
}

func (st *Store) NativeSessionLinks() ([]NativeSessionLink, error) {
	rows, err := st.db.Query("SELECT project_id,agent_type,session_id,chat_id,hidden FROM native_chat_links")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NativeSessionLink
	for rows.Next() {
		var link NativeSessionLink
		if err = rows.Scan(&link.ProjectID, &link.AgentType, &link.SessionID, &link.ChatID, &link.Hidden); err != nil {
			return nil, err
		}
		result = append(result, link)
	}
	return result, rows.Err()
}

// Uses the same canonical identity and operation reservations as project saves.
// This is a metadata attachment; opening history must not prepare Git, create a
// missing directory, or populate Projects just because someone browsed a list.
func (st *Store) projectForNativeSession(tx *sql.Tx, cwd string, revision int64) (Project, bool, error) {
	path, err := workspacepath.Canonical(cwd)
	if err != nil {
		return Project{}, false, invalid("conversation directory is invalid")
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return Project{}, false, fmt.Errorf("%w: this conversation's original folder no longer exists", ErrNotFound)
	}
	if err != nil || !info.IsDir() {
		return Project{}, false, invalid("this conversation's original folder is unavailable")
	}
	if err := checkRemovalAtPath(tx, path); err != nil {
		return Project{}, false, err
	}
	project, err := projectAtPath(tx, path, "")
	if err != nil {
		return Project{}, false, err
	}
	if project != nil {
		return *project, false, nil
	}
	if _, err := activeAtPath(tx, path); err == nil {
		return Project{}, false, fmt.Errorf("%w: a project operation owns this directory", ErrConflict)
	} else if !errors.Is(err, ErrNotFound) {
		return Project{}, false, err
	}
	p := Project{Record: Record{ID: nativeID(st.identity, "folder", path, strconv.FormatInt(revision, 10))}, Name: filepath.Base(path), Path: path}
	data, _ := json.Marshal(p)
	data, _, _, err = st.normalize("projects", p.ID, data, revision)
	if err != nil {
		return Project{}, false, err
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return Project{}, false, err
	}
	return p, true, save(tx, "projects", p.ID, data, revision, "", "")
}
