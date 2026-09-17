package mindwire

import (
	"context"
	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/surface"
)

type SurfaceSnapshot = surface.Snapshot
type SurfaceSession = surface.Session
type SurfaceController = surface.Controller
type SurfaceGeometry = surface.Geometry
type SurfaceCapabilities = surface.Capabilities
type SurfaceBinding = surface.Binding
type DesktopSSHConnection = surface.SSHConnection
type DesktopSSHSettings = surface.SSHSettings
type DesktopVNCSettings = surface.VNCSettings
type SurfaceOpenRequest = surface.OpenRequest
type SurfaceControlRequest = surface.ControlRequest
type SurfaceAction = surface.Action
type SurfaceActionRequest = surface.ActionRequest
type SurfaceReceipt = surface.Receipt
type SurfaceCapture = surface.Capture
type SurfaceError = surface.Error
type ArtifactContent = artifact.Content

// Surfaces is workspace scoped. All WithAgent views share the same controller.
type Surfaces struct{ c *Client }

var surfaceUser = surface.Actor{Kind: "user", Name: "You"}

func (s *Surfaces) List() []SurfaceSnapshot { return []SurfaceSnapshot{s.c.core.surfaces.Snapshot()} }
func (s *Surfaces) Status(ctx context.Context, refresh bool) (SurfaceSnapshot, error) {
	if refresh {
		snapshot, _ := s.c.core.surfaces.Refresh(ctx)
		return snapshot, nil
	}
	return s.c.core.surfaces.Snapshot(), nil
}
func (s *Surfaces) Bind(ctx context.Context, binding SurfaceBinding) (SurfaceSnapshot, error) {
	if err := s.c.core.surfaces.Bind(binding, s.c.core.store); err != nil {
		return SurfaceSnapshot{}, surfaceAPIError("Surfaces.Bind", err)
	}
	snapshot, _ := s.c.core.surfaces.Refresh(ctx)
	return snapshot, nil
}
func (s *Surfaces) Open(ctx context.Context, req SurfaceOpenRequest) (SurfaceSession, error) {
	value, err := s.c.core.surfaces.Open(ctx, surfaceUser, req)
	return value, surfaceAPIError("Surfaces.Open", err)
}
func (s *Surfaces) Control(ctx context.Context, id string, req SurfaceControlRequest) (SurfaceSession, error) {
	value, err := s.c.core.surfaces.Control(ctx, id, surfaceUser, req)
	return value, surfaceAPIError("Surfaces.Control", err)
}
func (s *Surfaces) Close(id string) error {
	return surfaceAPIError("Surfaces.Close", s.c.core.surfaces.CloseSession(id, surfaceUser))
}
func (s *Surfaces) Capture(ctx context.Context, id string) (SurfaceCapture, error) {
	value, err := s.c.core.surfaces.Capture(ctx, id, surfaceUser)
	return value, surfaceAPIError("Surfaces.Capture", err)
}
func (s *Surfaces) Action(ctx context.Context, req SurfaceActionRequest) (SurfaceReceipt, error) {
	value, err := s.c.core.surfaces.Apply(ctx, surfaceUser, req)
	return value, surfaceAPIError("Surfaces.Action", err)
}
func (s *Surfaces) Receipt(id string) (SurfaceReceipt, error) {
	value, err := s.c.core.surfaces.Receipt(id)
	return value, surfaceAPIError("Surfaces.Receipt", err)
}
func (s *Surfaces) Artifact(id string) (ArtifactContent, error) {
	value, err := s.c.core.surfaces.Artifacts().Get(id)
	return value, surfaceAPIError("Surfaces.Artifact", err)
}
func (s *Surfaces) Changes() <-chan struct{} { return s.c.core.surfaces.Changes() }

func surfaceAPIError(op string, err error) error {
	if err == nil {
		return nil
	}
	detail, status := surface.ErrorResponse(err)
	return &APIError{Op: op, Status: status, Message: detail.Message, Cause: err}
}
