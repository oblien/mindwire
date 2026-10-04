package surface

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
)

// Creates its own disposable virtual desktop. Never sends input to a saved
// user desktop or the main console. The fixture holds only a runtime token.
func TestProjectDesktopOblienLive(t *testing.T) {
	fixturePath := os.Getenv("MINDWIRE_PROJECT_DESKTOP_FIXTURE")
	if fixturePath == "" {
		t.Skip("opt in with a private workspace runtime fixture")
	}
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		WorkspaceID string `json:"workspaceId"`
		Token       string `json:"token"`
	}
	if json.Unmarshal(data, &fixture) != nil || fixture.Token == "" {
		t.Fatal("invalid private fixture")
	}
	root := filepath.Dir(fixturePath)
	db, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Import(registry.Import{Projects: []registry.Project{{Record: registry.Record{ID: "verification"}, Name: "Mindwire desktop verification", Path: t.TempDir()}}}); err != nil {
		t.Fatal(err)
	}
	creds, err := session.Open(filepath.Join(t.TempDir(), "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.BindRuntime(RuntimeBinding{RegistryID: db.Identity(), WorkspaceID: fixture.WorkspaceID, GatewayToken: fixture.Token}, creds); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	list, err := s.ListDesktops(ctx, "verification")
	if err != nil {
		t.Fatal(err)
	}
	if list.Capabilities.Mode != "virtual" || !list.Capabilities.CanCreate {
		t.Skip("this workspace cannot create a disposable virtual desktop")
	}
	request := CreateDesktopRequest{RequestID: newID(), ProjectID: "verification", Name: "Mindwire project desktop verification", Resolution: &DesktopResolution{960, 640}}
	desktop, err := s.CreateDesktop(ctx, request)
	if err != nil {
		desktop, err = s.CreateDesktop(ctx, request)
	}
	if err != nil {
		t.Fatal(err)
	}
	if desktop.ID == "console" {
		t.Fatal("provider returned the shared console for a virtual desktop")
	}
	receipt, _ := json.Marshal(map[string]string{"workspaceId": fixture.WorkspaceID, "desktopId": desktop.ID})
	if err = os.WriteFile(filepath.Join(root, "live-desktop-receipt.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.CloseRun("live-agent")
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := s.DesktopOperation(cleanup, desktop.ID, "delete", nil); err != nil {
			t.Errorf("remove disposable desktop %s: %v", desktop.ID, err)
		}
	}()
	var approvals atomic.Int32
	s.SetApproval(func(context.Context, Actor, string) error { approvals.Add(1); return nil })
	actor := Actor{Kind: "agent", Name: "Desktop verification", RunID: "live-agent", ProjectID: "verification"}
	client, finish := mcpClient(t, s, ctx, actor, "codex")
	defer finish()
	call := func(name string, args any) toolPayload {
		t.Helper()
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		payload := readToolPayload(t, result, err)
		if result.IsError {
			t.Fatalf("%s: %v", name, payload.Error)
		}
		return payload
	}
	opened := call("surface_project_desktop", map[string]any{"requestId": "live-open", "mode": "control"})
	if opened.DesktopID != desktop.ID || opened.Session == nil {
		t.Fatal("agent opened another desktop")
	}
	viewer, err := s.Open(ctx, Actor{Kind: "user"}, OpenRequest{RequestID: "live-phone", Mode: "view", DesktopID: desktop.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer s.CloseSession(viewer.ID, Actor{Kind: "user"})
	if viewer.DesktopID != opened.Session.DesktopID {
		t.Fatal("viewer and agent are on different displays")
	}
	frame := call("surface_capture", map[string]any{"sessionId": opened.Session.ID})
	if frame.Capture == nil || frame.Capture.Geometry.Width != 960 || frame.Capture.Geometry.Height != 640 {
		t.Fatalf("wrong created resolution: %+v", frame.Capture)
	}
	before, err := s.artifacts.Get(frame.Capture.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	input := func(action Action) {
		t.Helper()
		call("surface_action", ActionRequest{RequestID: newID(), SessionID: opened.Session.ID, ControlGeneration: opened.Session.Controller.Generation, Action: action})
	}
	input(Action{Kind: "key", Keys: []string{"Alt", "F2"}})
	time.Sleep(700 * time.Millisecond)
	input(Action{Kind: "text", Text: "mindwire-saved-desktop-input-test"})
	time.Sleep(500 * time.Millisecond)
	frame = call("surface_capture", map[string]any{"sessionId": opened.Session.ID})
	after, err := s.artifacts.Get(frame.Capture.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before.Data, after.Data) {
		t.Fatal("desktop did not change after opening the run dialog and typing")
	}
	if err = os.WriteFile(filepath.Join(root, "live-desktop-input.png"), after.Data, 0600); err != nil {
		t.Fatal(err)
	}
	input(Action{Kind: "key", Keys: []string{"Escape"}})
	call("surface_release", map[string]any{"sessionId": opened.Session.ID})
	if approvals.Load() != 1 {
		t.Fatal("duplicate permission")
	}
	_ = s.CloseSession(viewer.ID, Actor{Kind: "user"})
	if _, err = s.DesktopOperation(ctx, desktop.ID, "stop", nil); err != nil {
		t.Fatal(err)
	}
	for {
		result, err := s.DesktopOperation(ctx, desktop.ID, "get", nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Session.State == "stopped" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	s.Close()
	restored, err := NewConfigured(db, creds)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	resumed, err := restored.EnsureProjectDesktop(ctx, "verification")
	if err != nil || resumed.ID != desktop.ID {
		t.Fatalf("restart did not reuse the saved desktop: %s %v", resumed.ID, err)
	}
	for !resumed.Available {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
		result, err := restored.DesktopOperation(ctx, desktop.ID, "get", nil)
		if err != nil {
			t.Fatal(err)
		}
		resumed = result.Session
		if resumed.State == "failed" {
			t.Fatal("saved desktop did not restart")
		}
	}
	t.Logf("LIVE_PROJECT_DESKTOP %s: created at 960x640, MCP screenshot/input, shared viewer, stop/start and durable reassociation passed", desktop.ID)
}
