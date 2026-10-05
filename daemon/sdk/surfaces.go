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
type SurfaceCursor = surface.Cursor
type SurfaceCapabilities = surface.Capabilities
type SurfaceBinding = surface.Binding
type LocalDesktopInfo = surface.LocalDesktopInfo
type LocalDesktopSettings = surface.LocalDesktopSettings
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
type Surfaces struct {
	c        *Client
	selected *surface.Service
}

func (s *Surfaces) desktop() *surface.Service {
	if s.selected != nil {
		return s.selected
	}
	return s.c.core.surfaces
}

// ForDesktop scopes status/events and receipts to a saved provider desktop.
func (s *Surfaces) ForDesktop(id string) (*Surfaces, error) {
	selected, err := s.c.core.surfaces.ForDesktop(id)
	if err != nil {
		return nil, surfaceAPIError("Surfaces.ForDesktop", err)
	}
	return &Surfaces{c: s.c, selected: selected}, nil
}

var surfaceUser = surface.Actor{Kind: "user", Name: "You"}

func (s *Surfaces) List() []SurfaceSnapshot { return []SurfaceSnapshot{s.desktop().Snapshot()} }
func (s *Surfaces) Status(ctx context.Context, refresh bool) (SurfaceSnapshot, error) {
	if refresh {
		snapshot, _ := s.desktop().Refresh(ctx)
		return snapshot, nil
	}
	return s.desktop().Snapshot(), nil
}
func (s *Surfaces) Bind(ctx context.Context, binding SurfaceBinding) (SurfaceSnapshot, error) {
	if s.selected != nil && s.selected.Snapshot().DesktopID != binding.DesktopID {
		return SurfaceSnapshot{}, surfaceAPIError("Surfaces.Bind", &surface.Error{Code: "workspace_mismatch", Message: "The desktop grant belongs to another saved desktop."})
	}
	if err := s.c.core.surfaces.Bind(binding, s.c.core.store); err != nil {
		return SurfaceSnapshot{}, surfaceAPIError("Surfaces.Bind", err)
	}
	selected, err := s.c.core.surfaces.ForDesktop(binding.DesktopID)
	if err != nil {
		return SurfaceSnapshot{}, surfaceAPIError("Surfaces.Bind", err)
	}
	snapshot, _ := selected.Refresh(ctx)
	return snapshot, nil
}
func (s *Surfaces) LocalStatus(ctx context.Context) (LocalDesktopInfo, error) {
	value, err := s.desktop().LocalDesktopInfo(ctx)
	if err != nil {
		if err = s.desktop().EnableLocalDesktop(s.c.core.store); err == nil {
			value, err = s.desktop().LocalDesktopInfo(ctx)
		}
	}
	return value, surfaceAPIError("Surfaces.LocalStatus", err)
}

// ConfigureLocal is an in-process owner operation. HTTP clients must instead
// supply the separate local control credential through the Mac CLI.
func (s *Surfaces) ConfigureLocal(ctx context.Context, settings LocalDesktopSettings) (SurfaceSnapshot, error) {
	if _, err := s.desktop().LocalDesktopInfo(ctx); err != nil {
		if err = s.desktop().EnableLocalDesktop(s.c.core.store); err != nil {
			return SurfaceSnapshot{}, surfaceAPIError("Surfaces.ConfigureLocal", err)
		}
	}
	value, err := s.desktop().ConfigureLocalDesktop(ctx, settings, s.c.core.store)
	return value, surfaceAPIError("Surfaces.ConfigureLocal", err)
}
func (s *Surfaces) Open(ctx context.Context, req SurfaceOpenRequest) (SurfaceSession, error) {
	value, err := s.desktop().Open(ctx, surfaceUser, req)
	return value, surfaceAPIError("Surfaces.Open", err)
}
func (s *Surfaces) Control(ctx context.Context, id string, req SurfaceControlRequest) (SurfaceSession, error) {
	value, err := s.desktop().Control(ctx, id, surfaceUser, req)
	return value, surfaceAPIError("Surfaces.Control", err)
}
func (s *Surfaces) Close(id string) error {
	return surfaceAPIError("Surfaces.Close", s.desktop().CloseSession(id, surfaceUser))
}
func (s *Surfaces) Capture(ctx context.Context, id string) (SurfaceCapture, error) {
	value, err := s.desktop().Capture(ctx, id, surfaceUser)
	return value, surfaceAPIError("Surfaces.Capture", err)
}
func (s *Surfaces) Action(ctx context.Context, req SurfaceActionRequest) (SurfaceReceipt, error) {
	value, err := s.desktop().Apply(ctx, surfaceUser, req)
	return value, surfaceAPIError("Surfaces.Action", err)
}
func (s *Surfaces) Receipt(id string) (SurfaceReceipt, error) {
	value, err := s.desktop().Receipt(id)
	return value, surfaceAPIError("Surfaces.Receipt", err)
}
func (s *Surfaces) Artifact(id string) (ArtifactContent, error) {
	value, err := s.desktop().Artifacts().Get(id)
	return value, surfaceAPIError("Surfaces.Artifact", err)
}
func (s *Surfaces) Changes() <-chan struct{} { return s.desktop().Changes() }

func surfaceAPIError(op string, err error) error {
	if err == nil {
		return nil
	}
	detail, status := surface.ErrorResponse(err)
	return &APIError{Op: op, Status: status, Message: detail.Message, Cause: err}
}

// Saved desktops are durable; control sessions and live leases remain ephemeral.
type DesktopRuntimeBinding = surface.RuntimeBinding
type DesktopResolution = surface.DesktopResolution
type SavedDesktop = surface.SavedDesktop
type DesktopCatalog = surface.DesktopCatalog
type DesktopResult = surface.DesktopResult
type CreateSavedDesktop = surface.CreateDesktopRequest
type UpdateSavedDesktop = surface.UpdateDesktopRequest

func (s *Surfaces) BindRuntime(binding DesktopRuntimeBinding) error {
	return surfaceAPIError("Surfaces.BindRuntime", s.c.core.surfaces.BindRuntime(binding, s.c.core.store))
}
func (s *Surfaces) ListDesktops(ctx context.Context, projectID string) (DesktopCatalog, error) {
	value, err := s.c.core.surfaces.ListDesktops(ctx, projectID)
	return value, surfaceAPIError("Surfaces.ListDesktops", err)
}
func (s *Surfaces) CreateDesktop(ctx context.Context, request CreateSavedDesktop) (SavedDesktop, error) {
	value, err := s.c.core.surfaces.CreateDesktop(ctx, request)
	return value, surfaceAPIError("Surfaces.CreateDesktop", err)
}
func (s *Surfaces) EnsureProjectDesktop(ctx context.Context, projectID string) (SavedDesktop, error) {
	value, err := s.c.core.surfaces.EnsureProjectDesktop(ctx, projectID)
	return value, surfaceAPIError("Surfaces.EnsureProjectDesktop", err)
}
func (s *Surfaces) desktopOperation(ctx context.Context, id, operation string, update *UpdateSavedDesktop) (DesktopResult, error) {
	value, err := s.c.core.surfaces.DesktopOperation(ctx, id, operation, update)
	return value, surfaceAPIError("Surfaces.DesktopOperation", err)
}
func (s *Surfaces) GetDesktop(ctx context.Context, id string) (DesktopResult, error) {
	return s.desktopOperation(ctx, id, "get", nil)
}
func (s *Surfaces) UpdateDesktop(ctx context.Context, id string, update UpdateSavedDesktop) (DesktopResult, error) {
	return s.desktopOperation(ctx, id, "update", &update)
}
func (s *Surfaces) StartDesktop(ctx context.Context, id string) (DesktopResult, error) {
	return s.desktopOperation(ctx, id, "start", nil)
}
func (s *Surfaces) StopDesktop(ctx context.Context, id string) (DesktopResult, error) {
	return s.desktopOperation(ctx, id, "stop", nil)
}
func (s *Surfaces) DeleteDesktop(ctx context.Context, id string) (DesktopResult, error) {
	return s.desktopOperation(ctx, id, "delete", nil)
}
