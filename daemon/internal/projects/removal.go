package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

// RemoveRequest is explicit because ordinary registry deletion retains files.
// The path comes from the confirmed project record, never an arbitrary request path.
type RemoveRequest struct {
	OperationID      string `json:"operationId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func (s *Service) RemoveFiles(projectID string, req RemoveRequest) (registry.ProjectOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return registry.ProjectOperation{}, fmt.Errorf("project service is closed")
	}
	if old, err := s.store.ProjectOperation(req.OperationID); err == nil {
		if old.Source != "delete" || old.ProjectID != projectID || old.ExpectedRevision != req.ExpectedRevision {
			return old, registry.ErrConflict
		}
		return old, nil
	} else if !errors.Is(err, registry.ErrNotFound) {
		return registry.ProjectOperation{}, err
	}
	p, err := s.store.Project(projectID)
	if err != nil {
		return registry.ProjectOperation{}, err
	}
	path, err := workspacepath.Canonical(p.Path)
	if err != nil {
		return registry.ProjectOperation{}, invalid(err.Error())
	}
	if err := s.ensureRemovable(registry.ProjectOperation{ProjectSpec: registry.ProjectSpec{Path: path}, ProjectID: projectID}); err != nil {
		return registry.ProjectOperation{}, err
	}
	o, start, err := s.store.ReserveRemoval(req.OperationID, projectID, req.ExpectedRevision)
	if err == nil && start {
		s.launch(o, Auth{})
		s.notify()
	}
	return o, err
}

func (s *Service) ensureRemovable(o registry.ProjectOperation) error {
	path, err := workspacepath.Canonical(o.Path)
	if err != nil || path != o.Path {
		return invalid("project directory changed; refresh before removing it")
	}
	if filepath.Dir(path) == path {
		return invalid("the filesystem root cannot be deleted as a project")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, protected := range []string{home, s.store.Directory()} {
		canonical, err := workspacepath.Canonical(protected)
		if err != nil {
			return err
		}
		if workspacepath.Contains(path, canonical) {
			return invalid("this directory contains the home or daemon state and cannot be deleted as a project")
		}
	}
	if s.runs == nil {
		return nil
	}
	if s.runs.BusyPath(path) {
		return fmt.Errorf("%w: stop running chats in this directory before deleting it", registry.ErrConflict)
	}
	snapshot, err := s.store.Snapshot(nil)
	if err != nil {
		return err
	}
	for _, chat := range snapshot.Chats {
		if chat.ProjectID == o.ProjectID && s.runs.Busy(chat.ID) {
			return fmt.Errorf("%w: stop the running chat before deleting this project", registry.ErrConflict)
		}
	}
	return nil
}

// A sibling quarantine makes the filesystem/SQLite boundary recoverable. Before
// commit we can restore the directory. After commit we only touch this quarantine;
// a newly created folder at the original path is never removed by retry/recovery.
func (s *Service) runRemoval(ctx context.Context, o registry.ProjectOperation) (resultErr error) {
	committed := o.Phase == "deleting"
	defer func() {
		if resultErr == nil || committed {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := restoreRemoval(o); err != nil {
			resultErr = fmt.Errorf("%v; files retained at %s: %w", resultErr, filepath.Join(stagePath(o), "project"), err)
			return
		}
		_, _ = s.store.ChangeProjectOperation(o.ID, func(v *registry.ProjectOperation) error {
			if v.Phase == "deleting" {
				return registry.ErrConflict
			}
			v.Phase = "preparing" // no quarantined user files remain; release the reservation on failure
			return nil
		})
	}()
	err := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if committed {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.store.ValidateRemoval(o.ID); err != nil {
			return err
		}
		if err := s.ensureRemovable(o); err != nil {
			return err
		}
		stage := stagePath(o)
		if _, err := os.Lstat(stage); os.IsNotExist(err) {
			if _, err := prepareStage(o); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if err := ownedRemovalStage(o); err != nil {
			return err
		}
		if err := s.update(o.ID, "removing", nil, ""); err != nil {
			return err
		}
		quarantine := filepath.Join(stage, "project")
		if _, err := os.Lstat(quarantine); os.IsNotExist(err) {
			info, err := os.Lstat(o.Path)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return invalid("project directory changed; existing files were preserved")
				}
				if err := renameExclusive(o.Path, quarantine); err != nil {
					return err
				}
			}
		} else if err != nil {
			return err
		}
		if _, err := s.store.CommitRemoval(o.ID); err != nil {
			return err
		}
		committed = true
		s.notify()
		return nil
	}()
	if err != nil {
		return err
	}
	// Recursive deletion can take time. Do not hold the service lock: status and
	// reconnect remain responsive. The committed boundary cannot be cancelled.
	if err := cleanupRemoval(o); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.store.ChangeProjectOperation(o.ID, func(v *registry.ProjectOperation) error {
		v.Status, v.Phase, v.Error = "succeeded", "complete", ""
		progress := 1.0
		v.Progress = &progress
		return nil
	})
	return err
}

func ownedRemovalStage(o registry.ProjectOperation) error {
	info, err := os.Lstat(stagePath(o))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !markerMatches(stagePath(o), o) {
		return fmt.Errorf("removal staging ownership changed; files were preserved")
	}
	return nil
}

func restoreRemoval(o registry.ProjectOperation) error {
	stage := stagePath(o)
	if _, err := os.Lstat(stage); os.IsNotExist(err) {
		return nil
	}
	if err := ownedRemovalStage(o); err != nil {
		return err
	}
	quarantine := filepath.Join(stage, "project")
	if _, err := os.Lstat(quarantine); err == nil {
		if err := renameExclusive(quarantine, o.Path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return removeEmptyStage(o)
}

func removeEmptyStage(o registry.ProjectOperation) error {
	// Keep the ownership marker until the directory is empty. RemoveAll on the
	// wrapper could unlink its marker first and strand an interrupted cleanup.
	entries, err := os.ReadDir(stagePath(o))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != markerName(o) {
			return fmt.Errorf("unexpected files in removal staging directory; files were preserved")
		}
	}
	if err := os.Remove(filepath.Join(stagePath(o), markerName(o))); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Remove(stagePath(o))
}

func cleanupRemoval(o registry.ProjectOperation) error {
	stage := stagePath(o)
	if _, err := os.Lstat(stage); os.IsNotExist(err) {
		return nil
	}
	if err := ownedRemovalStage(o); err != nil {
		// A crash after removing the marker but before removing the wrapper leaves
		// an empty directory. os.Remove can clean that up without risking any files.
		if entries, readErr := os.ReadDir(stage); readErr == nil && len(entries) == 0 {
			return os.Remove(stage)
		}
		return err
	}
	if err := os.RemoveAll(filepath.Join(stage, "project")); err != nil {
		return err
	}
	return removeEmptyStage(o)
}
