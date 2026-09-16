// Package projects owns project workflows independently of any client connection.
// Its registry is the same SQLite database used by every workspace API and SDK.
package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

const maxDuration = 60 * time.Minute

// Auth is write-only, held in memory for an attempt. It is never part of a
// snapshot, operation record, command argument, or the repository's origin URL.
type Auth struct {
	Kind       string `json:"kind,omitempty"`
	Username   string `json:"username,omitempty"`
	Token      string `json:"token,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
}

type Request struct {
	registry.ProjectSpec
	Auth Auth `json:"auth,omitempty"`
}

// RunGuard is implemented by the existing supervisor, including legacy chats
// that have not yet acquired registry membership.
type RunGuard interface {
	Busy(chatID string) bool
	BusyPath(path string) bool
}

type Service struct {
	store   *registry.Store
	runs    RunGuard
	mu      sync.Mutex
	active  map[string]context.CancelFunc
	closed  bool
	wg      sync.WaitGroup
	pulseMu sync.Mutex
	pulse   chan struct{}
	// Test seam at the process boundary; production always uses gitClone.
	clone func(context.Context, registry.ProjectOperation, Auth, string, func(string)) error
}

func New(store *registry.Store, runs RunGuard) (*Service, error) {
	s := &Service{store: store, runs: runs, active: map[string]context.CancelFunc{}, pulse: make(chan struct{}), clone: gitClone}
	if err := s.recover(); err != nil {
		s.Close()
		return nil, err
	}
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

func (s *Service) Changes() <-chan struct{} {
	s.pulseMu.Lock()
	defer s.pulseMu.Unlock()
	return s.pulse
}
func (s *Service) notify() {
	s.pulseMu.Lock()
	close(s.pulse)
	s.pulse = make(chan struct{})
	s.pulseMu.Unlock()
}
func (s *Service) Get(id string) (registry.ProjectOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.ProjectOperation(id)
}
func (s *Service) List(activeOnly bool) ([]registry.ProjectOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.ProjectOperations(activeOnly)
}

func (s *Service) Start(req Request) (registry.ProjectOperation, error) {
	spec, err := normalize(req)
	if err != nil {
		return registry.ProjectOperation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return registry.ProjectOperation{}, fmt.Errorf("project service is closed")
	}
	o, start, err := s.store.ReserveProject(spec)
	if err != nil {
		return o, err
	}
	if start {
		s.launch(o, req.Auth)
	}
	s.notify()
	return o, nil
}

func (s *Service) Retry(id string, auth Auth) (registry.ProjectOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return registry.ProjectOperation{}, fmt.Errorf("project service is closed")
	}
	o, err := s.store.ProjectOperation(id)
	if err != nil {
		return o, err
	}
	if o.Active() || o.Status == "succeeded" {
		return o, nil
	}
	if o.Source == "delete" {
		if auth != (Auth{}) {
			return o, invalid("removing a project does not use authentication")
		}
		if o.Phase != "deleting" {
			if err := s.ensureRemovable(o); err != nil {
				return o, err
			}
		}
		o, err = s.store.RetryRemoval(id)
		if err == nil {
			s.launch(o, Auth{})
			s.notify()
		}
		return o, err
	}
	if auth.Kind == "" {
		auth.Kind = o.AuthKind
	}
	if auth.Kind != o.AuthKind {
		return o, invalid("retry must use the original authentication method")
	}
	// A failure after publishing the owned directory can be completed without
	// cloning again. Never delete a destination merely because a retry was requested.
	if markerMatches(o.Path, o) {
		o, err = s.store.ChangeProjectOperation(id, func(v *registry.ProjectOperation) error {
			v.Status = "running"
			v.Phase = "registering"
			v.Error = ""
			return nil
		})
		if err == nil {
			o, err = s.store.CompleteProject(id)
		}
		if err == nil {
			removeMarker(o.Path, o)
		} else {
			_, _ = s.store.ChangeProjectOperation(id, func(v *registry.ProjectOperation) error {
				v.Status, v.Error = "failed", err.Error()
				return nil
			})
		}
		s.notify()
		return o, err
	}
	if err := validateAuth(o.RepoURL, auth); err != nil {
		return o, err
	}
	o, err = s.store.RetryProject(id)
	if err != nil {
		return o, err
	}
	s.launch(o, auth)
	s.notify()
	return o, nil
}

func (s *Service) Cancel(id string) (registry.ProjectOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.store.ProjectOperation(id)
	if err != nil {
		return o, err
	}
	if !o.Active() {
		return o, nil
	}
	if o.Source == "delete" && o.Phase == "deleting" {
		return o, fmt.Errorf("%w: folder deletion has already committed and cannot be cancelled", registry.ErrConflict)
	}
	o, err = s.store.ChangeProjectOperation(id, func(v *registry.ProjectOperation) error { v.Status = "cancelling"; return nil })
	if err != nil {
		return o, err
	}
	if cancel := s.active[id]; cancel != nil {
		cancel()
	}
	s.notify()
	return o, nil
}

// Save preserves the existing metadata API while enforcing the same directory
// rules for every client. Moving a project needs a dedicated filesystem operation;
// a rename cannot quietly redirect running chats to a different directory.
func (s *Service) Save(id string, data []byte, expected *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p registry.Project
	if err := json.Unmarshal(data, &p); err != nil {
		return invalid("invalid project")
	}
	path, err := workspacepath.Canonical(p.Path)
	if err != nil {
		return invalid(err.Error())
	}
	previous, err := s.store.Project(id)
	if err == nil {
		oldPath, pathErr := workspacepath.Canonical(previous.Path)
		if pathErr != nil || oldPath != path {
			return invalid("a project's directory cannot change through a metadata edit")
		}
	} else if errors.Is(err, registry.ErrNotFound) {
		if err = directory(path); err != nil {
			return invalid(err.Error())
		}
	} else {
		return err
	}
	p.Path = path
	if p.RepoURL != "" {
		if err = validateRepo(p.RepoURL); err != nil {
			return err
		}
	}
	data, err = json.Marshal(p)
	if err != nil {
		return err
	}
	return s.store.Put("projects", id, data, expected)
}

func (s *Service) Remove(id string, expected *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.store.Snapshot(nil)
	if err != nil {
		return err
	}
	for _, chat := range snapshot.Chats {
		if chat.ProjectID == id && s.runs != nil && s.runs.Busy(chat.ID) {
			return fmt.Errorf("%w: stop the running chat before removing this project", registry.ErrConflict)
		}
	}
	return s.store.Delete("projects", id, expected, false)
}

func normalize(req Request) (registry.ProjectSpec, error) {
	spec := req.ProjectSpec
	if strings.TrimSpace(spec.ID) == "" || len(spec.ID) > 512 || spec.ID == "." || spec.ID == ".." {
		return spec, invalid("a stable operation id is required")
	}
	for _, c := range spec.ID {
		if c < 32 || c == 127 {
			return spec, invalid("invalid operation id")
		}
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" || len(spec.Name) > 512 {
		return spec, invalid("a project name is required")
	}
	path, err := workspacepath.Canonical(spec.Path)
	if err != nil {
		return spec, invalid(err.Error())
	}
	spec.Path = path
	if spec.Source != "folder" && spec.Source != "create" && spec.Source != "clone" {
		return spec, invalid("source must be folder, create, or clone")
	}
	if spec.ExpectedRevision != 0 {
		return spec, invalid("creation does not use an expected revision")
	}
	spec.AuthKind = req.Auth.Kind
	if spec.Source == "clone" {
		if err = validateRepo(spec.RepoURL); err != nil {
			return spec, err
		}
		if err = validateAuth(spec.RepoURL, req.Auth); err != nil {
			return spec, err
		}
		if len(spec.Branch) > 512 || strings.ContainsAny(spec.Branch, "\x00\r\n") {
			return spec, invalid("invalid branch")
		}
	} else if spec.RepoURL != "" || spec.Branch != "" || req.Auth != (Auth{}) {
		return spec, invalid("repository and authentication are only used for cloning")
	}
	return spec, nil
}

func invalid(message string) error { return fmt.Errorf("%w: %s", registry.ErrInvalid, message) }
func directory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("project path is not a directory")
	}
	return nil
}

func (s *Service) launch(o registry.ProjectOperation, auth Auth) {
	ctx, cancel := context.WithTimeout(context.Background(), maxDuration)
	s.active[o.ID] = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		var err error
		if o.Source == "delete" {
			err = s.runRemoval(ctx, o)
		} else {
			err = s.run(ctx, o, auth)
		}
		// A cancelled clone can leave a large tree. Keep status/reconnect responsive
		// during cleanup, and keep its reservation active until cleanup has finished.
		if err != nil && o.Source != "delete" {
			if cleanupErr := cleanupStage(o); cleanupErr != nil {
				err = fmt.Errorf("%v; temporary files retained at %s: %w", err, stagePath(o), cleanupErr)
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.active, o.ID)
		if err != nil {
			_, _ = s.store.ChangeProjectOperation(o.ID, func(v *registry.ProjectOperation) error {
				if !v.Active() {
					return registry.ErrConflict
				}
				wasCancelling := v.Status == "cancelling"
				v.Status = "failed"
				v.Error = redact(err.Error(), auth)
				if (wasCancelling || errors.Is(ctx.Err(), context.Canceled)) && !(v.Source == "delete" && v.Phase == "removing") {
					v.Status = "cancelled"
					v.Error = "Project operation cancelled"
				}
				if s.closed {
					v.Status = "interrupted"
					if v.Source != "delete" || v.Phase != "removing" {
						v.Error = "Interrupted by daemon shutdown; retry to continue"
					}
				}
				return nil
			})
		}
		s.notify()
	}()
}

func (s *Service) update(id, phase string, progress *float64, line string) error {
	_, err := s.store.ChangeProjectOperation(id, func(o *registry.ProjectOperation) error {
		if !o.Active() || o.Status == "cancelling" {
			return context.Canceled
		}
		o.Status = "running"
		o.Phase = phase
		o.Progress = progress
		if line != "" {
			o.Log += line + "\n"
			if len(o.Log) > 16384 {
				o.Log = o.Log[len(o.Log)-16384:]
			}
		}
		return nil
	})
	if err == nil {
		s.notify()
	}
	return err
}

func (s *Service) run(ctx context.Context, o registry.ProjectOperation, auth Auth) error {
	if err := s.update(o.ID, "preparing", nil, ""); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.Source == "folder" {
		if err := directory(o.Path); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := s.store.CompleteProject(o.ID)
		return err
	}
	if o.Path == filepath.Dir(o.Path) {
		return invalid("cannot create a project at the filesystem root")
	}
	if err := directory(filepath.Dir(o.Path)); err != nil {
		return err
	}
	if _, err := os.Lstat(o.Path); !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		return fmt.Errorf("destination already exists; open it as a folder instead")
	}
	stage, err := prepareStage(o)
	if err != nil {
		return err
	}
	repo := filepath.Join(stage, "project")
	if o.Source == "clone" {
		if err = s.update(o.ID, "cloning", nil, ""); err != nil {
			return err
		}
		// Bound SQLite writes and full-snapshot SSE traffic while retaining the log
		// tail. Native git can emit hundreds of progress records in a single burst.
		pending, phase := "", "cloning"
		var fraction *float64
		var outputErr error
		var lastFlush time.Time
		flush := func() {
			if pending != "" && outputErr == nil {
				outputErr = s.update(o.ID, phase, fraction, strings.TrimSuffix(pending, "\n"))
				pending = ""
				lastFlush = time.Now()
			}
		}
		err = s.clone(ctx, o, auth, repo, func(line string) {
			if next, progress := parseProgress(line); next != "" {
				phase, fraction = next, progress
			}
			pending += redact(line, auth) + "\n"
			if len(pending) > 16384 {
				pending = pending[len(pending)-16384:]
			}
			if time.Since(lastFlush) >= 150*time.Millisecond {
				flush()
			}
		})
		flush()
		if outputErr != nil {
			return outputErr
		}
		if err != nil {
			return err
		}
		if err = s.update(o.ID, "verifying", nil, ""); err != nil {
			return err
		}
		if err = verifyRepository(ctx, repo); err != nil {
			return err
		}
	} else if err = os.Mkdir(repo, 0755); err != nil {
		return err
	}
	if err = writeMarker(repo, o); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	// Persist intent before the filesystem rename. Restart recovery recognizes
	// the marker on either side and finishes registration without re-cloning.
	if err = s.update(o.ID, "registering", nil, ""); err != nil {
		return err
	}
	if err = publish(repo, o); err != nil {
		return err
	}
	if _, err = s.store.CompleteProject(o.ID); err != nil {
		return err
	}
	removeMarker(o.Path, o)
	_ = cleanupStage(o)
	return nil
}

func operationHash(o registry.ProjectOperation) string {
	sum := sha256.Sum256([]byte(o.ID))
	return hex.EncodeToString(sum[:16])
}
func stagePath(o registry.ProjectOperation) string {
	return filepath.Join(filepath.Dir(o.Path), fmt.Sprintf(".mindwire-project-%s-%d", operationHash(o), stagingAttempt(o)))
}
func stagingAttempt(o registry.ProjectOperation) int {
	if o.Source == "delete" {
		return 1
	} // retries retain the same quarantined directory
	return o.Attempt
}
func markerName(o registry.ProjectOperation) string { return ".mindwire-operation-" + operationHash(o) }
func markerData(o registry.ProjectOperation) []byte {
	return []byte(fmt.Sprintf("%s\n%d\n%s", o.ID, stagingAttempt(o), o.Path))
}
func markerMatches(dir string, o registry.ProjectOperation) bool {
	b, err := os.ReadFile(filepath.Join(dir, markerName(o)))
	return err == nil && string(b) == string(markerData(o))
}
func writeMarker(dir string, o registry.ProjectOperation) error {
	f, err := os.OpenFile(filepath.Join(dir, markerName(o)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(markerData(o))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func removeMarker(dir string, o registry.ProjectOperation) {
	if markerMatches(dir, o) {
		_ = os.Remove(filepath.Join(dir, markerName(o)))
	}
}
func prepareStage(o registry.ProjectOperation) (string, error) {
	stage := stagePath(o)
	if err := os.Mkdir(stage, 0700); err != nil {
		return "", fmt.Errorf("cannot reserve project staging directory: %w", err)
	}
	if err := writeMarker(stage, o); err != nil {
		_ = os.Remove(stage)
		return "", err
	}
	return stage, nil
}
func cleanupStage(o registry.ProjectOperation) error {
	stage := stagePath(o)
	info, err := os.Lstat(stage)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !markerMatches(stage, o) {
		return fmt.Errorf("staging directory ownership changed")
	}
	return os.RemoveAll(stage)
}
func publish(repo string, o registry.ProjectOperation) error {
	if !markerMatches(repo, o) {
		return fmt.Errorf("project staging ownership changed")
	}
	if _, err := os.Lstat(o.Path); !os.IsNotExist(err) {
		return fmt.Errorf("destination appeared during project creation; existing files were preserved")
	}
	return renameExclusive(repo, o.Path)
}

func (s *Service) recover() error {
	recent, err := s.store.ProjectOperations(false)
	if err != nil {
		return err
	}
	for _, o := range recent {
		if o.Status == "succeeded" {
			if o.Source == "delete" {
				_ = cleanupRemoval(o)
				continue
			}
			removeMarker(o.Path, o)
			_ = cleanupStage(o)
		}
	}
	operations, err := s.store.ProjectOperations(true)
	if err != nil {
		return err
	}
	for _, o := range operations {
		if o.Source == "delete" {
			s.mu.Lock()
			s.launch(o, Auth{})
			s.mu.Unlock()
			continue
		}
		if o.Status == "running" && o.Phase == "registering" {
			if !markerMatches(o.Path, o) && markerMatches(filepath.Join(stagePath(o), "project"), o) {
				err = publish(filepath.Join(stagePath(o), "project"), o)
			}
			if markerMatches(o.Path, o) {
				if _, err = s.store.CompleteProject(o.ID); err == nil {
					removeMarker(o.Path, o)
					_ = cleanupStage(o)
					continue
				}
			}
		}
		_, err = s.store.ChangeProjectOperation(o.ID, func(v *registry.ProjectOperation) error {
			v.Status = "interrupted"
			v.Error = "Interrupted by daemon restart; retry to continue"
			return nil
		})
		if err != nil {
			return err
		}
		_ = cleanupStage(o)
	}
	return nil
}
