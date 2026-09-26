package registry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

// GitSpec is the non-secret intent and idempotency key. Path is the canonical
// repository root, resolved by the daemon rather than supplied by the client.
type GitSpec struct {
	ID             string       `json:"id"`
	ProjectID      string       `json:"projectId"`
	Path           string       `json:"path"`
	Action         string       `json:"action"`
	Paths          []string     `json:"paths,omitempty"`
	Message        string       `json:"message,omitempty"`
	Branch         string       `json:"branch,omitempty"`
	Remote         bool         `json:"remote,omitempty"`
	NewName        string       `json:"newName,omitempty"`
	ExpectedTip    string       `json:"expectedTip,omitempty"`
	Force          bool         `json:"force,omitempty"`
	Identity       *GitIdentity `json:"identity,omitempty"`
	CommitID       string       `json:"commitId,omitempty"`
	ExpectedHead   string       `json:"expectedHead,omitempty"`
	ExpectedBranch string       `json:"expectedBranch,omitempty"`
}

// GitIdentity is explicit, user-provided attribution for author settings.
// Legacy commit intents may also carry a repository author so retries retain it.
type GitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type GitOperation struct {
	GitSpec
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Sequence  int64  `json:"sequence"`
}

func (o GitOperation) Active() bool {
	return o.Status == "queued" || o.Status == "running" || o.Status == "cancelling"
}

func gitOperation(q queryer, id string) (GitOperation, error) {
	var data []byte
	if err := q.QueryRow("SELECT data FROM git_operations WHERE id=?", id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GitOperation{}, ErrNotFound
		}
		return GitOperation{}, err
	}
	var o GitOperation
	err := json.Unmarshal(data, &o)
	return o, err
}

func saveGitOperation(tx *sql.Tx, o *GitOperation) error {
	o.UpdatedAt = operationNow()
	o.Sequence++
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO git_operations(id,project_id,path,status,updated_at,data) VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,updated_at=excluded.updated_at,data=excluded.data`,
		o.ID, o.ProjectID, o.Path, o.Status, o.UpdatedAt, data)
	return err
}

func checkGitAtPath(q queryer, path string) error {
	rows, err := q.Query("SELECT path FROM git_operations WHERE status IN ('queued','running','cancelling')")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var active string
		if err := rows.Scan(&active); err != nil {
			return err
		}
		if workspacepath.Overlaps(active, path) {
			return fmt.Errorf("%w: a Git operation is using this repository", ErrConflict)
		}
	}
	return rows.Err()
}

func (st *Store) ReserveGitOperation(spec GitSpec) (GitOperation, bool, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return GitOperation{}, false, err
	}
	defer tx.Rollback()
	if old, err := gitOperation(tx, spec.ID); err == nil {
		if !reflect.DeepEqual(old.GitSpec, spec) {
			return old, false, ErrConflict
		}
		return old, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return GitOperation{}, false, err
	}
	if err := checkGitAtPath(tx, spec.Path); err != nil {
		return GitOperation{}, false, err
	}
	if _, err := conflictingOperation(tx, spec.Path, true, ""); err == nil {
		return GitOperation{}, false, fmt.Errorf("%w: a project operation is using this repository", ErrConflict)
	} else if !errors.Is(err, ErrNotFound) {
		return GitOperation{}, false, err
	}
	o := GitOperation{GitSpec: spec, Status: "queued", CreatedAt: operationNow()}
	if err := saveGitOperation(tx, &o); err != nil {
		return o, false, err
	}
	return o, true, tx.Commit()
}

func (st *Store) GitOperation(id string) (GitOperation, error) { return gitOperation(st.db, id) }

// Latest history is bounded for clients; active-only recovery never applies a limit.
func (st *Store) GitOperations(projectID string, activeOnly bool) ([]GitOperation, error) {
	query := "SELECT data FROM git_operations WHERE (?='' OR project_id=?)"
	if activeOnly {
		query += " AND status IN ('queued','running','cancelling')"
	}
	query += " ORDER BY updated_at DESC,id"
	if !activeOnly {
		query += " LIMIT 50"
	}
	rows, err := st.db.Query(query, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GitOperation{}
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var o GitOperation
		if err := json.Unmarshal(data, &o); err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

func (st *Store) ChangeGitOperation(id string, change func(*GitOperation)) (GitOperation, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return GitOperation{}, err
	}
	defer tx.Rollback()
	o, err := gitOperation(tx, id)
	if err != nil {
		return o, err
	}
	change(&o)
	if err := saveGitOperation(tx, &o); err != nil {
		return o, err
	}
	return o, tx.Commit()
}
