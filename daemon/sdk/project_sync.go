package mindwire

import (
	"context"
	"errors"
	"fmt"

	"github.com/oblien/mindwire/daemon/internal/conversations"
	"github.com/oblien/mindwire/daemon/internal/projectsync"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

type ProjectSyncRequest = projectsync.Request
type ProjectSyncOperation = projectsync.Operation
type ProjectSyncCheckpoint = projectsync.Checkpoint
type ProjectSyncConflict = projectsync.Conflict
type ProjectSyncObjectPage = projectsync.ObjectPage
type ProjectSync struct{ c *Client }

func (co *core) Busy(id string) bool { return co.sup.Busy(id) }
func (co *core) BusyPath(path string) bool {
	return co.sup.BusyPath(path) || co.execution.BusyPath(path)
}

func (co *core) discoverForSync(ctx context.Context, id string) error {
	issues, err := co.conversations.Refresh(ctx, conversations.Query{ProjectID: id, Refresh: true})
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return fmt.Errorf("refresh native conversations before synchronizing: %s", issues[0].Message)
	}
	return nil
}
func (s *ProjectSync) Export(req ProjectSyncRequest) (ProjectSyncOperation, error) {
	v, e := s.c.core.projectSync.Start("export", req)
	return v, workspaceError("Workspace.Sync.Export", e)
}
func (s *ProjectSync) Import(req ProjectSyncRequest) (ProjectSyncOperation, error) {
	v, e := s.c.core.projectSync.Start("import", req)
	return v, workspaceError("Workspace.Sync.Import", e)
}
func (s *ProjectSync) Get(id string) (ProjectSyncOperation, error) {
	v, e := s.c.core.projectSync.Get(id)
	return v, workspaceError("Workspace.Sync.Get", e)
}
func (s *ProjectSync) List() ([]ProjectSyncOperation, error) {
	v, e := s.c.core.projectSync.List()
	return v, workspaceError("Workspace.Sync.List", e)
}
func (s *ProjectSync) Retry(id string) (ProjectSyncOperation, error) {
	v, e := s.c.core.projectSync.Retry(id)
	return v, workspaceError("Workspace.Sync.Retry", e)
}
func (s *ProjectSync) Cancel(id string) (ProjectSyncOperation, error) {
	v, e := s.c.core.projectSync.Cancel(id)
	return v, workspaceError("Workspace.Sync.Cancel", e)
}
func (s *ProjectSync) Wait(ctx context.Context, id string) (ProjectSyncOperation, error) {
	v, e := s.c.core.projectSync.Wait(ctx, id)
	return v, workspaceError("Workspace.Sync.Wait", e)
}
func (s *ProjectSync) Checkpoint(id string) (ProjectSyncCheckpoint, error) {
	v, e := s.c.core.projectSync.Checkpoint(id)
	return v, workspaceError("Workspace.Sync.Checkpoint", e)
}
func (s *ProjectSync) AcceptCheckpoint(ctx context.Context, c ProjectSyncCheckpoint) error {
	return workspaceError("Workspace.Sync.AcceptCheckpoint", s.c.core.projectSync.AcceptCheckpoint(ctx, c))
}
func (s *ProjectSync) Objects(id string, cursor int) (ProjectSyncObjectPage, error) {
	v, e := s.c.core.projectSync.Objects(id, cursor)
	return v, workspaceError("Workspace.Sync.Objects", e)
}
func (s *ProjectSync) Missing(ids []string) ([]string, error) {
	v, e := s.c.core.projectSync.Missing(ids)
	return v, workspaceError("Workspace.Sync.Missing", e)
}
func (s *ProjectSync) GetObject(id string) ([]byte, error) {
	v, e := s.c.core.projectSync.GetObject(id)
	return v, workspaceError("Workspace.Sync.GetObject", e)
}
func (s *ProjectSync) PutObject(id string, data []byte) error {
	return workspaceError("Workspace.Sync.PutObject", s.c.core.projectSync.PutObject(id, data))
}

// RelayCheckpoint transfers only missing immutable chunks, parents first. The
// caller may retry after losing connectivity; verified objects are reused.
func (s *ProjectSync) RelayCheckpoint(ctx context.Context, target *ProjectSync, id string, progress func(int64)) error {
	if _, err := target.Checkpoint(id); err == nil {
		return nil
	} else if !errors.Is(err, registry.ErrNotFound) {
		return err
	}
	c, err := s.Checkpoint(id)
	if err != nil {
		return err
	}
	for _, p := range c.Parents {
		if err = s.RelayCheckpoint(ctx, target, p, progress); err != nil {
			return err
		}
	}
	for cursor := 0; ; {
		if err = ctx.Err(); err != nil {
			return err
		}
		page, err := s.Objects(id, cursor)
		if err != nil {
			return err
		}
		missing, err := target.Missing(page.Objects)
		if err != nil {
			return err
		}
		for _, object := range missing {
			if err = ctx.Err(); err != nil {
				return err
			}
			b, err := s.GetObject(object)
			if err != nil {
				return err
			}
			if err = target.PutObject(object, b); err != nil {
				return err
			}
			if progress != nil {
				progress(int64(len(b)))
			}
		}
		if page.Next == 0 {
			break
		}
		cursor = page.Next
	}
	return target.AcceptCheckpoint(ctx, c)
}

// SwitchTo preserves the source and opens no chat or process. Stable request IDs
// make a lost acknowledgement safe. A conflict is returned as an operation so
// callers can show its paths and retained recovery checkpoint instead of hiding it.
func (s *ProjectSync) SwitchTo(ctx context.Context, target *ProjectSync, req ProjectSyncRequest, progress func(int64)) (ProjectSyncOperation, error) {
	if s.c.core == target.c.core {
		return ProjectSyncOperation{}, workspaceError("Workspace.Sync.SwitchTo", registry.ErrInvalid)
	}
	if accepted, err := target.Get(req.ID + "-import"); err == nil {
		if accepted.Path != req.Path {
			return accepted, workspaceError("Workspace.Sync.SwitchTo", registry.ErrConflict)
		}
		return target.Wait(ctx, accepted.ID)
	} else if !errors.Is(err, registry.ErrNotFound) {
		return accepted, err
	}
	o, err := s.Export(ProjectSyncRequest{ID: req.ID + "-export", ProjectID: req.ProjectID})
	if err != nil {
		return o, err
	}
	o, err = s.Wait(ctx, o.ID)
	if err != nil || o.Status != "succeeded" {
		return o, err
	}
	if err = s.RelayCheckpoint(ctx, target, o.ResultCheckpointID, progress); err != nil {
		return o, err
	}
	o, err = target.Import(ProjectSyncRequest{ID: req.ID + "-import", CheckpointID: o.ResultCheckpointID, Path: req.Path})
	if err != nil {
		return o, err
	}
	return target.Wait(ctx, o.ID)
}
