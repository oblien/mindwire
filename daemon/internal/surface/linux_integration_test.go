package surface

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Opt in with the disposable TigerVNC/Tk display in testdata/linux. Unlike an
// RFB receipt, the X11 window confirms that Linux actually received each input.
func TestLinuxDesktopInput(t *testing.T) {
	address, stateURL := os.Getenv("MINDWIRE_LINUX_VNC_ADDRESS"), os.Getenv("MINDWIRE_LINUX_DESKTOP_STATE")
	if address == "" || stateURL == "" {
		t.Skip("requires the isolated testdata/linux desktop")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		t.Fatal("the Linux test display must be bound to loopback")
	}
	type state struct {
		Fixture string   `json:"fixture"`
		Ready   bool     `json:"ready"`
		Pointer [2]int   `json:"pointer"`
		Presses []int    `json:"presses"`
		Keys    []string `json:"keys"`
		Text    string   `json:"text"`
	}
	httpClient := &http.Client{Timeout: 3 * time.Second}
	readState := func() state {
		t.Helper()
		response, err := httpClient.Get(stateURL)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var current state
		if err = json.NewDecoder(response.Body).Decode(&current); err != nil {
			t.Fatal(err)
		}
		if current.Fixture != "mindwire-linux-desktop-v1" {
			t.Fatal("refusing input without the disposable test window")
		}
		return current
	}
	waitFor := func(label string, condition func(state) bool) state {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			current := readState()
			if condition(current) {
				return current
			}
			if time.Now().After(deadline) {
				t.Fatalf("Linux did not receive %s: %+v", label, current)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	initial := waitFor("test window", func(s state) bool { return s.Ready })

	// Exercise the same HTTP status and binary WebSocket path as Oblien. The
	// upstream here is a real Linux VNC server rather than a synthetic RFB peer.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/desktop/status":
			_ = json.NewEncoder(w).Encode(ProviderStatus{Supported: true, Enabled: true, Available: true})
		case "/runtimes":
			_, _ = io.WriteString(w, `{"default_target":"local","targets":[{"id":"local","runtime":{"os":"linux"}}]}`)
		case "/desktop/ws":
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"binary"}})
			if err != nil {
				return
			}
			defer conn.CloseNow()
			upstream, err := (&net.Dialer{}).DialContext(r.Context(), "tcp", address)
			if err != nil {
				return
			}
			defer upstream.Close()
			stream := websocket.NetConn(r.Context(), conn, websocket.MessageBinary)
			defer stream.Close()
			done := make(chan struct{})
			go func() { defer close(done); _, _ = io.Copy(upstream, stream); _ = upstream.Close() }()
			_, _ = io.Copy(stream, upstream)
			_ = conn.CloseNow()
			<-done
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider := &Oblien{base: server.URL, http: server.Client(), binding: Binding{
		GatewayToken: "isolated-linux-test", Connection: SSHConnection{ExpiresAt: time.Now().Add(time.Hour)},
	}}
	s, _, _ := testService(t)
	defer s.Close()
	if err := s.Configure(provider); err != nil {
		t.Fatal(err)
	}
	user := Actor{Kind: "user", Name: "Linux desktop test"}
	view := mustOpen(t, s, user, "control")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	apply := func(input Action) ActionRequest {
		t.Helper()
		request := ActionRequest{RequestID: newID(), SessionID: view.ID, ControlGeneration: view.Controller.Generation,
			GeometryRevision: s.Snapshot().Geometry.Revision, Action: input}
		receipt, err := s.Apply(ctx, user, request)
		if err != nil || receipt.Error != nil {
			t.Fatalf("%s: %v %v", input.Kind, err, receipt.Error)
		}
		return request
	}
	x, y := 300, 220
	// No initial capture: this is the native app's first-click path.
	click := apply(Action{Kind: "click", X: &x, Y: &y})
	clicked := waitFor("first click", func(s state) bool {
		return len(s.Presses) == len(initial.Presses)+1 && s.Presses[len(s.Presses)-1] == 1
	})
	if _, err := s.Apply(ctx, user, click); err != nil {
		t.Fatal(err)
	}
	// Reset only the verified disposable text window so the check is repeatable.
	apply(Action{Kind: "key", Keys: []string{"Ctrl", "Home"}})
	apply(Action{Kind: "key", Keys: []string{"Ctrl", "Shift", "End"}})
	apply(Action{Kind: "key", Keys: []string{"Backspace"}})
	waitFor("cleared test text", func(s state) bool { return s.Text == "" })
	apply(Action{Kind: "text", TextMode: "keyboard", Text: "Mindwire Linux test"})
	typed := waitFor("keyboard text", func(s state) bool { return s.Text == "Mindwire Linux test" })
	if len(typed.Presses) != len(clicked.Presses) {
		t.Fatal("retry duplicated the click")
	}
	apply(Action{Kind: "key", Keys: []string{"Left"}})
	waitFor("arrow key", func(s state) bool { return slices.Contains(s.Keys, "Left") })
	x, y = 500, 350
	apply(Action{Kind: "pointer", X: &x, Y: &y})
	waitFor("pointer movement", func(s state) bool { return s.Pointer == [2]int{x, y} })
	apply(Action{Kind: "scroll", X: &x, Y: &y, DeltaY: 1})
	waitFor("scroll wheel", func(s state) bool { return slices.Contains(s.Presses, 5) })
	if _, err := s.Capture(ctx, view.ID, user); err != nil {
		t.Fatal(err)
	}
	t.Log("Linux received first click, exact text, arrow key, pointer and scroll; receipts prevented duplicate input")
	if file := os.Getenv("MINDWIRE_LINUX_NATIVE_FIXTURE_FILE"); file != "" {
		if err := s.CloseSession(view.ID, user); err != nil {
			t.Fatal(err)
		}
		serveLinuxNativeFixture(t, s, address, stateURL, file)
	}
}

// The native iOS test reuses the real Linux provider and service above. Only
// loopback listeners and the disposable X11 window are reachable from this fixture.
func serveLinuxNativeFixture(t *testing.T, s *Service, address, stateURL, file string) {
	t.Helper()
	mux := http.NewServeMux()
	registerDesktopFixtureRoutes(mux, s)
	mux.HandleFunc("GET /fixture/state", func(w http.ResponseWriter, r *http.Request) {
		request, err := http.NewRequestWithContext(r.Context(), "GET", stateURL, nil)
		if err != nil {
			desktopFixtureRespond(w, nil, err)
			return
		}
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Do(request)
		if err != nil {
			desktopFixtureRespond(w, nil, err)
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, io.LimitReader(response.Body, 1<<20))
	})
	token := newID()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	defer api.Close()
	_, port, _ := net.SplitHostPort(address)
	vncPort, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"apiURL": api.URL, "token": token, "vncPort": vncPort})
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file)
	deadline := time.NewTimer(6 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(file + ".done"); err == nil {
				return
			}
		case <-deadline.C:
			t.Fatal("native Linux desktop fixture timed out")
		}
	}
}
