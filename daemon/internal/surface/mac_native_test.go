package surface

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/computer"
)

// Opt-in simulator fixture: the real paired SSH transport, surface controller,
// ARD authentication and frame proxy talk only to a synthetic RFB desktop.
// Neither this fixture nor its test-only controls can touch the host's screen.
func TestNativeMacDesktopFixture(t *testing.T) {
	file := os.Getenv("MINDWIRE_MAC_NATIVE_FIXTURE_FILE")
	if file == "" {
		t.Skip("opt in with the iOS Mac desktop integration test")
	}
	s, store, frames, _ := macTestService(t)
	live := os.Getenv("MINDWIRE_MAC_PERFORMANCE_STATE_DIR") != ""
	if live {
		// The read-only performance test uses existing, explicitly enabled access.
		// Keep the real login only in memory, never in this fixture's state files.
		desktop := performanceMac(t)
		s.configureProvider(desktop)
		// Close the read-only input transport before Service.Close's generic
		// release. With no controller this test must send no pointer event,
		// including a cleanup mouse-up at the connection's initial position.
		t.Cleanup(func() { _ = desktop.Close() })
	}
	frames.resize = true
	// A six-MiB frame crosses the native forwarder's write high-water mark.
	// Tiny RFB fixtures miss stalls when NIOSSH reads resume after backpressure.
	frames.largeFrame = true
	frames.tiledFrame = true
	s.SetApproval(func(context.Context, Actor, string) error { return nil })
	directory := t.TempDir()
	const token = "synthetic-mac-desktop-fixture"
	mux := http.NewServeMux()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	defer api.Close()
	paired, err := computer.New(directory, api.Listener.Addr().String(), token, s.db.Identity())
	if err != nil {
		t.Fatal(err)
	}
	paired.SetDesktopViewer(DesktopPort, s.AuthorizeViewer, s.ServeViewer)
	if err = paired.Start("127.0.0.1:0", "127.0.0.1:0", directory); err != nil {
		t.Fatal(err)
	}
	defer paired.Close()
	paired.Register(mux)
	respond := desktopFixtureRespond
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"ok": true, "version": "fixture", "surfaceProtocolVersion": Version, "localDesktopVersion": LocalDesktopVersion}, nil)
	})
	mux.HandleFunc("GET /workspace/host", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"hostname": "Test Mac", "os": "darwin", "arch": "arm64", "home": directory, "directory": directory, "pid": os.Getpid()}, nil)
	})
	registerDesktopFixtureRoutes(mux, s)
	mux.HandleFunc("POST /fixture/enable", func(w http.ResponseWriter, r *http.Request) {
		value, err := s.ConfigureLocalDesktop(r.Context(), LocalDesktopSettings{Enabled: true, Username: "owner", Password: " test password Ω "}, store)
		respond(w, value, err)
	})
	mux.HandleFunc("POST /fixture/disable", func(w http.ResponseWriter, r *http.Request) {
		value, err := s.ConfigureLocalDesktop(r.Context(), LocalDesktopSettings{}, store)
		respond(w, value, err)
	})
	mux.HandleFunc("POST /fixture/drop-video", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		for _, viewer := range s.viewers {
			_ = viewer.Close()
		}
		s.mu.Unlock()
		respond(w, map[string]bool{"ok": true}, nil)
	})
	mux.HandleFunc("GET /fixture/state", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		sessions, viewers := len(s.sessions), len(s.viewers)
		s.mu.Unlock()
		frames.mu.Lock()
		presses := 0
		pointers := make([][]int, 0)
		for _, input := range frames.inputs {
			if input.kind == 4 && input.data[0] == 1 {
				presses++
			}
			if input.kind == 5 && len(input.data) == 5 {
				pointers = append(pointers, []int{int(input.data[0]), int(input.data[1])<<8 | int(input.data[2]), int(input.data[3])<<8 | int(input.data[4])})
			}
		}
		formats16, formats24 := frames.formats16, frames.formats24
		frames.mu.Unlock()
		respond(w, map[string]any{"sessions": sessions, "viewers": viewers, "keyPresses": presses,
			"formats16": formats16, "formats24": formats24, "pointerEvents": pointers}, nil)
	})
	runtimeData, err := os.ReadFile(filepath.Join(directory, "computer-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	var runtime struct{ SSHPort, WebsocketPort int }
	if err = json.Unmarshal(runtimeData, &runtime); err != nil {
		t.Fatal(err)
	}
	routes := []computer.Route{
		{Kind: "ssh", Host: "127.0.0.1", Port: runtime.SSHPort},
		{Kind: "websocket", URL: "ws://127.0.0.1:" + fmt.Sprint(runtime.WebsocketPort) + "/ssh"},
	}
	var invitations []computer.Invitation
	for _, route := range routes {
		inv, err := paired.Invite([]computer.Route{route})
		if err != nil {
			t.Fatal(err)
		}
		invitations = append(invitations, inv)
	}
	data, _ := json.Marshal(map[string]any{"invitations": invitations, "live": live})
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(8 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(file + ".done"); err == nil {
				return
			}
			for _, invitation := range invitations {
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, httptest.NewRequest("GET", "/computer/pairings/"+invitation.PairingID, nil))
				var offer computer.Offer
				if json.Unmarshal(response.Body.Bytes(), &offer) == nil && offer.Status == "pending" && offer.Request != nil {
					if _, err := paired.Decide(invitation.PairingID, offer.Request.ID, true); err != nil {
						t.Error(err)
					}
				}
			}
		case <-deadline.C:
			t.Fatal("native Mac desktop fixture timed out")
		}
	}
}
