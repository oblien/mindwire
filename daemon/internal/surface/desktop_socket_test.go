package surface

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDesktopSocketUsesHeaderAuthAndOutlivesOpenRequest(t *testing.T) {
	fixture := &rfbFixture{}
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/desktop/ws" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer scoped-test-token" {
			t.Error("desktop stream used the wrong route or exposed its token in the URL")
			w.WriteHeader(401)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"binary"}})
		if err != nil {
			t.Error(err)
			return
		}
		defer close(closed)
		fixture.serve(websocket.NetConn(r.Context(), conn, websocket.MessageBinary))
	}))
	defer server.Close()
	provider := &Oblien{base: server.URL, binding: Binding{GatewayToken: "scoped-test-token", Connection: SSHConnection{ExpiresAt: time.Now().Add(time.Hour)}}}
	defer provider.Close()
	request, cancel := context.WithCancel(t.Context())
	if _, err := provider.Connect(request); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel() // the session-open HTTP response has finished
	img, geometry, err := provider.Capture(t.Context())
	if err != nil || geometry.Width != 2 || img == nil {
		t.Fatalf("stream was tied to its opening request: %v", err)
	}
	provider.Close()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("closing the provider retained its display connection")
	}
}

func TestDesktopSocketRejectsExpiredGatewayAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer server.Close()
	provider := &Oblien{base: server.URL, binding: Binding{GatewayToken: "expired-test-token"}}
	_, err := provider.desktopSocket(t.Context())
	code(t, err, "needs_authorization")
}
