package surface

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

type captureRecord struct {
	Capture
	SessionID string `json:"sessionId"`
}

type savedReceipt struct {
	Receipt
	Fingerprint string `json:"fingerprint"`
}
type sessionState struct {
	Session
	authorized bool
	revoked    bool
	lastSeen   time.Time
}
type Service struct {
	mu        sync.Mutex
	operation sync.Mutex
	db        *registry.Store
	artifacts *artifact.Store
	provider  Provider
	snapshot  Snapshot
	sessions  map[string]*sessionState
	openIDs   map[string]string
	changed   chan struct{}
	stop      chan struct{}
	closeOnce sync.Once
	approval  Approval
	now       func() time.Time
	ipc       *runIPC
	lastPrune time.Time
}

func New(db *registry.Store) (*Service, error) {
	artifacts, err := artifact.New(db)
	if err != nil {
		return nil, err
	}
	s := &Service{db: db, artifacts: artifacts, sessions: map[string]*sessionState{},
		openIDs: map[string]string{}, changed: make(chan struct{}), stop: make(chan struct{}), now: time.Now}
	s.snapshot = Snapshot{ID: DesktopID, WorkspaceID: db.Identity(), Kind: "desktop", Provider: "oblien",
		Version: Version, InstanceID: newID(), State: "not_configured"}
	if err := s.pruneRecords(); err != nil {
		return nil, err
	}
	// A crash between dispatch and acknowledgement cannot be safely replayed.
	rows, err := db.SurfaceList("surface_receipt")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var rec savedReceipt
		if json.Unmarshal(row, &rec) == nil && rec.Status == "dispatching" {
			rec.Status = "outcome_unknown"
			rec.Error = &Error{"outcome_unknown", "The service restarted after input dispatch. Observe the desktop before trying another action."}
			if err := db.SurfacePut("surface_receipt", rec.ID, rec); err != nil {
				return nil, err
			}
		}
	}
	go s.reap()
	return s, nil
}

func newID() string {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(key[:])
}
func validRequest(id string) bool {
	return len(id) > 0 && len(id) <= 128 && strings.IndexFunc(id, func(r rune) bool { return r < 33 || r > 126 }) < 0
}
func (s *Service) Artifacts() *artifact.Store { return s.artifacts }
func (s *Service) SetApproval(fn Approval)    { s.mu.Lock(); s.approval = fn; s.mu.Unlock() }
func (s *Service) signalLocked() {
	s.snapshot.Revision++
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *Service) Changes() <-chan struct{} { s.mu.Lock(); defer s.mu.Unlock(); return s.changed }
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snapshot
	if out.Controller != nil {
		v := *out.Controller
		out.Controller = &v
	}
	if out.Geometry != nil {
		v := *out.Geometry
		out.Geometry = &v
	}
	return out
}
func (s *Service) Configure(p Provider) error {
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	old := s.provider
	s.provider = p
	if s.snapshot.Controller != nil {
		if session := s.sessions[s.snapshot.Controller.SessionID]; session != nil {
			session.revoked = true
			session.Controller = nil
		}
	}
	s.snapshot.Controller = nil
	s.snapshot.Geometry = nil
	s.snapshot.State = "disconnected"
	s.snapshot.Error = nil
	expires := p.ExpiresAt()
	s.snapshot.AuthorizationExpiresAt = &expires
	s.signalLocked()
	s.mu.Unlock()
	if old != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = old.Release(ctx)
		cancel()
		_ = old.Close()
	}
	return nil
}

func (s *Service) Refresh(ctx context.Context) (Snapshot, error) {
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	p := s.provider
	s.mu.Unlock()
	if p == nil {
		return s.Snapshot(), nil
	}
	status, err := p.Status(ctx)
	s.mu.Lock()
	if err == nil {
		at := s.now().UTC()
		s.snapshot.ProviderStatus = status
		s.snapshot.ObservedAt = &at
		s.snapshot.Error = nil
		switch {
		case !status.Supported:
			s.snapshot.State = "unsupported"
		case !status.Enabled:
			s.snapshot.State = "disabled"
		case !status.Available:
			s.snapshot.State = "preparing"
		case s.snapshot.Geometry == nil:
			s.snapshot.State = "ready"
		}
	} else {
		s.failLocked(err)
	}
	s.signalLocked()
	s.mu.Unlock()
	return s.Snapshot(), err
}

func (s *Service) Open(ctx context.Context, actor Actor, req OpenRequest) (Session, error) {
	if !validRequest(req.RequestID) || (req.Mode != "view" && req.Mode != "control") {
		return Session{}, problem("invalid_request", "Choose view or control and provide a request ID.")
	}
	if len(req.Name) > 160 {
		return Session{}, problem("invalid_request", "Session name is too long.")
	}
	s.operation.Lock()
	s.mu.Lock()
	key := actor.Kind + ":" + actor.RunID + ":" + req.RequestID
	if id := s.openIDs[key]; id != "" {
		old := s.sessions[id]
		if old != nil {
			out := old.Session
			if actor.Kind == "agent" && !old.authorized {
				s.mu.Unlock()
				s.operation.Unlock()
				return Session{}, problem("approval_pending", "Desktop access is still waiting for approval.")
			}
			if out.Controller != nil {
				c := *out.Controller
				out.Controller = &c
			}
			s.mu.Unlock()
			s.operation.Unlock()
			return out, nil
		}
	}
	if len(s.sessions) >= 32 {
		s.mu.Unlock()
		s.operation.Unlock()
		return Session{}, problem("session_limit", "Too many desktop sessions are open.")
	}
	p := s.provider
	s.mu.Unlock()
	if p == nil {
		s.operation.Unlock()
		return Session{}, problem("needs_authorization", "Open Desktop in this workspace to authorize its connection.")
	}
	status, err := p.Status(ctx)
	if err == nil && (!status.Supported || !status.Enabled || !status.Available) {
		switch {
		case !status.Supported:
			err = problem("unsupported", "This workspace does not provide a desktop.")
		case !status.Enabled:
			err = problem("disabled", "Enable desktop access in the workspace before connecting.")
		default:
			err = problem("preparing", "The workspace display is still starting.")
		}
	}
	var geometry Geometry
	if err == nil {
		geometry, err = p.Connect(ctx)
	}
	if err != nil {
		s.mu.Lock()
		s.failLocked(err)
		s.signalLocked()
		s.mu.Unlock()
		s.operation.Unlock()
		return Session{}, err
	}
	s.mu.Lock()
	at := s.now().UTC()
	name := strings.TrimSpace(req.Name)
	if actor.Kind == "agent" {
		name = actor.Name
	}
	if name == "" {
		name = "You"
	}
	session := &sessionState{Session: Session{ID: newID(), SurfaceID: DesktopID, Actor: actor.Kind, Name: name, ChatID: actor.ChatID, RunID: actor.RunID, Mode: "view", CreatedAt: at}, lastSeen: at}
	s.sessions[session.ID] = session
	s.openIDs[key] = session.ID
	s.snapshot.ProviderStatus = status
	s.snapshot.ObservedAt = &at
	s.snapshot.State = "connected"
	s.snapshot.Geometry = &geometry
	s.snapshot.Error = nil
	s.signalLocked()
	s.mu.Unlock()
	s.operation.Unlock()
	if err := s.authorize(ctx, session.ID, actor); err != nil {
		_ = s.CloseSession(session.ID, actor)
		return Session{}, err
	}
	if req.Mode == "control" {
		out, err := s.Control(ctx, session.ID, actor, ControlRequest{Action: "acquire"})
		if err != nil {
			_ = s.CloseSession(session.ID, actor)
		}
		return out, err
	}
	return s.sessionCopy(session.ID, actor)
}

func (s *Service) authorize(ctx context.Context, id string, actor Actor) error {
	s.mu.Lock()
	session, err := s.sessionLocked(id, actor)
	fn := s.approval
	if err != nil {
		s.mu.Unlock()
		return err
	}
	authorized := session.authorized
	s.mu.Unlock()
	if actor.Kind == "agent" && !authorized {
		if fn == nil {
			return problem("approval_required", "Desktop control needs permission for this agent run.")
		}
		if err := fn(ctx, actor, "Allow "+actor.Name+" to view and control this workspace desktop for this desktop session?"); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err = s.sessionLocked(id, actor)
	if err == nil {
		session.authorized = true
	}
	return err
}

func (s *Service) sessionLocked(id string, actor Actor) (*sessionState, error) {
	session := s.sessions[id]
	if session == nil {
		return nil, problem("session_expired", "This desktop session ended. Reconnect to continue.")
	}
	if actor.Kind == "agent" && (session.Actor != "agent" || session.RunID != actor.RunID) {
		return nil, problem("forbidden", "The desktop session belongs to a different run.")
	}
	if actor.Kind != "agent" && session.Actor == "agent" {
		return nil, problem("forbidden", "Use Take control to create your own desktop session.")
	}
	return session, nil
}

func (s *Service) Control(ctx context.Context, id string, actor Actor, req ControlRequest) (Session, error) {
	// Heartbeats never wait behind network input. A long paste must not expire
	// an otherwise connected user's lease.
	if req.Action == "renew" {
		s.mu.Lock()
		defer s.mu.Unlock()
		session, err := s.sessionLocked(id, actor)
		if err != nil {
			return Session{}, err
		}
		current := s.snapshot.Controller
		if session.Mode == "control" {
			if current == nil || current.SessionID != id || current.Generation != req.Generation {
				return Session{}, problem("control_lost", "Desktop control changed. Refresh its status.")
			}
			current.ExpiresAt = s.now().Add(15 * time.Second)
			v := *current
			session.Controller = &v
		}
		// A takeover can race the previous viewer's heartbeat. Renew its view
		// session and return the new mode; only input needs the old generation rejected.
		session.lastSeen = s.now()
		s.signalLocked()
		return session.Session, nil
	}

	if req.Action == "acquire" || req.Action == "takeover" {
		if req.Action == "takeover" && actor.Kind == "agent" {
			return Session{}, problem("forbidden", "Only a user can take control from another session.")
		}
		if err := s.authorize(ctx, id, actor); err != nil {
			return Session{}, err
		}
	}
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	session, err := s.sessionLocked(id, actor)
	if err != nil {
		s.mu.Unlock()
		return Session{}, err
	}
	current := s.snapshot.Controller
	now := s.now()
	switch req.Action {
	case "release":
		if current != nil && current.SessionID == id {
			if req.Generation != 0 && req.Generation != current.Generation {
				s.mu.Unlock()
				return Session{}, problem("control_lost", "The controller has changed.")
			}
			s.snapshot.Controller = nil
			session.Controller = nil
			session.Mode = "view"
			s.signalLocked()
			p := s.provider
			s.mu.Unlock()
			if p != nil {
				_ = p.Release(ctx)
			}
			s.mu.Lock()
		}
	case "acquire", "takeover":
		// A revoked agent needs another approved session. A human can explicitly
		// acquire again after the other controller leaves, without reopening the viewer.
		if session.revoked && session.Actor == "agent" {
			s.mu.Unlock()
			return Session{}, problem("control_taken", "A user took control. Release this session and request a new one before continuing.")
		}
		if current != nil && current.SessionID != id && req.Action != "takeover" {
			s.mu.Unlock()
			return Session{}, problem("control_busy", "Another session controls the desktop. Wait for it to release control.")
		}
		if current != nil && current.SessionID != id {
			if previous := s.sessions[current.SessionID]; previous != nil {
				previous.Controller = nil
				previous.Mode = "view"
				previous.revoked = true
			}
			s.snapshot.Controller = nil
			p := s.provider
			s.mu.Unlock()
			if p != nil {
				_ = p.Release(ctx)
			}
			s.mu.Lock()
		}
		if s.snapshot.Controller == nil {
			s.snapshot.Controller = &Controller{SessionID: id, Actor: session.Actor, Name: session.Name, RunID: session.RunID, ChatID: session.ChatID, Generation: s.snapshot.Revision + 1, ExpiresAt: now.Add(15 * time.Second)}
		}
		session.Mode = "control"
		session.revoked = false
	default:
		s.mu.Unlock()
		return Session{}, problem("invalid_request", "Unknown desktop control action.")
	}
	if c := s.snapshot.Controller; c != nil && c.SessionID == id {
		v := *c
		session.Controller = &v
	}
	session.lastSeen = now
	s.signalLocked()
	out := session.Session
	s.mu.Unlock()
	return out, nil
}

func (s *Service) CloseSession(id string, actor Actor) error { return s.closeSession(id, actor, false) }
func (s *Service) closeSession(id string, actor Actor, expiredOnly bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	session, err := s.sessionLocked(id, actor)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if expiredOnly && s.now().Sub(session.lastSeen) <= 20*time.Second {
		s.mu.Unlock()
		return nil
	}
	controller := s.snapshot.Controller != nil && s.snapshot.Controller.SessionID == id
	if controller {
		s.snapshot.Controller = nil
	}
	delete(s.sessions, id)
	for k, v := range s.openIDs {
		if v == id {
			delete(s.openIDs, k)
		}
	}
	p := s.provider
	empty := len(s.sessions) == 0
	if empty {
		s.snapshot.Geometry = nil
		s.snapshot.State = "disconnected"
	}
	session.Controller = nil
	s.signalLocked()
	s.mu.Unlock()
	if p != nil {
		if controller {
			_ = p.Release(ctx)
		}
		if empty {
			_ = p.Close()
		}
	}
	return nil
}
func (s *Service) CloseRun(runID string) {
	s.mu.Lock()
	ids := []string{}
	for id, session := range s.sessions {
		if session.RunID == runID {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.CloseSession(id, Actor{Kind: "agent", RunID: runID})
	}
}

func (s *Service) Capture(ctx context.Context, sessionID string, actor Actor) (Capture, error) {
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	session, err := s.sessionLocked(sessionID, actor)
	if err != nil {
		s.mu.Unlock()
		return Capture{}, err
	}
	if actor.Kind == "agent" && !session.authorized {
		s.mu.Unlock()
		return Capture{}, problem("approval_required", "Approve desktop access before capturing it.")
	}
	session.lastSeen = s.now()
	p := s.provider
	chatID := session.ChatID
	s.mu.Unlock()
	if p == nil {
		return Capture{}, problem("needs_authorization", "Desktop connection is not configured.")
	}
	img, geometry, err := p.Capture(ctx)
	if err != nil {
		s.mu.Lock()
		s.failLocked(err)
		s.signalLocked()
		s.mu.Unlock()
		return Capture{}, err
	}
	if err := s.pruneRecords(); err != nil {
		return Capture{}, err
	}
	rec, err := s.artifacts.SaveImage(chatID, img)
	if err != nil {
		return Capture{}, err
	}
	capture := Capture{ID: newID(), SurfaceID: DesktopID, ArtifactID: rec.ID, Mime: rec.Mime, Geometry: geometry, CreatedAt: s.now().UTC()}
	if err := s.db.SurfacePut("surface_capture", capture.ID, captureRecord{Capture: capture, SessionID: sessionID}); err != nil {
		_ = s.artifacts.Delete(rec.ID)
		return Capture{}, err
	}
	s.mu.Lock()
	s.snapshot.Geometry = &geometry
	s.snapshot.State = "connected"
	s.signalLocked()
	s.mu.Unlock()
	return capture, nil
}
func (s *Service) Apply(ctx context.Context, actor Actor, req ActionRequest) (Receipt, error) {
	if !validRequest(req.RequestID) {
		return Receipt{}, problem("invalid_request", "Provide a stable action request ID.")
	}
	if err := validateAction(req.Action); err != nil {
		return Receipt{}, err
	}
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	session, err := s.sessionLocked(req.SessionID, actor)
	if err != nil {
		s.mu.Unlock()
		return Receipt{}, err
	}
	s.mu.Unlock()
	encoded, _ := json.Marshal(req)
	sum := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(sum[:])
	var old savedReceipt
	if err := s.db.SurfaceGet("surface_receipt", req.RequestID, &old); err == nil {
		if old.Fingerprint != fingerprint {
			return Receipt{}, problem("request_conflict", "This request ID was already used for different input.")
		}
		return old.Receipt, nil
	} else if !errors.Is(err, registry.ErrNotFound) {
		return Receipt{}, err
	}
	s.mu.Lock()
	controller := s.snapshot.Controller
	if controller == nil || controller.SessionID != session.ID || controller.Generation != req.ControlGeneration || session.revoked {
		s.mu.Unlock()
		return Receipt{}, problem("control_lost", "This session no longer controls the desktop.")
	}
	if actor.Kind != "agent" && s.now().After(controller.ExpiresAt) {
		s.mu.Unlock()
		return Receipt{}, problem("control_lost", "Desktop control expired. Take control to continue.")
	}
	p := s.provider
	geometry := s.snapshot.Geometry
	if p == nil {
		s.mu.Unlock()
		return Receipt{}, problem("needs_authorization", "Desktop connection is not configured.")
	}
	session.lastSeen = s.now()
	s.mu.Unlock()
	if spatial(req.Action.Kind) {
		current, err := p.Connect(ctx)
		if err != nil {
			return Receipt{}, err
		}
		geometry = &current
		s.mu.Lock()
		if s.snapshot.Geometry == nil || *s.snapshot.Geometry != current {
			s.snapshot.Geometry = &current
			s.signalLocked()
		}
		s.mu.Unlock()
		if geometry == nil {
			return Receipt{}, problem("stale_frame", "Capture the desktop before sending pointer input.")
		}
		if actor.Kind == "agent" {
			var capture captureRecord
			if req.FrameID == "" || s.db.SurfaceGet("surface_capture", req.FrameID, &capture) != nil || s.now().Sub(capture.CreatedAt) > 30*time.Second || capture.Geometry != *geometry || capture.SessionID != req.SessionID {
				return Receipt{}, problem("stale_frame", "Capture the desktop again; the previous observation is missing, old or resized.")
			}
		} else if req.GeometryRevision != geometry.Revision {
			return Receipt{}, problem("stale_frame", "The display changed size. Wait for the current frame.")
		}
		if !pointValid(req.Action.X, req.Action.Y, *geometry) || (req.Action.Kind == "drag" && !pointValid(req.Action.ToX, req.Action.ToY, *geometry)) {
			return Receipt{}, problem("invalid_request", "Pointer coordinates are outside the current desktop.")
		}
	}
	if err := s.pruneRecords(); err != nil {
		return Receipt{}, err
	}
	count, err := s.db.SurfaceCount("surface_receipt")
	if err != nil {
		return Receipt{}, err
	}
	if count >= 100000 {
		return Receipt{}, problem("receipt_limit", "Desktop input storage is busy. Wait for older receipts to expire before sending more input.")
	}
	rec := savedReceipt{Receipt: Receipt{ID: req.RequestID, SessionID: req.SessionID, SurfaceID: DesktopID, Kind: req.Action.Kind, Status: "dispatching", CreatedAt: s.now().UTC()}, Fingerprint: fingerprint}
	if err := s.db.SurfacePut("surface_receipt", rec.ID, rec); err != nil {
		return Receipt{}, err
	}
	text, err := p.Apply(ctx, req.Action)
	if err != nil {
		rec.Status = "outcome_unknown"
		rec.Error = asError(err)
	} else {
		rec.Status = "dispatched"
	}
	if saveErr := s.db.SurfacePut("surface_receipt", rec.ID, rec); saveErr != nil {
		return Receipt{}, problem("outcome_unknown", "Input may have been sent, but its receipt could not be saved. Observe the desktop before sending again.")
	}
	s.mu.Lock()
	if err != nil {
		s.failLocked(err)
	}
	s.signalLocked()
	s.mu.Unlock()
	out := rec.Receipt
	out.Text = text
	return out, nil
}
func (s *Service) Receipt(id string) (Receipt, error) {
	var rec savedReceipt
	err := s.db.SurfaceGet("surface_receipt", id, &rec)
	return rec.Receipt, err
}
func (s *Service) failLocked(err error) {
	s.snapshot.Error = asError(err)
	s.snapshot.State = "disconnected"
	if s.snapshot.Error.Code == "needs_authorization" {
		s.snapshot.State = "needs_authorization"
	}
}
func asError(err error) *Error {
	var specific *Error
	if errors.As(err, &specific) {
		return specific
	}
	if errors.Is(err, registry.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return &Error{"not_found", "This desktop record is no longer available."}
	}
	if errors.Is(err, artifact.ErrStorageFull) {
		return &Error{"storage_full", artifact.ErrStorageFull.Error()}
	}
	if errors.Is(err, artifact.ErrImageLimit) {
		return &Error{"display_limit", artifact.ErrImageLimit.Error()}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{"interrupted", "Desktop operation was interrupted."}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{"timeout", "The desktop did not respond in time."}
	}
	return &Error{"unavailable", "The desktop connection is unavailable. Reconnect to continue."}
}
func (s *Service) Close() {
	s.closeOnce.Do(func() {
		close(s.stop)
		if s.ipc != nil {
			s.ipc.close()
		}
		s.operation.Lock()
		defer s.operation.Unlock()
		s.mu.Lock()
		p := s.provider
		s.provider = nil
		s.mu.Unlock()
		if p != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = p.Release(ctx)
			cancel()
			_ = p.Close()
		}
	})
}
func (s *Service) reap() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		var expired []Session
		for _, session := range s.sessions {
			if session.Actor == "agent" {
				if c := s.snapshot.Controller; c != nil && c.SessionID == session.ID {
					c.ExpiresAt = s.now().Add(15 * time.Second)
				}
				continue
			}
			if s.now().Sub(session.lastSeen) > 20*time.Second {
				expired = append(expired, session.Session)
			}
		}
		s.mu.Unlock()
		for _, session := range expired {
			_ = s.closeSession(session.ID, Actor{Kind: "user"}, true)
		}
	}
}
func pointValid(x, y *int, g Geometry) bool {
	return x != nil && y != nil && *x >= 0 && *y >= 0 && *x < g.Width && *y < g.Height
}
func spatial(kind string) bool {
	return kind == "pointer" || kind == "click" || kind == "drag" || kind == "scroll"
}
func validateAction(a Action) error {
	if a.TextMode != "" {
		if a.Kind != "text" || a.TextMode != "keyboard" || len(a.Text) > 4096 {
			return problem("invalid_request", "Keyboard text supports up to 4096 bytes on a text action.")
		}
		// Portable across Mac QEMU and Linux: control keys use a separate key action.
		for _, c := range a.Text {
			if c < 32 || c > 126 {
				return problem("invalid_request", "Use normal text input for Unicode, and key actions for Return or Tab.")
			}
		}
	}
	if len(a.Text) > MaxTextBytes {
		return problem("invalid_request", "Send up to one MiB of text at a time.")
	}
	switch a.Kind {
	case "pointer", "click", "drag", "scroll":
		if a.Button != "" && a.Button != "left" && a.Button != "middle" && a.Button != "right" {
			return problem("invalid_request", "Choose left, middle or right mouse button.")
		}
		if a.X == nil || a.Y == nil {
			return problem("invalid_request", "Pointer input requires x and y coordinates.")
		}
		if a.Buttons < 0 || a.Buttons > 7 || a.Count < 0 || a.Count > 3 || a.DeltaX < -50 || a.DeltaX > 50 || a.DeltaY < -50 || a.DeltaY > 50 {
			return problem("invalid_request", "Pointer input exceeds its limits.")
		}
	case "key":
		if len(a.Keys) == 0 || len(a.Keys) > 8 {
			return problem("invalid_request", "Provide one key or a chord of at most eight keys.")
		}
		for _, name := range a.Keys {
			if _, err := parseKey(name); err != nil {
				return err
			}
		}
	case "text", "clipboard_read", "clipboard_write", "release":
	default:
		return problem("invalid_request", fmt.Sprintf("Unsupported desktop action %q.", a.Kind))
	}
	return nil
}

func (s *Service) sessionCopy(id string, actor Actor) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.sessionLocked(id, actor)
	if err != nil {
		return Session{}, err
	}
	out := session.Session
	if out.Controller != nil {
		c := *out.Controller
		out.Controller = &c
	}
	return out, nil
}

// Called under operation (or during construction), at most once per minute.
// Receipts provide a 24-hour deduplication window; captures ground input for 30s.
func (s *Service) pruneRecords() error {
	now := s.now()
	if now.Sub(s.lastPrune) < time.Minute {
		return nil
	}
	if err := s.db.SurfacePruneBefore("surface_receipt", now.Add(-24*time.Hour)); err != nil {
		return err
	}
	if err := s.db.SurfacePruneBefore("surface_capture", now.Add(-5*time.Minute)); err != nil {
		return err
	}
	s.lastPrune = now
	return nil
}

// ErrorResponse is shared by HTTP and the embedded SDK.
func ErrorResponse(err error) (*Error, int) {
	detail := asError(err)
	status := 503
	switch detail.Code {
	case "invalid_request", "invalid_binding", "display_limit":
		status = 400
	case "forbidden", "denied":
		status = 403
	case "unsupported", "session_expired", "not_found":
		status = 404
	case "control_lost", "control_busy", "control_taken", "request_conflict", "workspace_mismatch", "stale_frame", "approval_pending":
		status = 409
	case "needs_authorization", "disabled", "preparing", "approval_required":
		status = 428
	case "session_limit", "receipt_limit":
		status = 429
	case "storage_full":
		status = 507
	}
	return detail, status
}
