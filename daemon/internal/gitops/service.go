// Package gitops owns durable workspace Git writes. HTTP is only a caller and
// observer: losing a connection never cancels or replays an accepted operation.
package gitops

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

const Version = 1
const maxDuration = 15 * time.Minute

type running struct {
	path   string
	cancel context.CancelFunc
}

type operationStore interface {
	GitOperation(string) (registry.GitOperation, error)
	GitOperations(string, bool) ([]registry.GitOperation, error)
	ReserveGitOperation(registry.GitSpec) (registry.GitOperation, bool, error)
	ChangeGitOperation(string, func(*registry.GitOperation)) (registry.GitOperation, error)
}

type Service struct {
	store         operationStore
	access        *gitaccess.Service
	mu            sync.Mutex
	active        map[string]running
	closed        bool
	wg            sync.WaitGroup
	pulse         chan struct{}
	storageErrors sync.Map
	// Replaceable only by tests at the execution boundary.
	run func(context.Context, registry.GitSpec, *gitaccess.Connection, *gitaccess.Auth) (gitaccess.Result, error)
}

func New(store *registry.Store, access *gitaccess.Service) (*Service, error) {
	s := &Service{store: store, access: access, active: map[string]running{}, pulse: make(chan struct{})}
	s.run = s.execute
	unfinished, err := store.GitOperations("", true)
	if err != nil {
		return nil, err
	}
	for _, o := range unfinished {
		if _, err := store.ChangeGitOperation(o.ID, func(v *registry.GitOperation) {
			v.Status = "interrupted"
			v.Error = "The daemon restarted during this Git operation. Refresh the repository before starting another operation; some changes may have completed."
		}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	for _, job := range s.active {
		job.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) Get(id string) (registry.GitOperation, error) {
	if err, ok := s.storageErrors.Load(id); ok {
		return registry.GitOperation{}, err.(error)
	}
	return s.store.GitOperation(id)
}
func (s *Service) List(projectID string, activeOnly bool) ([]registry.GitOperation, error) {
	return s.store.GitOperations(projectID, activeOnly)
}

// SameIntent ignores the server-resolved directory: a project moved after a
// completed request must not cause that request's ID to execute a second time.
func SameIntent(a, b registry.GitSpec) bool {
	a.Path, b.Path = "", ""
	return reflect.DeepEqual(a, b)
}

func (s *Service) Start(spec registry.GitSpec, c *gitaccess.Connection, auth *gitaccess.Auth) (registry.GitOperation, error) {
	if err := Normalize(&spec); err != nil {
		return registry.GitOperation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return registry.GitOperation{}, fmt.Errorf("Git operation service is closed")
	}
	if old, err := s.Get(spec.ID); err == nil {
		if !SameIntent(old.GitSpec, spec) {
			return old, registry.ErrConflict
		}
		return old, nil
	} else if !errors.Is(err, registry.ErrNotFound) {
		return registry.GitOperation{}, err
	}
	if spec.Path == "" {
		return registry.GitOperation{}, fmt.Errorf("%w: a repository directory is required", registry.ErrInvalid)
	}
	root, err := Root(context.Background(), spec.Path)
	if err != nil {
		return registry.GitOperation{}, err
	}
	spec.Path = root
	// Fail authorization before accepting a write. The credential remains only in
	// this worker's memory; neither the intent nor the operation record contains it.
	if Network(spec.Action) {
		resolved, err := s.access.Resolve(c, auth)
		if err != nil {
			return registry.GitOperation{}, err
		}
		if spec.Action == "push" && resolved.ReadOnly {
			return registry.GitOperation{}, fmt.Errorf("%w: this GitHub connection is read-only; choose a connection with write access", gitaccess.ErrCredential)
		}
		if c != nil {
			copy := *c
			c = &copy
			auth = &resolved
		}
	} else {
		c, auth = nil, nil
	}
	o, created, err := s.store.ReserveGitOperation(spec)
	if err != nil || !created {
		return o, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), maxDuration)
	s.active[o.ID] = running{path: spec.Path, cancel: cancel}
	s.wg.Add(1)
	s.notifyLocked()
	go s.work(ctx, cancel, o, c, auth)
	return o, nil
}

func (s *Service) work(ctx context.Context, cancel context.CancelFunc, o registry.GitOperation, c *gitaccess.Connection, auth *gitaccess.Auth) {
	defer s.wg.Done()
	defer cancel()
	s.mu.Lock()
	_, err := s.store.ChangeGitOperation(o.ID, func(v *registry.GitOperation) {
		if v.Status == "queued" {
			v.Status = "running"
		}
	})
	s.notifyLocked()
	s.mu.Unlock()
	var result gitaccess.Result
	if err == nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			result, err = s.run(ctx, o.GitSpec, c, auth)
		}
	}
	redact := gitaccess.Auth{}
	if auth != nil {
		redact = *auth
	}
	result.Output = gitaccess.Redact(result.Output, redact)
	if err != nil {
		err = errors.New(gitaccess.Redact(err.Error(), redact))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, saveErr := s.store.ChangeGitOperation(o.ID, func(v *registry.GitOperation) {
		v.Status, v.Output = "succeeded", result.Output
		if err != nil {
			v.Status, v.Error = "failed", err.Error()
			if ctx.Err() != nil {
				v.Status = "cancelled"
				if s.closed || errors.Is(ctx.Err(), context.DeadlineExceeded) {
					v.Status = "interrupted"
				}
				v.Error = "Git was interrupted. Refresh the repository before starting another operation; some changes may have completed."
			}
		}
	})
	// If saving the outcome fails, the durable reservation stays active and
	// blocks further writes until recovery. Never report an unrecorded success.
	if saveErr == nil {
		delete(s.active, o.ID)
	} else {
		s.storageErrors.Store(o.ID, fmt.Errorf("could not persist the Git operation result: %w", saveErr))
	}
	s.notifyLocked()
}

func (s *Service) Cancel(id string) (registry.GitOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.Get(id)
	if err != nil || !o.Active() {
		return o, err
	}
	job, ok := s.active[id]
	if !ok {
		return o, registry.ErrConflict
	}
	o, err = s.store.ChangeGitOperation(id, func(v *registry.GitOperation) { v.Status = "cancelling" })
	if err == nil {
		job.cancel()
		s.notifyLocked()
	}
	return o, err
}

func (s *Service) BusyPath(path string) bool {
	canonical, err := workspacepath.WorkingDirectory(path)
	if err != nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.active {
		if workspacepath.Overlaps(canonical, job.path) {
			return true
		}
	}
	return false
}
func (s *Service) ActiveCount() int         { s.mu.Lock(); defer s.mu.Unlock(); return len(s.active) }
func (s *Service) Changes() <-chan struct{} { s.mu.Lock(); defer s.mu.Unlock(); return s.pulse }
func (s *Service) notifyLocked()            { close(s.pulse); s.pulse = make(chan struct{}) }

func (s *Service) Wait(ctx context.Context, id string) (registry.GitOperation, error) {
	for {
		changed := s.Changes()
		o, err := s.Get(id)
		if err != nil || !o.Active() {
			return o, err
		}
		select {
		case <-ctx.Done():
			return o, ctx.Err()
		case <-changed:
		}
	}
}
