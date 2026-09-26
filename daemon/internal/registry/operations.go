package registry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

const ProjectOperationsVersion = 1

var ErrNotFound = errors.New("workspace record not found")

// ProjectSpec is the durable, non-secret intent. ID is also the idempotency key.
// Credentials are deliberately absent; retrying after a restart can supply fresh auth.
type ProjectSpec struct {
	ID               string                `json:"id"`
	Source           string                `json:"source"` // folder, create, clone, delete
	Name             string                `json:"name"`
	Path             string                `json:"path"`
	RepoURL          string                `json:"repoUrl,omitempty"`
	Branch           string                `json:"branch,omitempty"`
	AuthKind         string                `json:"authKind,omitempty"` // token, ssh, gh; empty uses native git auth
	GitConnection    *gitaccess.Connection `json:"gitConnection,omitempty"`
	ExpectedRevision int64                 `json:"expectedRevision,omitempty"` // deletion's confirmed project version
}

type ProjectOperation struct {
	ProjectSpec
	Status    string   `json:"status"` // queued, running, cancelling, succeeded, failed, cancelled, interrupted
	Phase     string   `json:"phase"`
	Progress  *float64 `json:"progress,omitempty"`
	Log       string   `json:"log,omitempty"` // bounded, redacted tail, never a command containing credentials
	Error     string   `json:"error,omitempty"`
	ProjectID string   `json:"projectId,omitempty"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
	Sequence  int64    `json:"sequence"`
	Attempt   int      `json:"attempt"`
}

func (o ProjectOperation) Active() bool {
	return o.Status == "queued" || o.Status == "running" || o.Status == "cancelling"
}
func (o ProjectOperation) Retryable() bool {
	return o.Status == "failed" || o.Status == "interrupted" || o.Status == "cancelled"
}
func operationNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type queryer interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}

func operation(q queryer, id string) (ProjectOperation, error) {
	var data []byte
	if err := q.QueryRow("SELECT data FROM project_operations WHERE id=?", id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectOperation{}, ErrNotFound
		}
		return ProjectOperation{}, err
	}
	var o ProjectOperation
	err := json.Unmarshal(data, &o)
	return o, err
}

func saveOperation(tx *sql.Tx, o *ProjectOperation) error {
	o.UpdatedAt = operationNow()
	o.Sequence++
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO project_operations(id,path,status,updated_at,data) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET path=excluded.path,status=excluded.status,updated_at=excluded.updated_at,data=excluded.data`, o.ID, o.Path, o.Status, o.UpdatedAt, data)
	return err
}

func activeAtPath(q queryer, path string) (ProjectOperation, error) {
	return conflictingOperation(q, path, false, "")
}

// Deleting a directory also reserves its descendants and ancestors. Failed,
// uncommitted removals retain that reservation until their quarantine is restored.
func conflictingOperation(q queryer, path string, removing bool, except string) (ProjectOperation, error) {
	rows, err := q.Query("SELECT data FROM project_operations WHERE id<>? AND (status IN ('queued','running','cancelling') OR json_extract(data,'$.phase')='removing')", except)
	if err != nil {
		return ProjectOperation{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return ProjectOperation{}, err
		}
		var o ProjectOperation
		if err := json.Unmarshal(data, &o); err != nil {
			return o, err
		}
		if o.Path == path || ((removing || o.Source == "delete") && workspacepath.Overlaps(o.Path, path)) {
			return o, nil
		}
	}
	if err := rows.Err(); err != nil {
		return ProjectOperation{}, err
	}
	return ProjectOperation{}, ErrNotFound
}

// Legacy records may have aliases (~ or symlinks). Preserve their IDs/history while
// checking actual directory identity before a new operation can create another record.
func projectAtPath(q queryer, path, except string) (*Project, error) {
	rows, err := q.Query("SELECT data FROM projects WHERE id<>? ORDER BY id", except)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var p Project
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		canonical, err := workspacepath.Canonical(p.Path)
		if err == nil && canonical == path {
			return &p, nil
		}
	}
	return nil, rows.Err()
}

func sameProjectIntent(a, b ProjectSpec) bool { a.ID = ""; b.ID = ""; return reflect.DeepEqual(a, b) }

// ReserveProject atomically resolves idempotency and claims the destination. The
// claim and final record use the same database, including across two HTTP clients.
func (st *Store) ReserveProject(spec ProjectSpec) (ProjectOperation, bool, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return ProjectOperation{}, false, err
	}
	defer tx.Rollback()
	if old, err := operation(tx, spec.ID); err == nil {
		if !sameProjectIntent(old.ProjectSpec, spec) {
			return old, false, ErrConflict
		}
		return old, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ProjectOperation{}, false, err
	}
	if gone, err := isDeleted(tx, "projects", spec.ID); err != nil {
		return ProjectOperation{}, false, err
	} else if gone {
		return ProjectOperation{}, false, ErrDeleted
	}
	if err := checkGitAtPath(tx, spec.Path); err != nil {
		return ProjectOperation{}, false, err
	}
	if err := checkSyncAtPath(tx, spec.Path, ""); err != nil {
		return ProjectOperation{}, false, err
	}
	if old, err := activeAtPath(tx, spec.Path); err == nil {
		if !sameProjectIntent(old.ProjectSpec, spec) {
			return old, false, fmt.Errorf("%w: another project operation owns this directory", ErrConflict)
		}
		return old, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ProjectOperation{}, false, err
	}
	o := ProjectOperation{ProjectSpec: spec, Status: "queued", Phase: "queued", CreatedAt: operationNow(), Attempt: 1}
	if p, err := projectAtPath(tx, spec.Path, ""); err != nil {
		return o, false, err
	} else if p != nil {
		if spec.Source == "clone" && p.RepoURL != spec.RepoURL {
			return o, false, fmt.Errorf("%w: this directory belongs to another project", ErrConflict)
		}
		o.Status, o.Phase, o.ProjectID = "succeeded", "complete", p.ID
	}
	if data, _, err := record(tx, "projects", spec.ID); err != nil {
		return o, false, err
	} else if data != nil && o.ProjectID != spec.ID {
		return o, false, ErrConflict
	}
	if err = saveOperation(tx, &o); err != nil {
		return o, false, err
	}
	return o, o.Active(), tx.Commit()
}

func (st *Store) ProjectOperation(id string) (ProjectOperation, error) { return operation(st.db, id) }

func (st *Store) ProjectOperations(activeOnly bool) ([]ProjectOperation, error) {
	query := "SELECT data FROM project_operations"
	if activeOnly {
		query += " WHERE status IN ('queued','running','cancelling')"
	} else {
		query += " WHERE status IN ('queued','running','cancelling') OR id IN (SELECT id FROM project_operations WHERE status NOT IN ('queued','running','cancelling') ORDER BY updated_at DESC LIMIT 100)"
	}
	query += " ORDER BY updated_at DESC"
	rows, err := st.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectOperation{}
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		var o ProjectOperation
		if err = json.Unmarshal(data, &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ChangeProjectOperation gives cancellation/progress/retries a compare-and-swap
// sequence. A stale worker can never overwrite a terminal result or a new attempt.
func (st *Store) ChangeProjectOperation(id string, change func(*ProjectOperation) error) (ProjectOperation, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return ProjectOperation{}, err
	}
	defer tx.Rollback()
	o, err := operation(tx, id)
	if err != nil {
		return o, err
	}
	if err = change(&o); err != nil {
		return o, err
	}
	if err = saveOperation(tx, &o); err != nil {
		return o, err
	}
	return o, tx.Commit()
}

func (st *Store) RetryProject(id string) (ProjectOperation, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return ProjectOperation{}, err
	}
	defer tx.Rollback()
	o, err := operation(tx, id)
	if err != nil {
		return o, err
	}
	if !o.Retryable() {
		return o, ErrConflict
	}
	if gone, err := isDeleted(tx, "projects", id); err != nil {
		return o, err
	} else if gone {
		return o, ErrDeleted
	}
	if _, err := activeAtPath(tx, o.Path); err == nil {
		return o, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return o, err
	}
	if p, err := projectAtPath(tx, o.Path, ""); err != nil {
		return o, err
	} else if p != nil {
		return o, ErrConflict
	}
	o.Status, o.Phase, o.Error, o.Log = "queued", "queued", "", ""
	o.Progress = nil
	o.Attempt++
	if err := saveOperation(tx, &o); err != nil {
		return o, err
	}
	return o, tx.Commit()
}

// CompleteProject commits BOTH the canonical project and terminal operation in one
// transaction. Files are reconciled separately by the service's owned staging marker.
func (st *Store) CompleteProject(id string) (ProjectOperation, error) {
	var result ProjectOperation
	err := st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		o, err := operation(tx, id)
		if err != nil {
			return false, err
		}
		result = o
		if o.Status == "succeeded" {
			return false, nil
		}
		if o.Status != "running" || o.Source == "delete" {
			return false, ErrConflict
		}
		if gone, err := isDeleted(tx, "projects", id); err != nil {
			return false, err
		} else if gone {
			return false, ErrDeleted
		}
		if p, err := projectAtPath(tx, o.Path, id); err != nil {
			return false, err
		} else if p != nil {
			return false, fmt.Errorf("%w: directory already registered", ErrConflict)
		}
		if old, _, err := record(tx, "projects", id); err != nil {
			return false, err
		} else if old != nil {
			return false, ErrConflict
		}
		p := Project{Record: Record{ID: id, CreatedAt: o.CreatedAt}, Name: o.Name, Path: o.Path, RepoURL: o.RepoURL, GitConnection: o.GitConnection}
		data, _ := json.Marshal(p)
		data, _, _, err = st.normalize("projects", id, data, revision)
		if err != nil {
			return false, err
		}
		if err = save(tx, "projects", id, data, revision, "", ""); err != nil {
			return false, err
		}
		o.Status, o.Phase, o.ProjectID, o.Error = "succeeded", "complete", id, ""
		progress := 1.0
		o.Progress = &progress
		if err = saveOperation(tx, &o); err != nil {
			return false, err
		}
		result = o
		return true, nil
	})
	return result, err
}

func (st *Store) Project(id string) (*Project, error) {
	var data []byte
	if err := st.db.QueryRow("SELECT data FROM projects WHERE id=?", id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var p Project
	err := json.Unmarshal(data, &p)
	return &p, err
}
