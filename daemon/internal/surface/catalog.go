package surface

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

const runtimeBindingKey = "#surface:oblien-runtime"

// RuntimeBinding is write-only and workspace-scoped. No account credential is
// stored in the daemon. It enables tools even before a human opens the viewer.
type RuntimeBinding struct {
	RegistryID   string    `json:"registryId"`
	WorkspaceID  string    `json:"workspaceId"`
	GatewayToken string    `json:"gatewayToken"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

type DesktopResolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}
type DesktopSessionCapabilities struct {
	Mode        string `json:"mode"`
	MaxSessions int    `json:"max_sessions"`
	CanCreate   bool   `json:"can_create"`
	Resolution  *struct {
		Min     DesktopResolution `json:"min"`
		Max     DesktopResolution `json:"max"`
		Default DesktopResolution `json:"default"`
	} `json:"resolution,omitempty"`
}
type SavedDesktop struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Resolution      *DesktopResolution `json:"resolution,omitempty"`
	State           string             `json:"state"`
	Managed         bool               `json:"managed"`
	Available       bool               `json:"available"`
	CanResize       *bool              `json:"can_resize,omitempty"`
	CanDelete       *bool              `json:"can_delete,omitempty"`
	DeletionPending *bool              `json:"deletion_pending,omitempty"`
	Mode            string             `json:"mode,omitempty"`
	CreatedAt       string             `json:"created_at,omitempty"`
	UpdatedAt       string             `json:"updated_at,omitempty"`
	Error           string             `json:"error,omitempty"`
}
type DesktopCatalog struct {
	Success      bool                       `json:"success"`
	Sessions     []SavedDesktop             `json:"sessions"`
	Capabilities DesktopSessionCapabilities `json:"capabilities"`
	ProjectID    string                     `json:"projectId,omitempty"`
}
type DesktopResult struct {
	Success bool         `json:"success"`
	Session SavedDesktop `json:"session"`
}
type CreateDesktopRequest struct {
	RequestID  string             `json:"requestId"`
	ProjectID  string             `json:"projectId,omitempty"`
	Name       string             `json:"name"`
	Resolution *DesktopResolution `json:"resolution,omitempty"`
}
type UpdateDesktopRequest struct {
	Name       *string            `json:"name,omitempty"`
	Resolution *DesktopResolution `json:"resolution,omitempty"`
}
type desktopCreation struct {
	Request    CreateDesktopRequest `json:"request"`
	Resolution *DesktopResolution   `json:"resolution,omitempty"`
	Result     *SavedDesktop        `json:"result,omitempty"`
}
type desktopLink struct {
	ProjectID string `json:"projectId"`
	DesktopID string `json:"desktopId"`
}

// The catalog has no polling worker. Only open controllers are instantiated,
// and the root Service's existing lease reaper also covers those controllers.
type desktopCatalog struct {
	// Management operations serialize creation/lifecycle requests. Existing
	// viewers use childrenMu so a slow stop/delete cannot stall other displays'
	// input, heartbeats or the shared lease reaper. Map/closed writes hold both.
	mu              sync.Mutex
	childrenMu      sync.Mutex
	binding         RuntimeBinding
	backend         *Oblien
	children        map[string]*Service
	closed          bool
	providerFactory func(string) (Provider, error) // integration-test seam
}

var desktopIDPattern = regexp.MustCompile(`^(console|ds_[a-f0-9]{16})$`)

func validDesktopID(id string) bool { return desktopIDPattern.MatchString(id) }

func runtimeExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

func (s *Service) BindRuntime(binding RuntimeBinding, credentials Credentials) error {
	if s.catalog == nil || binding.RegistryID != s.db.Identity() {
		return problem("workspace_mismatch", "Reload this workspace before authorizing its desktops.")
	}
	if binding.ExpiresAt.IsZero() {
		binding.ExpiresAt = runtimeExpiry(binding.GatewayToken)
	}
	if binding.ExpiresAt.IsZero() || binding.GatewayToken == "" {
		return problem("invalid_binding", "Provide the workspace runtime's expiring desktop authorization.")
	}
	p, err := NewOblien(Binding{RegistryID: binding.RegistryID, WorkspaceID: binding.WorkspaceID, GatewayToken: binding.GatewayToken, RuntimeExpiresAt: binding.ExpiresAt})
	if err != nil {
		return err
	}
	s.operation.Lock()
	defer s.operation.Unlock()
	if s.macFactory != nil {
		return problem("unsupported", "This computer uses its local desktop setup.")
	}
	s.mu.Lock()
	previous := s.provider
	s.mu.Unlock()
	if old, ok := previous.(*Oblien); ok && old.bindingSnapshot().WorkspaceID != binding.WorkspaceID {
		return problem("workspace_mismatch", "This service belongs to another Oblien workspace.")
	}
	c := s.catalog
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return problem("unavailable", "The desktop service stopped.")
	}
	if c.binding.WorkspaceID != "" && c.binding.WorkspaceID != binding.WorkspaceID {
		return problem("workspace_mismatch", "This service belongs to another Oblien workspace.")
	}
	if c.binding == binding {
		return nil
	}
	encoded, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	if err = credentials.Set(runtimeBindingKey, string(encoded)); err != nil {
		return err
	}
	c.binding = binding
	if c.backend != nil {
		p.base = c.backend.base
		p.http = c.backend.http
	}
	c.backend = p
	for _, child := range c.children {
		child.operation.Lock()
		child.mu.Lock()
		if provider, ok := child.provider.(*Oblien); ok {
			provider.mu.Lock()
			provider.binding.GatewayToken = binding.GatewayToken
			provider.binding.RuntimeExpiresAt = binding.ExpiresAt
			provider.mu.Unlock()
			child.snapshot.AuthorizationExpiresAt = &binding.ExpiresAt
			child.signalLocked()
		}
		child.mu.Unlock()
		child.operation.Unlock()
	}
	return nil
}

func (s *Service) HasDesktopCatalog() bool {
	if s.catalog == nil {
		return false
	}
	s.catalog.mu.Lock()
	defer s.catalog.mu.Unlock()
	return s.catalog.backend != nil && !s.catalog.closed
}
func (s *Service) desktopChildren() []*Service {
	if s.catalog == nil {
		return nil
	}
	s.catalog.childrenMu.Lock()
	defer s.catalog.childrenMu.Unlock()
	out := make([]*Service, 0, len(s.catalog.children))
	for _, child := range s.catalog.children {
		out = append(out, child)
	}
	return out
}

// ForDesktop selects a physical display. A Mindwire control session is a
// different ID, and can never be used to select or change the physical display.
func (s *Service) ForDesktop(id string) (*Service, error) {
	if id == "" || id == DesktopID {
		return s, nil
	}
	if !validDesktopID(id) {
		return nil, problem("invalid_request", "Invalid saved desktop ID.")
	}
	if s.catalog == nil {
		if s.surfaceID == id {
			return s, nil
		}
		return nil, problem("unsupported", "Select desktops on the workspace service.")
	}
	c := s.catalog
	c.childrenMu.Lock()
	if c.closed {
		c.childrenMu.Unlock()
		return nil, problem("unavailable", "The desktop service stopped.")
	}
	cached := c.children[id]
	c.childrenMu.Unlock()
	if cached != nil {
		return cached, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, problem("unavailable", "The desktop service stopped.")
	}
	if id == "console" {
		s.mu.Lock()
		legacyViewer := len(s.sessions) > 0
		s.mu.Unlock()
		if legacyViewer {
			return nil, problem("desktop_conflict", "Close the older desktop viewer before opening this saved console.")
		}
	}
	if child := c.children[id]; child != nil {
		return child, nil
	}
	if c.backend == nil {
		return nil, problem("needs_authorization", "Refresh the workspace to authorize its saved desktops.")
	}
	if len(c.children) >= 32 {
		return nil, problem("session_limit", "Close an unused desktop before opening another.")
	}
	binding := c.backend.bindingSnapshot()
	binding.DesktopID = id
	provider, err := NewOblien(binding)
	if err != nil {
		return nil, err
	}
	provider.base = c.backend.base
	provider.http = c.backend.http
	var p Provider = provider
	if c.providerFactory != nil {
		p, err = c.providerFactory(id)
		if err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	approval := s.approval
	s.mu.Unlock()
	child := &Service{db: s.db, artifacts: s.artifacts, surfaceID: id, provider: p, approval: approval,
		sessions: map[string]*sessionState{}, openIDs: map[string]string{}, viewers: map[string]io.ReadWriteCloser{}, changed: make(chan struct{}), stop: make(chan struct{}), now: s.now}
	child.snapshot = Snapshot{ID: id, DesktopID: id, WorkspaceID: s.db.Identity(), Kind: "desktop", Provider: "oblien", Version: Version, InstanceID: newID(), State: "disconnected"}
	expiry := p.ExpiresAt()
	if !expiry.IsZero() {
		child.snapshot.AuthorizationExpiresAt = &expiry
	}
	c.childrenMu.Lock()
	c.children[id] = child
	c.childrenMu.Unlock()
	return child, nil
}

func (s *Service) serviceForSession(id string, actor Actor) (*Service, error) {
	for _, candidate := range append([]*Service{s}, s.desktopChildren()...) {
		candidate.mu.Lock()
		_, exists := candidate.sessions[id]
		candidate.mu.Unlock()
		if exists {
			if _, err := candidate.sessionCopy(id, actor); err != nil {
				return nil, err
			}
			return candidate, nil
		}
	}
	return nil, problem("session_expired", "Open a new desktop control session.")
}

func (s *Service) projectForActor(actor Actor) (*registry.Project, error) {
	if actor.ProjectID != "" {
		return s.db.Project(actor.ProjectID)
	}
	if actor.ChatID != "" {
		_, _, project, err := s.db.ChatContext(actor.ChatID)
		if err == nil && project != nil {
			return project, nil
		}
	}
	if actor.CWD != "" {
		path := filepath.Clean(actor.CWD)
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
		}
		return s.db.ProjectAtPath(path)
	}
	return nil, registry.ErrNotFound
}

func (s *Service) desktopLinks(projectID string) (map[string]bool, error) {
	rows, err := s.db.SurfaceList("project_desktop_link")
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, row := range rows {
		var link desktopLink
		if err := json.Unmarshal(row, &link); err != nil {
			return nil, err
		}
		if link.ProjectID == projectID {
			ids[link.DesktopID] = true
		}
	}
	return ids, nil
}
func (s *Service) listDesktopsLocked(ctx context.Context, projectID string) (DesktopCatalog, error) {
	c := s.catalog
	if c == nil || c.backend == nil {
		return DesktopCatalog{}, problem("needs_authorization", "Refresh the workspace to authorize its desktops.")
	}
	if projectID != "" {
		if _, err := s.db.Project(projectID); err != nil {
			return DesktopCatalog{}, err
		}
	}
	var result DesktopCatalog
	if err := c.backend.json(ctx, "GET", "/desktop/sessions", nil, &result); err != nil {
		return result, err
	}
	if result.Capabilities.Mode != "virtual" && result.Capabilities.Mode != "console" {
		return result, problem("unsupported", "Update the workspace runtime to use saved desktops.")
	}
	if result.Sessions == nil {
		result.Sessions = []SavedDesktop{}
	}
	if projectID != "" {
		links, err := s.desktopLinks(projectID)
		if err != nil {
			return result, err
		}
		filtered := []SavedDesktop{}
		for _, item := range result.Sessions {
			if links[item.ID] {
				filtered = append(filtered, item)
			}
		}
		result.Sessions = filtered
		result.ProjectID = projectID
	}
	return result, nil
}
func (s *Service) ListDesktops(ctx context.Context, projectID string) (DesktopCatalog, error) {
	if s.catalog == nil {
		return DesktopCatalog{}, problem("unsupported", "Saved desktops are available on Oblien workspaces.")
	}
	s.catalog.mu.Lock()
	defer s.catalog.mu.Unlock()
	return s.listDesktopsLocked(ctx, projectID)
}

func (s *Service) createDesktopLocked(ctx context.Context, req CreateDesktopRequest, capabilities DesktopSessionCapabilities) (SavedDesktop, error) {
	if !validRequest(req.RequestID) || len(req.Name) > 80 || strings.TrimSpace(req.Name) == "" {
		return SavedDesktop{}, problem("invalid_request", "Use a desktop name of 1–80 bytes and a stable request ID.")
	}
	if req.ProjectID != "" {
		if _, err := s.db.Project(req.ProjectID); err != nil {
			return SavedDesktop{}, err
		}
	}
	var previous desktopCreation
	err := s.db.SurfaceGet("desktop_create", req.RequestID, &previous)
	if err == nil {
		a, _ := json.Marshal(req)
		b, _ := json.Marshal(previous.Request)
		if string(a) != string(b) {
			return SavedDesktop{}, problem("request_conflict", "This request ID already created a different desktop.")
		}
		if previous.Result != nil {
			return *previous.Result, nil
		}
	} else if !errors.Is(err, registry.ErrNotFound) {
		return SavedDesktop{}, err
	}
	if req.Resolution != nil {
		limits := capabilities.Resolution
		if capabilities.Mode == "console" || limits == nil {
			return SavedDesktop{}, problem("invalid_request", "This console uses the operating system's display size.")
		}
		if req.Resolution.Width < limits.Min.Width || req.Resolution.Height < limits.Min.Height || req.Resolution.Width > limits.Max.Width || req.Resolution.Height > limits.Max.Height {
			return SavedDesktop{}, problem("invalid_request", "Choose a resolution within this workspace's supported range.")
		}
	}
	resolution := req.Resolution
	if resolution == nil && capabilities.Mode == "virtual" && capabilities.Resolution != nil {
		resolution = &capabilities.Resolution.Default
	}
	if err == nil {
		resolution = previous.Resolution
	}
	if err := s.db.SurfacePut("desktop_create", req.RequestID, desktopCreation{Request: req, Resolution: resolution}); err != nil {
		return SavedDesktop{}, err
	}
	sum := sha256.Sum256([]byte(s.db.Identity() + ":" + req.RequestID))
	body := struct {
		Name       string             `json:"name"`
		Resolution *DesktopResolution `json:"resolution,omitempty"`
		Key        string             `json:"idempotency_key"`
		// The runtime accepts at most 64 bytes for an idempotency key. The hash
		// already includes the workspace identity, so no additional prefix is needed.
	}{req.Name, resolution, hex.EncodeToString(sum[:])}
	var result DesktopResult
	if err := s.catalog.backend.json(ctx, "POST", "/desktop/sessions", body, &result); err != nil {
		return SavedDesktop{}, err
	}
	if !result.Success || !validDesktopID(result.Session.ID) {
		return SavedDesktop{}, problem("unavailable", "The provider did not return a saved desktop ID. Retry this same request.")
	}
	if req.ProjectID != "" {
		if err := s.db.SurfacePut("project_desktop_link", req.ProjectID+":"+result.Session.ID, desktopLink{req.ProjectID, result.Session.ID}); err != nil {
			return SavedDesktop{}, err
		}
	}
	if err := s.db.SurfacePut("desktop_create", req.RequestID, desktopCreation{Request: req, Resolution: resolution, Result: &result.Session}); err != nil {
		return SavedDesktop{}, err
	}
	return result.Session, nil
}
func (s *Service) CreateDesktop(ctx context.Context, req CreateDesktopRequest) (SavedDesktop, error) {
	if s.catalog == nil {
		return SavedDesktop{}, problem("unsupported", "Saved desktops are available on Oblien workspaces.")
	}
	s.catalog.mu.Lock()
	defer s.catalog.mu.Unlock()
	list, err := s.listDesktopsLocked(ctx, "")
	if err != nil {
		return SavedDesktop{}, err
	}
	return s.createDesktopLocked(ctx, req, list.Capabilities)
}

// EnsureProjectDesktop is atomic across callers and durable across a daemon
// restart. An uncertain create always reuses its saved request and provider key.
func (s *Service) EnsureProjectDesktop(ctx context.Context, projectID string) (SavedDesktop, error) {
	if s.catalog == nil {
		return SavedDesktop{}, problem("unsupported", "Saved desktops are available on Oblien workspaces.")
	}
	s.catalog.mu.Lock()
	defer s.catalog.mu.Unlock()
	project, err := s.db.Project(projectID)
	if err != nil {
		return SavedDesktop{}, err
	}
	if s.catalog.backend == nil {
		return SavedDesktop{}, problem("needs_authorization", "Refresh the workspace desktop authorization.")
	}
	var status ProviderStatus
	if err := s.catalog.backend.json(ctx, "GET", "/desktop/status", nil, &status); err != nil {
		return SavedDesktop{}, err
	}
	if !status.Supported {
		return SavedDesktop{}, problem("desktop_setup_required", "Install a desktop in the workspace's Desktop settings first.")
	}
	if !status.Enabled {
		if err := s.catalog.backend.json(ctx, "POST", "/desktop/enable", nil, nil); err != nil {
			return SavedDesktop{}, err
		}
	}
	list, err := s.listDesktopsLocked(ctx, projectID)
	if err != nil {
		return SavedDesktop{}, err
	}
	for _, item := range list.Sessions {
		if item.State != "deleting" {
			return s.startDesktopLocked(ctx, item)
		}
	}
	if list.Capabilities.Mode == "console" {
		all, err := s.listDesktopsLocked(ctx, "")
		if err != nil {
			return SavedDesktop{}, err
		}
		for _, item := range all.Sessions {
			if item.ID == "console" && item.State != "deleting" {
				if err := s.db.SurfacePut("project_desktop_link", projectID+":"+item.ID, desktopLink{projectID, item.ID}); err != nil {
					return SavedDesktop{}, err
				}
				return s.startDesktopLocked(ctx, item)
			}
		}
	}
	var req CreateDesktopRequest
	err = s.db.SurfaceGet("project_desktop_default", projectID, &req)
	if err != nil && !errors.Is(err, registry.ErrNotFound) {
		return SavedDesktop{}, err
	}
	if req.RequestID != "" {
		var old desktopCreation
		if err := s.db.SurfaceGet("desktop_create", req.RequestID, &old); err == nil && old.Result != nil {
			req = CreateDesktopRequest{}
		}
	}
	if req.RequestID == "" {
		req = CreateDesktopRequest{RequestID: newID(), ProjectID: projectID, Name: project.Name}
		if strings.TrimSpace(req.Name) == "" {
			req.Name = filepath.Base(project.Path)
		}
		if len(req.Name) > 80 {
			req.Name = "Project desktop"
		}
		if err := s.db.SurfacePut("project_desktop_default", projectID, req); err != nil {
			return SavedDesktop{}, err
		}
	}
	return s.createDesktopLocked(ctx, req, list.Capabilities)
}

func (s *Service) startDesktopLocked(ctx context.Context, item SavedDesktop) (SavedDesktop, error) {
	if item.State != "stopped" && item.State != "failed" {
		return item, nil
	}
	var result DesktopResult
	err := s.catalog.backend.json(ctx, "POST", "/desktop/sessions/"+item.ID+"/start", nil, &result)
	return result.Session, err
}
func (s *Service) DesktopOperation(ctx context.Context, id, operation string, update *UpdateDesktopRequest) (DesktopResult, error) {
	if !validDesktopID(id) {
		return DesktopResult{}, problem("invalid_request", "Invalid saved desktop ID.")
	}
	if s.catalog == nil {
		return DesktopResult{}, problem("unsupported", "Saved desktops are available on Oblien workspaces.")
	}
	c := s.catalog
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.backend == nil {
		return DesktopResult{}, problem("needs_authorization", "Refresh the workspace desktop authorization.")
	}
	method, path := "GET", "/desktop/sessions/"+id
	var body any
	switch operation {
	case "get":
	case "update":
		if update == nil || (update.Name != nil && (strings.TrimSpace(*update.Name) == "" || len(*update.Name) > 80)) {
			return DesktopResult{}, problem("invalid_request", "Use a desktop name of 1–80 bytes.")
		}
		method = "PATCH"
		body = update
	case "start", "stop":
		method = "POST"
		path += "/" + operation
	case "delete":
		// Oblien requires virtual desktops to be stopped before deleting their
		// saved profile. Keep the user-confirmed operation in the shared service
		// so live agents and viewers lose their leases as soon as stop succeeds.
		if id != "console" {
			cleanup, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			var current DesktopResult
			if err := c.backend.json(cleanup, "GET", path, nil, &current); err != nil {
				if desktopWasDeleted(err) {
					c.retireDesktop(id)
					return DesktopResult{Success: true}, nil
				}
				return current, err
			}
			if current.Session.State == "deleting" || (current.Session.DeletionPending != nil && *current.Session.DeletionPending) {
				return current, nil
			}
			if current.Session.CanDelete != nil && !*current.Session.CanDelete {
				return DesktopResult{}, problem("invalid_request", "This workspace does not allow deleting that desktop.")
			}
			for current.Session.State == "starting" || current.Session.State == "stopping" {
				if err := c.waitDesktop(cleanup, path, &current); err != nil {
					return current, err
				}
			}
			if current.Session.State != "stopped" {
				if err := c.backend.json(cleanup, "POST", path+"/stop", nil, &current); err != nil {
					return current, err
				}
				c.retireDesktop(id)
				for current.Session.State != "stopped" {
					if err := c.waitDesktop(cleanup, path, &current); err != nil {
						return current, err
					}
				}
			}
		}
		method = "DELETE"
	default:
		return DesktopResult{}, problem("invalid_request", "Unknown desktop operation.")
	}
	var result DesktopResult
	if err := c.backend.json(ctx, method, path, body, &result); err != nil {
		if operation == "delete" && desktopWasDeleted(err) {
			c.retireDesktop(id)
			return DesktopResult{Success: true}, nil
		}
		return result, err
	}
	if operation == "stop" || operation == "delete" {
		c.retireDesktop(id)
	}
	return result, nil
}

func desktopWasDeleted(err error) bool {
	var detail *Error
	return errors.As(err, &detail) && detail.Code == "desktop_not_found"
}

// Caller holds the catalog lock; these helpers never create a background poller.
func (c *desktopCatalog) waitDesktop(ctx context.Context, path string, out *DesktopResult) error {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return problem("preparing", "The desktop is still stopping. Retry Delete when it has stopped.")
	case <-timer.C:
	}
	return c.backend.json(ctx, "GET", path, nil, out)
}

func (c *desktopCatalog) retireDesktop(id string) {
	if child := c.children[id]; child != nil {
		c.childrenMu.Lock()
		delete(c.children, id)
		c.childrenMu.Unlock()
		child.Close()
		child.mu.Lock()
		child.snapshot.State = "disconnected"
		child.snapshot.Controller = nil
		child.sessions = map[string]*sessionState{}
		child.signalLocked()
		child.mu.Unlock()
	}
}
