package surface

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
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

type testProvider struct {
	mu                           sync.Mutex
	size                         Geometry
	applied, connected, released int
	failure                      error
	gate                         chan struct{}
}

func (p *testProvider) Status(context.Context) (ProviderStatus, error) {
	return ProviderStatus{Supported: true, Enabled: true, Available: true, Capabilities: Capabilities{View: true, Capture: true, Pointer: true, Keyboard: true, Text: true}}, nil
}
func (p *testProvider) Connect(context.Context) (Geometry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected++
	return p.size, nil
}
func (p *testProvider) Capture(context.Context) (image.Image, Geometry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, p.size.Width, p.size.Height))
	img.Set(0, 0, color.RGBA{200, 30, 40, 255})
	return img, p.size, nil
}
func (p *testProvider) Apply(ctx context.Context, _ Action) (string, error) {
	p.mu.Lock()
	p.applied++
	gate, failure := p.gate, p.failure
	p.mu.Unlock()
	if gate != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-gate:
		}
	}
	return "clipboard-private-content", failure
}
func (p *testProvider) Release(context.Context) error {
	p.mu.Lock()
	p.released++
	p.mu.Unlock()
	return nil
}
func (p *testProvider) Close() error         { return nil }
func (p *testProvider) ExpiresAt() time.Time { return time.Now().Add(time.Hour) }
func testService(t *testing.T) (*Service, *testProvider, *registry.Store) {
	t.Helper()
	db, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	p := &testProvider{size: Geometry{7, 5, 1}}
	if err := s.Configure(p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); _ = db.Close() })
	return s, p, db
}
func mustOpen(t *testing.T, s *Service, actor Actor, mode string) Session {
	t.Helper()
	session, err := s.Open(t.Context(), actor, OpenRequest{RequestID: newID(), Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return session
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var problem *Error
	if !errors.As(err, &problem) || problem.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}
func pointerRequest(session Session, capture Capture) ActionRequest {
	x, y := 1, 2
	return ActionRequest{RequestID: newID(), SessionID: session.ID, ControlGeneration: session.Controller.Generation, FrameID: capture.ID, GeometryRevision: capture.Geometry.Revision, Action: Action{Kind: "click", X: &x, Y: &y}}
}

func TestDesktopOneControllerAndDurableCapture(t *testing.T) {
	s, p, db := testService(t)
	var approvals atomic.Int32
	s.SetApproval(func(context.Context, Actor, string) error { approvals.Add(1); return nil })
	agent := Actor{Kind: "agent", Name: "Codex", RunID: "r1", ChatID: "c1"}
	a := mustOpen(t, s, agent, "control")
	capture, err := s.Capture(t.Context(), a.ID, agent)
	if err != nil {
		t.Fatal(err)
	}
	req := pointerRequest(a, capture)
	first, err := s.Apply(t.Context(), agent, req)
	if err != nil || first.Status != "dispatched" {
		t.Fatalf("action: %v %v", first, err)
	}
	_, err = s.Apply(t.Context(), agent, req)
	if err != nil {
		t.Fatal(err)
	}
	// A reconnect can resend an accepted request after a resize or handoff. Its
	// durable receipt wins over current geometry and input is never dispatched twice.
	p.mu.Lock()
	p.size = Geometry{9, 6, 2}
	p.mu.Unlock()
	if receipt, err := s.Apply(t.Context(), agent, req); err != nil || receipt.ID != first.ID {
		t.Fatalf("duplicate after resize lost its receipt: %v %v", receipt, err)
	}
	p.mu.Lock()
	count := p.applied
	p.mu.Unlock()
	if count != 1 {
		t.Fatalf("input replayed %d times", count)
	}
	req.Action.Kind = "pointer"
	_, err = s.Apply(t.Context(), agent, req)
	code(t, err, "request_conflict")
	user := Actor{Kind: "user"}
	view := mustOpen(t, s, user, "view")
	if _, err = s.Control(t.Context(), view.ID, user, ControlRequest{Action: "acquire"}); err == nil {
		t.Fatal("acquired another controller without takeover")
	}
	if _, err = s.Control(t.Context(), view.ID, user, ControlRequest{Action: "takeover"}); err != nil {
		t.Fatal(err)
	}
	req.Action.Kind = "click"
	if receipt, err := s.Apply(t.Context(), agent, req); err != nil || receipt.ID != first.ID {
		t.Fatalf("duplicate after takeover lost its receipt: %v %v", receipt, err)
	}
	req.RequestID = newID()
	_, err = s.Apply(t.Context(), agent, req)
	code(t, err, "control_lost")
	_, err = s.Control(t.Context(), a.ID, agent, ControlRequest{Action: "takeover"})
	code(t, err, "forbidden")
	_, err = s.Control(t.Context(), a.ID, agent, ControlRequest{Action: "acquire"})
	code(t, err, "control_taken")
	s.CloseRun(agent.RunID)
	if s.Snapshot().Controller.SessionID != view.ID {
		t.Fatal("closing an agent run closed the user's controller")
	}
	if approvals.Load() != 1 {
		t.Fatalf("approval count %d", approvals.Load())
	}
	s.Close()
	recovered, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	content, err := recovered.Artifacts().Get(capture.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(content.Data))
	if err != nil || img.Bounds().Dx() != 7 {
		t.Fatalf("persisted image invalid: %v", err)
	}
	if err = recovered.Artifacts().DeleteChat("c1"); err != nil {
		t.Fatal(err)
	}
	if _, err = recovered.Artifacts().Get(capture.ArtifactID); err == nil {
		t.Fatal("chat deletion retained screenshot")
	}
}

func TestDesktopTakeoverKeepsPreviousHumanViewerAlive(t *testing.T) {
	s, _, _ := testService(t)
	user := Actor{Kind: "user"}
	first := mustOpen(t, s, user, "control")
	second := mustOpen(t, s, user, "view")
	if _, err := s.Control(t.Context(), second.ID, user, ControlRequest{Action: "takeover"}); err != nil {
		t.Fatal(err)
	}
	renewed, err := s.Control(t.Context(), first.ID, user, ControlRequest{Action: "renew", Generation: first.Controller.Generation})
	if err != nil || renewed.Controller != nil {
		t.Fatalf("previous viewer disconnected on takeover: %v %v", renewed, err)
	}
	if s.Snapshot().Controller.SessionID != second.ID {
		t.Fatal("stale heartbeat reclaimed control")
	}
	_, err = s.Control(t.Context(), first.ID, user, ControlRequest{Action: "acquire"})
	code(t, err, "control_busy")
	if err := s.CloseSession(second.ID, user); err != nil {
		t.Fatal(err)
	}
	reacquired, err := s.Control(t.Context(), first.ID, user, ControlRequest{Action: "acquire"})
	if err != nil || reacquired.Controller == nil || reacquired.Controller.Generation <= first.Controller.Generation {
		t.Fatalf("the human viewer could not explicitly reacquire control: %v %v", reacquired, err)
	}
}

func TestDesktopPermissionOverlayPreservesUserRules(t *testing.T) {
	existing := json.RawMessage(`{"env":{"CUSTOM":"yes"},"permissions":{"allow":["Bash(git status)"],"deny":["mcp__mindwire_desktop__surface_action"],"defaultMode":"default"}}`)
	first, err := PermissionSettings("claude-code", existing)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PermissionSettings("claude-code", first)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("permission overlay was not idempotent: %v", err)
	}
	var value struct {
		Env         map[string]string
		Permissions struct {
			Allow, Deny []string
			DefaultMode string
		}
	}
	if err := json.Unmarshal(first, &value); err != nil {
		t.Fatal(err)
	}
	if value.Env["CUSTOM"] != "yes" || value.Permissions.DefaultMode != "default" || len(value.Permissions.Deny) != 1 || value.Permissions.Deny[0] != "mcp__mindwire_desktop__surface_action" {
		t.Fatal("desktop changed unrelated settings or explicit deny rules")
	}
	if len(value.Permissions.Allow) != 6 || value.Permissions.Allow[0] != "Bash(git status)" {
		t.Fatal("permission overlay lost user rules or allowed extra tools")
	}
	if output, err := PermissionSettings("codex", existing); err != nil || !bytes.Equal(output, existing) {
		t.Fatal("changed another harness's settings")
	}
}

func TestDesktopFrameResizeAndRunIsolation(t *testing.T) {
	s, p, _ := testService(t)
	s.SetApproval(func(context.Context, Actor, string) error { return nil })
	a := Actor{Kind: "agent", RunID: "a"}
	opened := mustOpen(t, s, a, "control")
	frame, err := s.Capture(t.Context(), opened.ID, a)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Capture(t.Context(), opened.ID, Actor{Kind: "agent", RunID: "b"})
	code(t, err, "forbidden")
	p.mu.Lock()
	p.size = Geometry{9, 6, 2}
	p.mu.Unlock()
	_, err = s.Apply(t.Context(), a, pointerRequest(opened, frame))
	code(t, err, "stale_frame")
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.applied != 0 {
		t.Fatal("sent input against obsolete geometry")
	}
}

func TestDesktopPendingApprovalCannotBeBypassed(t *testing.T) {
	s, _, _ := testService(t)
	started := make(chan struct{})
	release := make(chan struct{})
	s.SetApproval(func(ctx context.Context, _ Actor, _ string) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	a := Actor{Kind: "agent", RunID: "a"}
	request := OpenRequest{RequestID: "repeat", Mode: "view"}
	done := make(chan error, 1)
	go func() { _, err := s.Open(t.Context(), a, request); done <- err }()
	<-started
	_, err := s.Open(t.Context(), a, request)
	code(t, err, "approval_pending")
	s.mu.Lock()
	id := ""
	for sessionID := range s.sessions {
		id = sessionID
	}
	s.mu.Unlock()
	_, err = s.Capture(t.Context(), id, a)
	code(t, err, "approval_required")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDesktopUnknownOutcomeIsNeverReplayed(t *testing.T) {
	s, p, db := testService(t)
	user := Actor{Kind: "user"}
	opened := mustOpen(t, s, user, "control")
	p.failure = errors.New("connection lost after write")
	req := ActionRequest{RequestID: "action", SessionID: opened.ID, ControlGeneration: opened.Controller.Generation, Action: Action{Kind: "text", Text: "private input"}}
	receipt, err := s.Apply(t.Context(), user, req)
	if err != nil || receipt.Status != "outcome_unknown" {
		t.Fatalf("uncertain receipt: %v %v", receipt, err)
	}
	_, _ = s.Apply(t.Context(), user, req)
	p.mu.Lock()
	count := p.applied
	p.mu.Unlock()
	if count != 1 {
		t.Fatal("uncertain input was sent again")
	}
	rows, _ := db.SurfaceList("surface_receipt")
	if strings.Contains(string(rows[0]), "private input") || strings.Contains(string(rows[0]), "clipboard-private") {
		t.Fatal("receipt persisted input or clipboard data")
	}
	if err := db.SurfacePut("surface_receipt", "interrupted", savedReceipt{Receipt: Receipt{ID: "interrupted", Status: "dispatching"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	recovered, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	saved, err := recovered.Receipt("interrupted")
	if err != nil || saved.Status != "outcome_unknown" {
		t.Fatalf("restart receipt: %v %v", saved, err)
	}
}

func TestDesktopHeartbeatWhileInputIsBlocked(t *testing.T) {
	s, p, _ := testService(t)
	user := Actor{Kind: "user"}
	opened := mustOpen(t, s, user, "control")
	p.gate = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.Apply(ctx, user, ActionRequest{RequestID: "slow", SessionID: opened.ID, ControlGeneration: opened.Controller.Generation, Action: Action{Kind: "text", Text: "abc"}})
	}()
	deadline := time.Now().Add(time.Second)
	for {
		p.mu.Lock()
		started := p.applied > 0
		p.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("input did not start")
		}
		time.Sleep(time.Millisecond)
	}
	renewed := make(chan error, 1)
	go func() {
		_, err := s.Control(t.Context(), opened.ID, user, ControlRequest{Action: "renew", Generation: opened.Controller.Generation})
		renewed <- err
	}()
	select {
	case err := <-renewed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat blocked behind input")
	}
	close(p.gate)
	<-done
	view := mustOpen(t, s, user, "view")
	if _, err := s.Control(t.Context(), view.ID, user, ControlRequest{Action: "renew"}); err != nil {
		t.Fatal("view session could not renew", err)
	}
}

type tokenTransport struct{ token string }

func (t tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}
func mcpClient(t *testing.T, s *Service, run context.Context, actor Actor, harness string) (*mcp.ClientSession, func()) {
	t.Helper()
	overlay, env, cleanup, err := s.BindRun(run, actor, harness, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(overlay), env[runTokenEnv]) {
		t.Fatal("run token leaked into harness config")
	}
	var cfg struct {
		Servers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(overlay, &cfg); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "desktop-test", Version: "1"}, nil)
	connection, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: cfg.Servers[MCPName].URL, HTTPClient: &http.Client{Transport: tokenTransport{env[runTokenEnv]}, Timeout: 10 * time.Second}}, nil)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	return connection, func() { _ = connection.Close(); cleanup() }
}
func TestDesktopMCPImagesAuthorizationAndCleanup(t *testing.T) {
	s, _, _ := testService(t)
	s.SetApproval(func(context.Context, Actor, string) error { return nil })
	a := Actor{Kind: "agent", RunID: "a", ChatID: "chat"}
	client, closeClient := mcpClient(t, s, t.Context(), a, "codex")
	defer closeClient()
	list, err := client.ListTools(t.Context(), nil)
	if err != nil || len(list.Tools) != 5 {
		t.Fatalf("tools: %v %v", list, err)
	}
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "surface_open", Arguments: map[string]any{"requestId": "one", "mode": "control"}})
	if err != nil || result.IsError {
		t.Fatalf("open: %v %v", result, err)
	}
	var payload struct {
		Surface toolPayload `json:"mindwireSurface"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &payload); err != nil {
		t.Fatal(err)
	}
	opened := payload.Surface.Session
	result, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "surface_capture", Arguments: map[string]any{"sessionId": opened.ID}})
	if err != nil || result.IsError || len(result.Content) != 2 {
		t.Fatalf("capture: %v %v", result, err)
	}
	imageContent, ok := result.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatal("capture is text instead of an MCP image")
	}
	img, err := png.Decode(bytes.NewReader(imageContent.Data))
	if err != nil || img.Bounds().Dx() != 7 {
		t.Fatalf("MCP image: %v", err)
	}
	other, closeOther := mcpClient(t, s, t.Context(), Actor{Kind: "agent", RunID: "b"}, "claude-code")
	defer closeOther()
	denied, err := other.CallTool(t.Context(), &mcp.CallToolParams{Name: "surface_capture", Arguments: map[string]any{"sessionId": opened.ID}})
	if err != nil || !denied.IsError {
		t.Fatal("another run captured this session")
	}
	closeClient()
	if s.Snapshot().Controller != nil {
		t.Fatal("MCP cleanup retained controller")
	}
}

func TestDesktopBindingRefreshKeepsControllerAndNoPublicSecrets(t *testing.T) {
	s, _, db := testService(t)
	creds, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{RegistryID: db.Identity(), WorkspaceID: "163119cc95a4dddc", GatewayToken: "scoped-first",
		Connection: SSHConnection{ExpiresAt: time.Now().Add(8 * time.Hour), SSH: SSHSettings{Host: "ssh.oblien.com", Port: 22, Username: "desktop-163119cc95a4dddc-test", Password: "first-password", HostKeyFingerprint: "SHA256:test"}, VNC: VNCSettings{Host: "127.0.0.1", Port: 5900, Authentication: "none"}}}
	if err := s.Bind(binding, creds); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.snapshot.Controller = &Controller{SessionID: "active-run", Generation: 7}
	s.mu.Unlock()
	binding.Connection.SSH.Password = "phone-grant"
	binding.GatewayToken = "scoped-refreshed"
	if err := s.Bind(binding, creds); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Controller == nil || s.Snapshot().Controller.Generation != 7 {
		t.Fatal("refresh revoked an active controller")
	}
	var saved Binding
	if err := json.Unmarshal([]byte(creds.Get(bindingKey)), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Connection.SSH.Password != "first-password" || saved.GatewayToken != "scoped-refreshed" {
		t.Fatal("did not preserve the service grant while refreshing its JWT")
	}
	public, _ := json.Marshal(s.Snapshot())
	for _, secret := range []string{"first-password", "scoped-refreshed", "phone-grant"} {
		if bytes.Contains(public, []byte(secret)) {
			t.Fatal("secret leaked into public status")
		}
	}
	binding.WorkspaceID = "0000000000000000"
	if err = s.Bind(binding, creds); err == nil {
		t.Fatal("accepted foreign workspace binding")
	}
}
