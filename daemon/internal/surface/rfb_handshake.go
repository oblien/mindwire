package surface

import (
	"bytes"
	"context"
	"errors"
	"net"

	vnc "github.com/kward/go-vnc"
)

// The pinned go-vnc revision writes the RGB maxima in SetPixelFormat using
// little-endian rather than RFB's network byte order. Connect sends this message
// itself: correcting the format afterwards is too late for strict servers such
// as TigerVNC, which have already closed the connection.
//
// Its handshake writes Raw SetEncodings, then one complete SetPixelFormat. Limit
// the compatibility correction to that pair, before returning the connection.
// Authentication writes and subsequent native viewer traffic are untouched.
// Remove this adapter when go-vnc's encoding.PixelFormatWire is fixed upstream.
type rfbHandshakeConn struct {
	net.Conn
	expectPixelFormat bool
	corrected         bool
}

func (c *rfbHandshakeConn) Write(data []byte) (int, error) {
	if c.corrected {
		return c.Conn.Write(data)
	}
	if c.expectPixelFormat {
		if len(data) != 20 || !bytes.Equal(data[:4], []byte{0, 0, 0, 0}) {
			return 0, errors.New("unexpected VNC pixel format handshake")
		}
		var wire [20]byte
		copy(wire[:], data)
		for offset := 8; offset < 14; offset += 2 {
			wire[offset], wire[offset+1] = wire[offset+1], wire[offset]
		}
		c.corrected = true
		return c.Conn.Write(wire[:])
	}
	if bytes.Equal(data, []byte{2, 0, 0, 1, 0, 0, 0, 0}) {
		c.expectPixelFormat = true
	}
	return c.Conn.Write(data)
}

func connectDesktopRFB(ctx context.Context, conn net.Conn, cfg *vnc.ClientConfig) (*vnc.ClientConn, error) {
	handshake := &rfbHandshakeConn{Conn: conn}
	client, err := vnc.Connect(ctx, handshake, cfg)
	if err != nil {
		return nil, err
	}
	if !handshake.corrected {
		_ = conn.Close()
		return nil, errors.New("VNC pixel format was not negotiated")
	}
	return client, nil
}
