package surface

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
)

// The workspace daemon uses Oblien's authenticated binary RFB stream. A Mac
// workspace's display is a private QEMU socket in its Linux launcher, not a VNC
// server inside macOS. HTTPS/WSS also works where outbound SSH is unavailable.
// The iOS viewer independently uses the provider's native, pinned SSH tunnel.
func (p *Oblien) desktopSocket(ctx context.Context) (net.Conn, error) {
	if p.binding.GatewayToken == "" {
		return nil, problem("needs_authorization", "Refresh the workspace desktop authorization.")
	}
	endpoint, err := url.Parse(p.base + "/desktop/ws")
	if err != nil {
		return nil, err
	}
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	dialCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(dialCtx, endpoint.String(), &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": {"Bearer " + p.binding.GatewayToken}},
		Subprotocols: []string{"binary"}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		if response != nil {
			if response.Body != nil {
				response.Body.Close()
			}
			if response.StatusCode == 401 || response.StatusCode == 403 {
				return nil, problem("needs_authorization", "Refresh the workspace desktop authorization.")
			}
		}
		return nil, problem("unavailable", "Could not connect to the workspace display. Check that desktop access is enabled.")
	}
	// A successful HTTP request ending must not close this shared desktop stream.
	// RFB operations supply their own deadlines, and Service.Close owns its lifetime.
	stream := websocket.NetConn(context.Background(), conn, websocket.MessageBinary)
	conn.SetReadLimit(16 << 20) // matches the provider's native desktop tunnel SDK
	return &desktopStream{Conn: stream, socket: conn}, nil
}

type desktopStream struct {
	net.Conn
	socket *websocket.Conn
}

func (c *desktopStream) Close() error {
	_ = c.socket.CloseNow()
	return c.Conn.Close() // also release NetConn's deadline timers and contexts
}
