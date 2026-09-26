package projectsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

type RunGuard interface {
	Busy(string) bool
	BusyPath(string) bool
}
type Service struct {
	reg      *registry.Store
	store    *session.Store
	runs     RunGuard
	gate     *sync.Mutex
	root     string
	porters  map[string]agent.SessionPorter
	discover func(context.Context, string) error
	mu       sync.Mutex
	active   map[string]context.CancelFunc
	closed   bool
	wg       sync.WaitGroup
	// Test fault seam after each durable path replacement, never enabled in production.
	afterWrite func(int) error
}

func New(reg *registry.Store, store *session.Store, runs RunGuard, gate *sync.Mutex, adapters []agent.Adapter, discover func(context.Context, string) error) (*Service, error) {
	s := &Service{reg: reg, store: store, runs: runs, gate: gate, root: filepath.Join(reg.Directory(), "project-sync"), porters: map[string]agent.SessionPorter{}, discover: discover, active: map[string]context.CancelFunc{}}
	if s.gate == nil {
		s.gate = &sync.Mutex{}
	}
	for _, a := range adapters {
		if p, ok := a.(agent.SessionPorter); ok {
			s.porters[a.ID()] = p
		}
	}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return nil, err
	}
	rows, err := s.reg.SyncList("operation")
	if err != nil {
		return nil, err
	}
	var recoveries []Operation
	for _, row := range rows {
		var o Operation
		if err = json.Unmarshal(row, &o); err != nil {
			return nil, err
		}
		if !o.Active() {
			if o.Status != "recovery_required" {
				_ = s.reg.ReleaseSync(o.ID)
			}
			continue
		}
		var j journal
		if err = s.reg.SyncGet("journal", o.ID, &j); err == nil {
			// The path reservation survives restarts until replay is acknowledged.
			recoveries = append(recoveries, o)
		} else if errors.Is(err, registry.ErrNotFound) {
			o.Status, o.Phase, o.Error = "failed", "interrupted", "Synchronization was interrupted before applying changes. Retry to resume."
			if err = s.save(o); err != nil {
				return nil, err
			}
			_ = s.reg.ReleaseSync(o.ID)
		} else {
			return nil, err
		}
	}
	// Validate all durable rows before launching workers. The workers remove
	// their active entry under this mutex, including when recovery finishes fast.
	s.mu.Lock()
	for _, o := range recoveries {
		s.launch(o, true)
	}
	s.mu.Unlock()
	return s, nil
}
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	for _, cancel := range s.active {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *Service) ActiveCount() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.active) }
func (s *Service) save(o Operation) error {
	o.UpdatedAt = now()
	return s.reg.SyncPut("operation", o.ID, o)
}
func (s *Service) Get(id string) (Operation, error) {
	var o Operation
	err := s.reg.SyncGet("operation", id, &o)
	return o, err
}
func (s *Service) List() ([]Operation, error) {
	rows, err := s.reg.SyncList("operation")
	if err != nil {
		return nil, err
	}
	out := []Operation{}
	for _, row := range rows {
		var o Operation
		if err = json.Unmarshal(row, &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *Service) Start(kind string, req Request) (Operation, error) {
	if ProtocolVersion() == 0 {
		return Operation{}, invalid("project synchronization currently requires macOS or Linux")
	}
	if kind != "export" && kind != "import" || !agent.ValidNativeSessionID(req.ID) || len(req.ID) > 128 {
		return Operation{}, invalid("invalid sync request")
	}
	if kind == "export" && (req.ProjectID == "" || req.CheckpointID != "") || kind == "import" && (!hashOK(req.CheckpointID) || req.ProjectID != "") {
		return Operation{}, invalid("select a source project or incoming checkpoint")
	}
	if kind == "import" {
		if _, err := s.Checkpoint(req.CheckpointID); err != nil {
			return Operation{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Operation{}, fmt.Errorf("synchronization service is closed")
	}
	if old, err := s.Get(req.ID); err == nil {
		if old.Kind != kind || !equal(old.Request, req) {
			return old, registry.ErrConflict
		}
		return old, nil
	} else if !errors.Is(err, registry.ErrNotFound) {
		return old, err
	}
	o := Operation{Request: req, Kind: kind, Status: "queued", Phase: "queued", CreatedAt: now(), UpdatedAt: now()}
	if err := s.save(o); err != nil {
		return o, err
	}
	s.launch(o, false)
	return o, nil
}
func (s *Service) Retry(id string) (Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Operation{}, fmt.Errorf("synchronization service is closed")
	}
	o, err := s.Get(id)
	if err != nil {
		return o, err
	}
	if o.Active() || o.Status == "succeeded" || s.active[id] != nil {
		return o, nil
	}
	var j journal
	journalErr := s.reg.SyncGet("journal", id, &j)
	if journalErr != nil && !errors.Is(journalErr, registry.ErrNotFound) {
		return o, journalErr
	}
	recovering := journalErr == nil
	o.Status, o.Phase, o.Error, o.Conflicts = "queued", "queued", "", nil
	if err = s.save(o); err != nil {
		return o, err
	}
	s.launch(o, recovering)
	return o, nil
}
func (s *Service) Cancel(id string) (Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.Get(id)
	if err != nil {
		return o, err
	}
	if o.Status == "applying" || o.Status == "recovery_required" {
		return o, fmt.Errorf("%w: this sync is applying a protected transaction; wait for it to finish", registry.ErrConflict)
	}
	if cancel := s.active[id]; cancel != nil {
		cancel()
	}
	return o, nil
}
func (s *Service) Wait(ctx context.Context, id string) (Operation, error) {
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		o, err := s.Get(id)
		if err != nil || !o.Active() {
			return o, err
		}
		select {
		case <-ctx.Done():
			return o, ctx.Err()
		case <-t.C:
		}
	}
}
func (s *Service) launch(o Operation, recovering bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	s.active[o.ID] = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.active, o.ID); s.mu.Unlock() }()
		var err error
		if recovering {
			err = s.applyJournal(ctx, &o)
		} else if o.Kind == "export" {
			err = s.export(ctx, &o)
		} else {
			err = s.importCheckpoint(ctx, &o)
		}
		if err != nil {
			var j journal
			if s.reg.SyncGet("journal", o.ID, &j) == nil {
				o.Status, o.Phase = "recovery_required", "recovery"
				o.Error = "Synchronization paused safely. Recovery data is retained. " + err.Error()
			} else {
				o.Status, o.Phase = "failed", "failed"
				o.Error = err.Error()
				_ = s.reg.ReleaseSync(o.ID)
			}
			_ = s.save(o)
		}
	}()
}
func (s *Service) reserve(o *Operation, p registry.Project) error {
	s.gate.Lock()
	defer s.gate.Unlock()
	if s.runs != nil && s.runs.BusyPath(p.Path) {
		return fmt.Errorf("finish or stop the running work in this project before switching workspaces")
	}
	snap, err := s.reg.Snapshot(nil)
	if err != nil {
		return err
	}
	for _, chat := range snap.Chats {
		if chat.ProjectID == p.ID && s.runs != nil && s.runs.Busy(chat.ID) {
			return fmt.Errorf("finish or stop the running chat before switching workspaces")
		}
	}
	return s.reg.ReserveSync(o.ID, p.Path)
}

func (s *Service) export(ctx context.Context, o *Operation) error {
	p, err := s.reg.Project(o.ProjectID)
	if err != nil {
		return err
	}
	if s.discover != nil {
		if err = s.discover(ctx, p.ID); err != nil {
			return err
		}
	}
	s.gate.Lock()
	err = s.reg.EnsureSyncIdentity(p.ID)
	s.gate.Unlock()
	if err != nil {
		return err
	}
	p, err = s.reg.Project(p.ID)
	if err != nil {
		return err
	}
	p.Path, err = workspacepath.Canonical(p.Path)
	if err != nil {
		return err
	}
	if err = nativeIdle(ctx, p.Path); err != nil {
		return err
	}
	if err = s.reserve(o, *p); err != nil {
		return err
	}
	defer s.reg.ReleaseSync(o.ID)
	o.Status, o.Phase = "exporting", "checkpoint"
	if err = s.save(*o); err != nil {
		return err
	}
	m, err := s.captureProject(ctx, *p)
	if err != nil {
		return err
	}
	var head string
	if err = s.reg.SyncGet("head", p.SyncID, &head); err != nil && !errors.Is(err, registry.ErrNotFound) {
		return err
	}
	m.Parents = []string{}
	if head != "" {
		m.Parents = append(m.Parents, head)
	}
	// Scan again before sealing; an external editor/CLI is not governed by the
	// daemon's lock. A moving source is retried instead of producing a torn copy.
	check, err := s.captureProject(ctx, *p)
	if err != nil {
		return err
	}
	if !sameSnapshot(m, check) {
		return fmt.Errorf("project changed during export; save the work and retry")
	}
	if err = nativeIdle(ctx, p.Path); err != nil {
		return err
	}
	if head != "" {
		prior, err := s.manifest(head)
		if err != nil {
			return err
		}
		if sameSnapshot(prior, m) {
			o.ResultCheckpointID = head
		} else {
			c, err := s.seal(ctx, m)
			if err != nil {
				return err
			}
			o.ResultCheckpointID = c.ID
		}
	} else {
		c, err := s.seal(ctx, m)
		if err != nil {
			return err
		}
		o.ResultCheckpointID = c.ID
	}
	if err = s.reg.SyncPut("head", p.SyncID, o.ResultCheckpointID); err != nil {
		return err
	}
	o.Status, o.Phase, o.Progress, o.ResultProjectID = "succeeded", "complete", 1, p.ID
	return s.save(*o)
}

func sameSnapshot(a, b Manifest) bool {
	return a.ProjectID == b.ProjectID && a.Name == b.Name && a.RepoURL == b.RepoURL && equal(a.IconPath, b.IconPath) && equal(a.Entries, b.Entries) && equal(a.Chats, b.Chats) && equal(a.Artifacts, b.Artifacts) && gitEqual(a.Git, b.Git)
}

func (s *Service) captureProject(ctx context.Context, p registry.Project) (Manifest, error) {
	m := Manifest{Version: Version, ProjectID: p.SyncID, Name: p.Name, RepoURL: p.RepoURL, IconPath: p.IconPath, CreatedAt: now(), Parents: []string{}, Chats: map[string]Chat{}}
	var err error
	m.Entries, err = s.scanFiles(ctx, p.Path)
	if err != nil {
		return m, err
	}
	m.Git, err = s.captureGit(ctx, p.Path, true)
	if err != nil {
		return m, err
	}
	snap, err := s.reg.Snapshot(nil)
	if err != nil {
		return m, err
	}
	profiles := map[string]registry.Agent{}
	for _, a := range snap.Agents {
		profiles[a.ID] = a
	}
	chatIDs := map[string]string{}
	for _, c := range snap.Chats {
		if c.ProjectID != p.ID {
			continue
		}
		profile := profiles[c.AgentID]
		porter := s.porters[profile.AgentType]
		if porter == nil {
			return m, invalid("native conversation export is not supported for " + profile.AgentType)
		}
		id := c.SyncID
		if id == "" {
			id = registry.SyncKey(snap.WorkspaceID, "chat", c.ID)
		}
		chatIDs[c.ID] = id
		sid := s.store.Session(profile.AgentType, c.ID)
		if sid == "" {
			sid = c.SessionID
		}
		chat := Chat{ID: id, Harness: profile.AgentType, ProfileName: profile.Name, Title: c.Title, NativeTitle: c.NativeTitle, TitleIsUserSet: c.TitleIsUserSet, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, SessionID: sid, ArchiveVersion: 1}
		if sid != "" {
			archive, err := porter.ExportSession(ctx, p.Path, sid)
			if err != nil {
				return m, err
			}
			if archive.Version != 1 {
				return m, invalid("unsupported native session archive version")
			}
			for _, file := range archive.Files {
				key := "native/" + profile.AgentType + "/" + file.Name
				var e *Entry
				var err error
				if file.Resource {
					key = "data/" + profile.AgentType + "/" + file.Name
					e, err = s.dataEntry(ctx, file.Data, p.Path)
				} else {
					e, err = s.capture(ctx, file.Path, p.Path, file.JSONLines)
				}
				if err != nil {
					return m, err
				}
				if e == nil {
					return m, fmt.Errorf("a native conversation artifact disappeared during export")
				}
				e.Mode = 0600
				if old, ok := m.Entries[key]; ok && !equal(old, *e) {
					return m, fmt.Errorf("project memory changed during export")
				}
				m.Entries[key] = *e
				chat.NativeFiles = append(chat.NativeFiles, key)
			}
		}
		sort.Strings(chat.NativeFiles)
		data, err := s.store.ExportChat(profile.AgentType, c.ID)
		if err != nil {
			return m, err
		}
		if err = portableAttachments(&data, p.Path); err != nil {
			return m, err
		}
		e, err := s.recordEntry(ctx, data, p.Path)
		if err != nil {
			return m, err
		}
		m.Entries["records/"+id] = e
		m.Chats[id] = chat
	}
	if err = s.captureArtifacts(ctx, &m, chatIDs); err != nil {
		return m, err
	}
	return m, s.validateManifest(m)
}
