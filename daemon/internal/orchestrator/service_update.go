package orchestrator

import (
	"crypto/rand"
	"errors"
	"os"
	"sync"
	"time"
)

const ServiceUpdateVersion = 1

var (
	ErrServiceBusy     = errors.New("workspace operations are still running; the service update must wait")
	ErrServiceUpdating = errors.New("the Mindwire service is updating; retry shortly")
)

// ServiceUpdateLease closes admission only for the final binary replacement. The
// installer downloads first, acquires this lease, then replaces the idle service.
// A crashed installer cannot leave admission closed indefinitely.
type ServiceUpdateLease struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type ServiceUpdateState struct {
	Idle             bool `json:"idle"`
	Updating         bool `json:"updating"`
	ActiveOperations int  `json:"activeOperations"`
}

func (s *Supervisor) serviceUpdatingLocked() bool {
	if s.updateLease != nil && !time.Now().Before(s.updateLease.ExpiresAt) {
		s.updateLease = nil
	}
	return s.updateLease != nil
}

func (s *Supervisor) serviceActivityLocked() int {
	// inflight is decremented AFTER the terminal transcript/run has been saved.
	n := s.inflight + s.serviceOperations
	for _, a := range s.agents {
		if s.setup.Status(a.ID()).Running {
			n++
		}
		if auth, ok := a.Auth.(interface{ Active() bool }); ok && auth.Active() {
			n++
		}
	}
	return n
}

func (s *Supervisor) ServiceUpdateState() ServiceUpdateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	updating, active := s.serviceUpdatingLocked(), s.serviceActivityLocked()
	return ServiceUpdateState{Idle: !updating && active == 0, Updating: updating, ActiveOperations: active}
}

// BeginServiceOperation covers a mutating API request until its synchronous
// work finishes or its asynchronous job has been registered. New turns/setup
// also check this same admission lock, including calls from the embedded SDK.
func (s *Supervisor) BeginServiceOperation() (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serviceUpdatingLocked() {
		return nil, ErrServiceUpdating
	}
	s.serviceOperations++
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); s.serviceOperations--; s.mu.Unlock() }) }, nil
}

func (s *Supervisor) AcquireServiceUpdate() (ServiceUpdateLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serviceUpdatingLocked() {
		return ServiceUpdateLease{}, ErrServiceUpdating
	}
	if s.serviceActivityLocked() > 0 {
		return ServiceUpdateLease{}, ErrServiceBusy
	}
	lease := ServiceUpdateLease{ID: rand.Text(), PID: os.Getpid(), ExpiresAt: time.Now().Add(time.Minute)}
	s.updateLease = &lease
	return lease, nil
}

func (s *Supervisor) ReleaseServiceUpdate(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.serviceUpdatingLocked() || s.updateLease.ID != id {
		return false
	}
	s.updateLease = nil
	return true
}
