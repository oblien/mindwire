package registry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

func checkProjectRemoval(q queryer, data []byte) error {
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	path, err := workspacepath.Canonical(p.Path)
	if err != nil {
		return nil
	} // legacy metadata may reference an unavailable directory
	return checkRemovalAtPath(q, path)
}

func checkRemovalAtPath(q queryer, path string) error {
	if err := checkSyncAtPath(q, path, ""); err != nil {
		return err
	}
	if err := checkGitAtPath(q, path); err != nil {
		return err
	}
	o, err := conflictingOperation(q, path, false, "")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil && o.Source == "delete" {
		return fmt.Errorf("%w: this project directory is being removed", ErrConflict)
	}
	return nil
}

// CheckProjectPath also gates legacy/unregistered chat turns by their working directory.
// The transport serializes this check and turn start against reserving a removal.
func (st *Store) CheckProjectPath(path string) error {
	canonical, err := workspacepath.WorkingDirectory(path)
	if err != nil {
		return invalid(err.Error())
	}
	return checkRemovalAtPath(st.db, canonical)
}

func removalTarget(tx *sql.Tx, o ProjectOperation) error {
	data, revision, err := record(tx, "projects", o.ProjectID)
	if err != nil {
		return err
	}
	if data == nil {
		return ErrDeleted
	}
	if o.ExpectedRevision != revision {
		return ErrConflict
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	path, err := workspacepath.Canonical(p.Path)
	if err != nil || path != o.Path {
		return fmt.Errorf("%w: project directory changed", ErrConflict)
	}
	return nil
}

func removalReservation(tx *sql.Tx, o ProjectOperation) error {
	if err := checkSyncAtPath(tx, o.Path, ""); err != nil {
		return err
	}
	if err := checkGitAtPath(tx, o.Path); err != nil {
		return err
	}
	if _, err := conflictingOperation(tx, o.Path, true, o.ID); err == nil {
		return fmt.Errorf("%w: another project operation uses this directory", ErrConflict)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	rows, err := tx.Query("SELECT data FROM projects WHERE id<>?", o.ProjectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return err
		}
		var p Project
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		path, err := workspacepath.Canonical(p.Path)
		if err == nil && workspacepath.Overlaps(path, o.Path) {
			return fmt.Errorf("%w: another registered project uses this directory or a parent/child directory", ErrConflict)
		}
	}
	return rows.Err()
}

// ReserveRemoval binds a destructive request to a specific registered project and
// revision. A replay is resolved before reading that record, including after deletion.
func (st *Store) ReserveRemoval(id, projectID string, expected int64) (ProjectOperation, bool, error) {
	if !validID(id) || !validID(projectID) || expected <= 0 {
		return ProjectOperation{}, false, invalid("operation id and current project revision are required")
	}
	tx, err := st.db.Begin()
	if err != nil {
		return ProjectOperation{}, false, err
	}
	defer tx.Rollback()
	if old, err := operation(tx, id); err == nil {
		if old.Source != "delete" || old.ProjectID != projectID || old.ExpectedRevision != expected {
			return old, false, ErrConflict
		}
		return old, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ProjectOperation{}, false, err
	}
	data, revision, err := record(tx, "projects", projectID)
	if err != nil {
		return ProjectOperation{}, false, err
	}
	if data == nil {
		return ProjectOperation{}, false, ErrDeleted
	}
	if revision != expected {
		return ProjectOperation{}, false, ErrConflict
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return ProjectOperation{}, false, err
	}
	path, err := workspacepath.Canonical(p.Path)
	if err != nil {
		return ProjectOperation{}, false, invalid(err.Error())
	}
	o := ProjectOperation{
		ProjectSpec: ProjectSpec{ID: id, Source: "delete", Name: p.Name, Path: path, ExpectedRevision: expected},
		ProjectID:   projectID, Status: "queued", Phase: "queued", CreatedAt: operationNow(), Attempt: 1,
	}
	if old, err := conflictingOperation(tx, path, true, id); err == nil && old.Source == "delete" && old.ProjectID == projectID && old.ExpectedRevision == expected {
		return old, false, nil
	}
	if err := removalReservation(tx, o); err != nil {
		return o, false, err
	}
	if err := saveOperation(tx, &o); err != nil {
		return o, false, err
	}
	return o, true, tx.Commit()
}

func (st *Store) ValidateRemoval(id string) error {
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	o, err := operation(tx, id)
	if err != nil {
		return err
	}
	if o.Source != "delete" || !o.Active() || o.Status == "cancelling" {
		return ErrConflict
	}
	if err := removalTarget(tx, o); err != nil {
		return err
	}
	return removalReservation(tx, o)
}

// CommitRemoval records the irreversible boundary with the membership tombstones.
// From this point recovery only cleans the owned quarantine, never the original path.
func (st *Store) CommitRemoval(id string) (ProjectOperation, error) {
	var result ProjectOperation
	err := st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		o, err := operation(tx, id)
		if err != nil {
			return false, err
		}
		result = o
		if o.Source != "delete" {
			return false, ErrConflict
		}
		if o.Phase == "deleting" || o.Status == "succeeded" {
			return false, nil
		}
		if o.Status != "running" || o.Phase != "removing" {
			return false, ErrConflict
		}
		if err := removalTarget(tx, o); err != nil {
			return false, err
		}
		if err := removalReservation(tx, o); err != nil {
			return false, err
		}
		changed, err := deleteRecord(tx, "projects", o.ProjectID, &o.ExpectedRevision, false, revision)
		if err != nil {
			return false, err
		}
		o.Phase, o.Error = "deleting", ""
		if err := saveOperation(tx, &o); err != nil {
			return false, err
		}
		result = o
		return changed, nil
	})
	return result, err
}

func (st *Store) RetryRemoval(id string) (ProjectOperation, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return ProjectOperation{}, err
	}
	defer tx.Rollback()
	o, err := operation(tx, id)
	if err != nil {
		return o, err
	}
	if o.Source != "delete" || !o.Retryable() {
		return o, ErrConflict
	}
	if o.Phase != "deleting" {
		if err := removalTarget(tx, o); err != nil {
			return o, err
		}
		if err := removalReservation(tx, o); err != nil {
			return o, err
		}
	} else {
		if _, err := conflictingOperation(tx, o.Path, false, o.ID); err == nil {
			return o, ErrConflict
		} else if !errors.Is(err, ErrNotFound) {
			return o, err
		}
	}
	o.Status, o.Error = "queued", ""
	o.Attempt++
	if err := saveOperation(tx, &o); err != nil {
		return o, err
	}
	return o, tx.Commit()
}
