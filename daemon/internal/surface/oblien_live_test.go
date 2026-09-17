package surface

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

// Uses an explicitly selected workspace and its short-lived desktop-only grant.
// Obtain the fixture with WorkspaceDesktopLiveTests on the signed-in iPhone.
func TestOblienDesktopLive(t *testing.T) {
	path := os.Getenv("MINDWIRE_DESKTOP_FIXTURE")
	if path == "" {
		t.Skip("opt in with a private desktop grant fixture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		WorkspaceID string        `json:"workspaceId"`
		Token       string        `json:"token"`
		Connection  SSHConnection `json:"connection"`
	}
	if json.Unmarshal(data, &fixture) != nil {
		t.Fatal("invalid private fixture")
	}
	p, err := NewOblien(Binding{WorkspaceID: fixture.WorkspaceID, GatewayToken: fixture.Token, Connection: fixture.Connection})
	if err != nil {
		t.Fatal(err)
	}
	db, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Configure(p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	status, err := s.Refresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LIVE_DESKTOP supported=%v enabled=%v available=%v os=%s clipboard=%v", status.Supported, status.Enabled, status.Available, status.OS, status.Capabilities.ClipboardRead)
	user := Actor{Kind: "user", Name: "Desktop verification"}
	session, err := s.Open(ctx, user, OpenRequest{RequestID: "live-open", Mode: "control"})
	if err != nil {
		t.Fatal(err)
	}
	heartbeat, stopHeartbeat := context.WithCancel(ctx)
	var heartbeats sync.WaitGroup
	heartbeats.Add(1)
	go func() {
		defer heartbeats.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeat.Done():
				return
			case <-ticker.C:
				_, _ = s.Control(heartbeat, session.ID, user, ControlRequest{Action: "renew", Generation: session.Controller.Generation})
			}
		}
	}()
	defer func() { stopHeartbeat(); heartbeats.Wait() }()
	capture, err := s.Capture(ctx, session.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(input Action) Receipt {
		t.Helper()
		receipt, err := s.Apply(ctx, user, ActionRequest{RequestID: newID(), SessionID: session.ID, ControlGeneration: session.Controller.Generation, GeometryRevision: s.Snapshot().Geometry.Revision, Action: input})
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Error != nil {
			t.Fatal(receipt.Error)
		}
		return receipt
	}
	if os.Getenv("MINDWIRE_DESKTOP_INPUT") == "1" {
		// Wake the display without typing into, opening or editing an application.
		x, y := 10, 10
		apply(Action{Kind: "pointer", X: &x, Y: &y})
		apply(Action{Kind: "key", Keys: []string{"Shift"}})
		time.Sleep(500 * time.Millisecond)
		capture, err = s.Capture(ctx, session.ID, user)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("LIVE_DESKTOP pointer and keyboard acknowledged")
	}
	if os.Getenv("MINDWIRE_DESKTOP_SIGNIN") == "1" {
		// Only for the observed login screen in the explicitly authorized test workspace.
		var credentials struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := p.json(ctx, "GET", "/desktop/credentials", nil, &credentials); err != nil {
			t.Fatal(err)
		}
		if credentials.Password == "" {
			t.Fatal("no desktop login password provided")
		}
		x, y := 640, 718
		apply(Action{Kind: "click", X: &x, Y: &y})
		apply(Action{Kind: "text", Text: credentials.Password})
		apply(Action{Kind: "key", Keys: []string{"Enter"}})
		time.Sleep(3 * time.Second)
		capture, err = s.Capture(ctx, session.ID, user)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("LIVE_DESKTOP sign-in input acknowledged (credential omitted)")
	}
	if os.Getenv("MINDWIRE_DESKTOP_CLIPBOARD") == "1" {
		original := apply(Action{Kind: "clipboard_read"}).Text
		defer func() { _, _ = p.Apply(context.Background(), Action{Kind: "clipboard_write", Text: original}) }()
		want := "Mindwire desktop verification — مرحبا\nSecond line"
		apply(Action{Kind: "clipboard_write", Text: want})
		if apply(Action{Kind: "clipboard_read"}).Text != want {
			t.Fatal("clipboard Unicode/multiline roundtrip differed")
		}
		apply(Action{Kind: "clipboard_write", Text: original})
		t.Log("LIVE_DESKTOP Unicode clipboard roundtrip passed and previous clipboard restored")
	}
	content, err := s.Artifacts().Get(capture.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("MINDWIRE_DESKTOP_FRAME"); output != "" {
		if err = os.WriteFile(output, content.Data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("LIVE_DESKTOP saved PNG %dx%d bytes=%d", content.Width, content.Height, len(content.Data))
}
