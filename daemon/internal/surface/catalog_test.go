package surface

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
)

// Exercises the actual runtime HTTP catalog with independent display providers.
// No existing account, project, display or credential is used by these tests.
type catalogFixture struct {
	mu                                     sync.Mutex
	server                                 *httptest.Server
	items                                  map[string]SavedDesktop
	keys                                   map[string]string
	bodies                                 [][]byte
	posts, starts, stops, enables, deletes int
	loseFirstResponse                      bool
	mode                                   string
	defaultWidth                           int
	providers                              map[string]*testProvider
	stopEntered                            chan struct{}
	stopRelease                            <-chan struct{}
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	f := &catalogFixture{items: map[string]SavedDesktop{}, keys: map[string]string{}, mode: "virtual", defaultWidth: 1280, providers: map[string]*testProvider{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}
func (f *catalogFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/stop") {
		f.mu.Lock()
		entered, release := f.stopEntered, f.stopRelease
		f.mu.Unlock()
		if entered != nil {
			close(entered)
		}
		if release != nil {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer runtime-test" {
		http.Error(w, "unauthorized", 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	if r.URL.Path == "/desktop/status" {
		write(ProviderStatus{Supported: true, Enabled: false})
		return
	}
	if r.URL.Path == "/desktop/enable" {
		f.enables++
		write(map[string]bool{"success": true})
		return
	}
	if r.URL.Path == "/desktop/sessions" {
		if r.Method == "GET" {
			items := []SavedDesktop{}
			for _, item := range f.items {
				items = append(items, item)
			}
			write(map[string]any{"success": true, "sessions": items, "capabilities": map[string]any{
				"mode": f.mode, "max_sessions": 10, "can_create": len(f.items) < 10,
				"resolution": map[string]any{"min": DesktopResolution{640, 480}, "max": DesktopResolution{3840, 2160}, "default": DesktopResolution{f.defaultWidth, 800}},
			}})
			return
		}
		if r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			var request struct {
				Name       string             `json:"name"`
				Key        string             `json:"idempotency_key"`
				Resolution *DesktopResolution `json:"resolution"`
			}
			if json.Unmarshal(body, &request) != nil || request.Key == "" || len(request.Key) > 64 || len(request.Name) > 80 {
				http.Error(w, "invalid desktop name or idempotency key", 400)
				return
			}
			f.posts++
			f.bodies = append(f.bodies, body)
			id := f.keys[request.Key]
			if id == "" {
				id = fmt.Sprintf("ds_%016x", len(f.items)+1)
				if f.mode == "console" {
					id = "console"
				}
				f.keys[request.Key] = id
				f.items[id] = SavedDesktop{ID: id, Name: request.Name, Resolution: request.Resolution, State: "running", Managed: true, Available: true, Mode: f.mode}
			}
			if f.loseFirstResponse && f.posts == 1 {
				http.Error(w, "accepted but response interrupted", 503)
				return
			}
			write(DesktopResult{true, f.items[id]})
			return
		}
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/desktop/sessions/"), "/")
	item, ok := f.items[parts[0]]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == "DELETE" {
		if item.ID != "console" && item.State != "stopped" {
			http.Error(w, "stop the desktop before deleting its saved profile", http.StatusConflict)
			return
		}
		f.deletes++
		delete(f.items, item.ID)
		write(map[string]bool{"success": true})
		return
	}
	if r.Method == "PATCH" {
		var update UpdateDesktopRequest
		if json.NewDecoder(r.Body).Decode(&update) != nil {
			http.Error(w, "invalid update", http.StatusBadRequest)
			return
		}
		if update.Resolution != nil && item.State != "stopped" {
			w.WriteHeader(http.StatusConflict)
			write(map[string]any{"success": false, "error": "Stop the desktop before changing its resolution"})
			return
		}
		if update.Name != nil {
			item.Name = *update.Name
		}
		if update.Resolution != nil {
			item.Resolution = update.Resolution
		}
		f.items[item.ID] = item
	}
	if len(parts) == 2 && r.Method == "POST" {
		if parts[1] == "start" {
			f.starts++
			item.State = "running"
			item.Available = true
		}
		if parts[1] == "stop" {
			f.stops++
			item.State = "stopped"
			item.Available = false
		}
		f.items[item.ID] = item
	}
	write(DesktopResult{true, item})
}
func (f *catalogFixture) configure(t *testing.T, s *Service, creds Credentials) {
	t.Helper()
	if err := s.BindRuntime(RuntimeBinding{RegistryID: s.db.Identity(), WorkspaceID: "163119cc95a4dddc", GatewayToken: "runtime-test", ExpiresAt: time.Now().Add(time.Hour)}, creds); err != nil {
		t.Fatal(err)
	}
	s.catalog.mu.Lock()
	s.catalog.backend.base = f.server.URL
	s.catalog.providerFactory = func(id string) (Provider, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := &testProvider{size: Geometry{7, 5, 1}}
		f.providers[id] = p
		return p, nil
	}
	s.catalog.mu.Unlock()
}
func catalogService(t *testing.T) (*Service, *catalogFixture, Credentials) {
	t.Helper()
	s, _, db := testService(t)
	if err := db.Import(registry.Import{Projects: []registry.Project{
		{Record: registry.Record{ID: "project-a"}, Name: "App", Path: t.TempDir()},
		{Record: registry.Record{ID: "project-b"}, Name: "Other", Path: t.TempDir()},
	}}); err != nil {
		t.Fatal(err)
	}
	creds, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := newCatalogFixture(t)
	f.configure(t, s, creds)
	return s, f, creds
}

func TestDesktopResolutionRequiresExplicitStop(t *testing.T) {
	s, f, _ := catalogService(t)
	desktop, err := s.EnsureProjectDesktop(t.Context(), "project-a")
	if err != nil {
		t.Fatal(err)
	}
	name, size := "Renamed desktop", &DesktopResolution{1024, 640}
	_, err = s.DesktopOperation(t.Context(), desktop.ID, "update", &UpdateDesktopRequest{Name: &name, Resolution: size})
	code(t, err, "desktop_conflict")
	if err.Error() != "Stop this desktop before changing its resolution." {
		t.Fatalf("lost actionable provider precondition: %v", err)
	}
	if f.stops != 0 || f.items[desktop.ID].Name != desktop.Name {
		t.Fatal("a rejected resize stopped apps or partially renamed the desktop")
	}
	renamed, err := s.DesktopOperation(t.Context(), desktop.ID, "update", &UpdateDesktopRequest{Name: &name})
	if err != nil || renamed.Session.Name != name || f.stops != 0 {
		t.Fatalf("rename should work while running: %+v %v", renamed, err)
	}
	if _, err = s.DesktopOperation(t.Context(), desktop.ID, "stop", nil); err != nil {
		t.Fatal(err)
	}
	resized, err := s.DesktopOperation(t.Context(), desktop.ID, "update", &UpdateDesktopRequest{Resolution: size})
	if err != nil || resized.Session.Resolution == nil || *resized.Session.Resolution != *size || resized.Session.State != "stopped" {
		t.Fatalf("stopped resolution update failed: %+v %v", resized, err)
	}
}

func TestSlowDesktopStopDoesNotBlockAnotherDisplaysInputOrLeaseReaper(t *testing.T) {
	s, f, _ := catalogService(t)
	a, err := s.EnsureProjectDesktop(t.Context(), "project-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureProjectDesktop(t.Context(), "project-b")
	if err != nil {
		t.Fatal(err)
	}
	user := Actor{Kind: "user"}
	viewer, err := s.Open(t.Context(), user, OpenRequest{RequestID: "view-b", Mode: "control", DesktopID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.stopEntered, f.stopRelease = entered, release
	f.mu.Unlock()
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	stopped := make(chan error, 1)
	go func() { _, err := s.DesktopOperation(t.Context(), a.ID, "stop", nil); stopped <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("stop never reached the runtime")
	}
	input := make(chan error, 1)
	go func() {
		selected, err := s.ForDesktop(b.ID)
		if err == nil {
			_, err = selected.Apply(t.Context(), user, ActionRequest{RequestID: "input-on-b", SessionID: viewer.ID,
				ControlGeneration: viewer.Controller.Generation, Action: Action{Kind: "key", Keys: []string{"Escape"}}})
		}
		if err == nil && s.ActiveSessionCount() != 1 {
			err = fmt.Errorf("another desktop lost its viewer")
		}
		input <- err
	}()
	select {
	case err = <-input:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("a slow management request stalled a different display")
	}
	unblock.Do(func() { close(release) })
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

func TestProjectDesktopConcurrentEnsureAndRestart(t *testing.T) {
	s, f, creds := catalogService(t)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			desktop, err := s.EnsureProjectDesktop(t.Context(), "project-a")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- desktop.ID
		}()
	}
	wg.Wait()
	close(ids)
	for id := range ids {
		if id != "ds_0000000000000001" {
			t.Fatalf("duplicate desktop %s", id)
		}
	}
	if f.posts != 1 {
		t.Fatalf("created %d times", f.posts)
	}
	s.Close()
	restored, err := NewConfigured(s.db, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	f.configure(t, restored, creds)
	list, err := restored.ListDesktops(t.Context(), "project-a")
	if err != nil || len(list.Sessions) != 1 {
		t.Fatalf("lost saved association: %+v %v", list, err)
	}
	same, err := restored.EnsureProjectDesktop(t.Context(), "project-a")
	if err != nil || same.ID != list.Sessions[0].ID || f.posts != 1 {
		t.Fatalf("restart created another desktop: %+v %v", same, err)
	}
}

func TestDeleteRunningProjectDesktopReleasesOnlyItsControllerAndCanRetry(t *testing.T) {
	s, fixture, _ := catalogService(t)
	a, err := s.EnsureProjectDesktop(t.Context(), "project-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureProjectDesktop(t.Context(), "project-b")
	if err != nil {
		t.Fatal(err)
	}
	user := Actor{Kind: "user"}
	for _, desktop := range []SavedDesktop{a, b} {
		if _, err := s.Open(t.Context(), user, OpenRequest{RequestID: newID(), DesktopID: desktop.ID, Mode: "control"}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		result, err := s.DesktopOperation(t.Context(), a.ID, "delete", nil)
		if err != nil || !result.Success {
			t.Fatalf("idempotent delete: %+v %v", result, err)
		}
	}
	if fixture.stops != 1 || fixture.deletes != 1 {
		t.Fatalf("duplicate lifecycle mutations: stops=%d deletes=%d", fixture.stops, fixture.deletes)
	}
	if s.ActiveSessionCount() != 1 {
		t.Fatal("delete retained its viewer or closed another desktop")
	}
	other, err := s.ForDesktop(b.ID)
	if err != nil || other.Snapshot().Controller == nil {
		t.Fatal("deleting one desktop interrupted another controller")
	}
}

func TestProjectDesktopUncertainCreateUsesDurableIdenticalRequest(t *testing.T) {
	s, f, creds := catalogService(t)
	f.loseFirstResponse = true
	if _, err := s.EnsureProjectDesktop(t.Context(), "project-a"); err == nil {
		t.Fatal("expected lost response")
	}
	s.Close()
	restored, err := NewConfigured(s.db, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	f.configure(t, restored, creds)
	f.defaultWidth = 1440 // changed provider defaults must not alter the accepted request
	desktop, err := restored.EnsureProjectDesktop(t.Context(), "project-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.items) != 1 || f.posts != 2 || desktop.ID != "ds_0000000000000001" {
		t.Fatalf("uncertain retry duplicated desktop: %+v", desktop)
	}
	if !bytes.Equal(f.bodies[0], f.bodies[1]) {
		t.Fatal("retry changed its body or idempotency key")
	}
}

func TestSavedDesktopsKeepControlCaptureAndProjectScopesSeparate(t *testing.T) {
	s, f, _ := catalogService(t)
	a, err := s.EnsureProjectDesktop(t.Context(), "project-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureProjectDesktop(t.Context(), "project-b")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDesktops(t.Context(), "project-a")
	if err != nil || len(list.Sessions) != 1 || list.Sessions[0].ID != a.ID {
		t.Fatalf("wrong project list: %+v %v", list, err)
	}
	user := Actor{Kind: "user"}
	x, err := s.Open(t.Context(), user, OpenRequest{RequestID: "open-a", Mode: "control", DesktopID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	y, err := s.Open(t.Context(), user, OpenRequest{RequestID: "open-b", Mode: "control", DesktopID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if x.DesktopID != a.ID || y.DesktopID != b.ID || s.ActiveSessionCount() != 2 {
		t.Fatal("controllers were not scoped to saved displays")
	}
	frame, err := s.Capture(t.Context(), x.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Apply(t.Context(), Actor{Kind: "agent", RunID: "unrelated"}, pointerRequest(y, frame))
	code(t, err, "forbidden")
	// A human can control both displays, but a frame from A must never target B.
	agent := Actor{Kind: "agent", RunID: "b", desktopApproved: true}
	_ = s.CloseSession(y.ID, user)
	y, err = s.Open(t.Context(), agent, OpenRequest{RequestID: "agent-b", Mode: "control", DesktopID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Apply(t.Context(), agent, pointerRequest(y, frame))
	code(t, err, "stale_frame")
	child, _ := s.ForDesktop(a.ID)
	if child.Snapshot().Controller.SessionID != x.ID {
		t.Fatal("opening B stole control of A")
	}
	if err := s.CloseSession(x.ID, user); err != nil {
		t.Fatal(err)
	}
	if f.stops != 0 || f.deletes != 0 {
		t.Fatal("viewer close stopped or deleted the saved desktop")
	}
}

func readToolPayload(t *testing.T, result *mcp.CallToolResult, err error) toolPayload {
	t.Helper()
	if err != nil || result == nil {
		t.Fatalf("tool transport: %v", err)
	}
	var value struct {
		Surface toolPayload `json:"mindwireSurface"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &value); err != nil {
		t.Fatal(err)
	}
	return value.Surface
}
func TestProjectDesktopToolsApprovalSameViewerAndRunIsolation(t *testing.T) {
	for _, harness := range []string{"codex", "claude-code"} {
		t.Run(harness, func(t *testing.T) {
			s, f, _ := catalogService(t)
			var approvals atomic.Int32
			s.SetApproval(func(_ context.Context, actor Actor, _ string) error {
				if f.posts != 0 && approvals.Load() == 0 {
					t.Error("desktop created before approval")
				}
				if actor.ProjectID != "project-a" {
					t.Error("lost project context")
				}
				approvals.Add(1)
				return nil
			})
			actor := Actor{Kind: "agent", Name: "Agent", RunID: "run-a", ProjectID: "project-a", ChatID: "chat-a"}
			client, finish := mcpClient(t, s, t.Context(), actor, harness)
			defer finish()
			result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "surface_project_desktop", Arguments: map[string]any{"requestId": "ui-work", "mode": "control"}})
			opened := readToolPayload(t, result, err)
			if result.IsError || opened.Session == nil || opened.DesktopID == "" {
				t.Fatalf("open failed: %+v", opened)
			}
			result, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "surface_project_desktop", Arguments: map[string]any{"requestId": "ui-work", "mode": "control"}})
			retry := readToolPayload(t, result, err)
			if retry.Session.ID != opened.Session.ID || approvals.Load() != 1 || f.posts != 1 {
				t.Fatal("duplicate approval, session, or desktop")
			}
			viewer, err := s.Open(t.Context(), Actor{Kind: "user"}, OpenRequest{RequestID: "phone", Mode: "view", DesktopID: opened.DesktopID})
			if err != nil {
				t.Fatal(err)
			}
			if viewer.DesktopID != opened.Session.DesktopID {
				t.Fatal("phone is watching a different desktop")
			}
			result, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "surface_capture", Arguments: map[string]any{"sessionId": opened.Session.ID}})
			capture := readToolPayload(t, result, err)
			if result.IsError || capture.Capture == nil || len(result.Content) != 2 {
				t.Fatal("agent did not receive the actual image")
			}
			other, err := s.EnsureProjectDesktop(t.Context(), "project-b")
			if err != nil {
				t.Fatal(err)
			}
			result, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "surface_project_desktop", Arguments: map[string]any{"requestId": "wrong-project", "mode": "control", "desktopId": other.ID}})
			denied := readToolPayload(t, result, err)
			if denied.Error == nil || denied.Error.Code != "forbidden" {
				t.Fatal("agent accessed another project's desktop")
			}
			finish()
			if s.ActiveSessionCount() != 1 {
				t.Fatal("run cleanup should keep the phone viewer only")
			}
			if f.stops != 0 || f.deletes != 0 {
				t.Fatal("run cleanup destroyed the desktop")
			}
		})
	}
}

func TestProjectDesktopDenialDoesNotCreateOrEnable(t *testing.T) {
	s, f, _ := catalogService(t)
	s.SetApproval(func(context.Context, Actor, string) error { return problem("denied", "Declined") })
	result, _, err := s.openForRun(t.Context(), Actor{Kind: "agent", ProjectID: "project-a"}, OpenRequest{RequestID: "request", Mode: "control"})
	if err != nil || !result.IsError || f.posts != 0 || f.enables != 0 {
		t.Fatal("denial enabled or created a desktop")
	}
}

func TestConcurrentProjectDesktopOpenCannotDuplicateApproval(t *testing.T) {
	s, f, _ := catalogService(t)
	entered, allow := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(allow) })
	var approvals atomic.Int32
	s.SetApproval(func(ctx context.Context, _ Actor, _ string) error {
		if approvals.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-allow:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	actor := Actor{Kind: "agent", Name: "Agent", RunID: "run-a", ProjectID: "project-a"}
	request := OpenRequest{RequestID: "ui-work", Mode: "control"}
	type openedResult struct {
		result *mcp.CallToolResult
		err    error
	}
	first := make(chan openedResult, 1)
	go func() {
		result, _, err := s.openForRun(t.Context(), actor, request)
		first <- openedResult{result, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("approval never started")
	}
	result, _, err := s.openForRun(t.Context(), actor, request)
	pending := readToolPayload(t, result, err)
	if pending.Error == nil || pending.Error.Code != "approval_pending" || approvals.Load() != 1 || f.posts != 0 {
		t.Fatalf("duplicate request bypassed or repeated approval: %+v", pending)
	}
	once.Do(func() { close(allow) })
	var completed openedResult
	select {
	case completed = <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("approved desktop did not open")
	}
	opened := readToolPayload(t, completed.result, completed.err)
	if opened.Session == nil {
		t.Fatalf("first open failed: %+v", opened)
	}
	result, _, err = s.openForRun(t.Context(), actor, request)
	retry := readToolPayload(t, result, err)
	if retry.Session == nil || retry.Session.ID != opened.Session.ID || approvals.Load() != 1 || f.posts != 1 {
		t.Fatalf("completed retry repeated approval or creation: %+v", retry)
	}
}

func TestSavedConsoleCannotCompeteWithLegacyController(t *testing.T) {
	s, _, _ := testService(t)
	legacy := mustOpen(t, s, Actor{Kind: "user"}, "control")
	creds, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := newCatalogFixture(t)
	f.configure(t, s, creds)
	_, err = s.ForDesktop("console")
	code(t, err, "desktop_conflict")
	if err := s.CloseSession(legacy.ID, Actor{Kind: "user"}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Open(t.Context(), Actor{Kind: "user"}, OpenRequest{RequestID: "old-client", Mode: "control"})
	code(t, err, "desktop_selection_required")
	if _, err = s.ForDesktop("console"); err != nil {
		t.Fatal(err)
	}
}

func TestProjectDesktopLifecycleAndSharedConsole(t *testing.T) {
	s, f, _ := catalogService(t)
	f.mode = "console"
	a, err := s.EnsureProjectDesktop(t.Context(), "project-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureProjectDesktop(t.Context(), "project-b")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "console" || b.ID != a.ID || f.posts != 1 {
		t.Fatal("console was duplicated")
	}
	var body map[string]any
	_ = json.Unmarshal(f.bodies[0], &body)
	if body["resolution"] != nil {
		t.Fatal("console creation sent a virtual resolution")
	}
	if _, err = s.DesktopOperation(t.Context(), a.ID, "stop", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.EnsureProjectDesktop(t.Context(), "project-a"); err != nil || f.starts != 1 {
		t.Fatalf("stopped desktop was not reused: %v", err)
	}
	if _, err = s.DesktopOperation(t.Context(), a.ID, "delete", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.EnsureProjectDesktop(t.Context(), "project-a"); err != nil || f.posts != 2 {
		t.Fatalf("removed desktop could not be replaced: %v", err)
	}
}

func TestCommandDesktopHelperUsesSameToolsAndRevokesRunAccess(t *testing.T) {
	s, f, _ := catalogService(t)
	s.SetApproval(func(context.Context, Actor, string) error { return nil })
	overlay, env, finish, err := s.BindRun(t.Context(), Actor{Kind: "agent", RunID: "fallback", ProjectID: "project-a"}, "opencode", json.RawMessage(`{"existing":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if string(overlay) != `{"existing":true}` {
		t.Fatal("fallback changed harness configuration")
	}
	for key, value := range env {
		t.Setenv(key, value)
	}
	var output bytes.Buffer
	if err := DesktopToolHelper(t.Context(), []string{"surface_project_desktop", `{"requestId":"helper-open","mode":"control"}`}, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Surface   toolPayload `json:"mindwireSurface"`
		ImagePath string      `json:"imagePath"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.Surface.Session == nil {
		t.Fatalf("helper output: %s %v", output.String(), err)
	}
	output.Reset()
	if err := DesktopToolHelper(t.Context(), []string{"surface_capture", fmt.Sprintf(`{"sessionId":%q}`, result.Surface.Session.ID)}, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(result.ImagePath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("screenshot not private")
	}
	finish()
	if _, err := os.Stat(env["MINDWIRE_DESKTOP_IMAGES"]); !os.IsNotExist(err) {
		t.Fatal("run screenshot directory retained")
	}
	if err := DesktopToolHelper(t.Context(), []string{"list"}, io.Discard); err == nil {
		t.Fatal("ended run token still accepted")
	}
	if s.ActiveSessionCount() != 0 || f.stops != 0 {
		t.Fatal("helper cleanup leaked control or stopped the display")
	}
}
