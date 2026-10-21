// Package shortcuts opens saved app links using the existing workspace executor.
package shortcuts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/session"
	"github.com/oblien/mindwire/packages/daemon/internal/workspaceexec"
)

type opening struct {
	done chan struct{}
	app  registry.AppShortcut
	err  error
}

type Service struct {
	db                  *registry.Store
	exec                *workspaceexec.Service
	ctx                 context.Context
	cancel              context.CancelFunc
	mu                  sync.Mutex
	opening             map[string]*opening
	closed              bool
	schedulerStarted    bool
	http                *http.Client
	notifications       *session.Store
	notifyBase          string
	credentialMu        sync.Mutex
	wake                chan struct{}
	workers             chan struct{}
	background          sync.WaitGroup
	deliveryHTTP        *http.Client
	budgets             map[string]notificationBudget
	admission           func() (func(), error)
	chatRuntime         ChatScheduleRuntime
	integrationChannels interface {
		Channels() []Channel
		Send(context.Context, string, string) error
	}
}

func (s *Service) SetIntegrationChannels(channels interface {
	Channels() []Channel
	Send(context.Context, string, string) error
}) {
	s.integrationChannels = channels
}

func (s *Service) SetAdmission(admission func() (func(), error)) { s.admission = admission }
func (s *Service) admit() (func(), error) {
	if s.admission != nil {
		return s.admission()
	}
	return func() {}, nil
}

func New(db *registry.Store, execution *workspaceexec.Service) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	// Own the standard connection pool; closing one SDK client must not clear
	// the host application's shared default HTTP pool.
	deliveryTransport := http.DefaultTransport
	if shared, ok := deliveryTransport.(*http.Transport); ok {
		deliveryTransport = shared.Clone()
	}
	return &Service{db: db, exec: execution, ctx: ctx, cancel: cancel, opening: map[string]*opening{},
		wake: make(chan struct{}, 1), workers: make(chan struct{}, 4), deliveryHTTP: &http.Client{Timeout: 10 * time.Second, Transport: deliveryTransport}, budgets: map[string]notificationBudget{},
		http: &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	pending := make([]<-chan struct{}, 0, len(s.opening))
	for _, job := range s.opening {
		pending = append(pending, job.done)
	}
	s.mu.Unlock()
	for _, done := range pending {
		<-done
	}
	s.background.Wait()
	s.http.CloseIdleConnections()
	if transport, ok := s.deliveryHTTP.Transport.(*http.Transport); ok && transport != http.DefaultTransport {
		transport.CloseIdleConnections()
	}
}

// One startup per app. A disconnected client only cancels its waiter; the accepted
// startup has its own bounded lifetime and never depends on an agent/chat staying open.
func (s *Service) Open(ctx context.Context, id string) (registry.AppShortcut, error) {
	if err := ctx.Err(); err != nil {
		return registry.AppShortcut{}, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return registry.AppShortcut{}, context.Canceled
	}
	job := s.opening[id]
	if job == nil {
		job = &opening{done: make(chan struct{})}
		s.opening[id] = job
		go func() {
			work, cancel := context.WithTimeout(s.ctx, 35*time.Second)
			defer cancel()
			done, err := s.admit()
			if err != nil {
				job.err = err
			} else {
				job.app, job.err = s.ensure(work, id)
				done()
			}
			s.mu.Lock()
			delete(s.opening, id)
			close(job.done)
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return registry.AppShortcut{}, ctx.Err()
	case <-job.done:
		return job.app, job.err
	}
}

func (s *Service) ready(ctx context.Context, app registry.AppShortcut) bool {
	u, err := url.Parse(app.URL)
	if err != nil {
		return false
	}
	if app.HealthPath != "" {
		p, err := url.Parse(app.HealthPath)
		if err != nil {
			return false
		}
		u.Path, u.RawPath, u.RawQuery, u.Fragment = p.Path, p.RawPath, p.RawQuery, ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode >= 200 && resp.StatusCode < 400 || resp.StatusCode == 401 || resp.StatusCode == 403
}

func (s *Service) ensure(ctx context.Context, id string) (registry.AppShortcut, error) {
	app, err := s.db.App(id)
	if err != nil {
		return app, err
	}
	if app.Archived {
		return app, fmt.Errorf("%w: this app was removed from Apps", registry.ErrDeleted)
	}
	// Public links need no remote process, health request or tunnel.
	if app.Automation || !registry.LocalAppURL(app.URL) && app.StartupCommand == "" {
		return app, nil
	}
	if s.ready(ctx, app) {
		return app, nil
	}
	if app.StartupCommand == "" {
		return app, errors.New("this app is not running; ask its bot to start it or save a startup script")
	}
	directory, err := s.db.AppDirectory(app)
	if err != nil {
		return app, err
	}
	result, err := s.exec.Exec(ctx, workspaceexec.ExecRequest{Argv: []string{"sh", "-lc", app.StartupCommand}, Directory: directory, TimeoutMS: 20000}, nil)
	if err != nil {
		return app, fmt.Errorf("app startup did not finish: %w", err)
	}
	if result.ExitCode != 0 {
		return app, fmt.Errorf("app startup script failed (exit %d); ask its bot to repair the startup script", result.ExitCode)
	}
	timer := time.NewTicker(400 * time.Millisecond)
	defer timer.Stop()
	for {
		if s.ready(ctx, app) {
			return app, nil
		}
		select {
		case <-ctx.Done():
			return app, errors.New("the app has not become ready yet; try opening it again")
		case <-timer.C:
		}
	}
}
