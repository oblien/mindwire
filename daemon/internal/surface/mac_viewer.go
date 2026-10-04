package surface

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"time"

	vnc "github.com/kward/go-vnc"
)

// AuthorizeViewer is used for the existing paired SSH forward grant. A raw
// display cannot outlive its human session or enable desktop access implicitly.
func (s *Service) AuthorizeViewer(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authorizeViewerLocked(sessionID)
}

func (s *Service) authorizeViewerLocked(id string) error {
	select {
	case <-s.stop:
		return problem("disconnected", "Desktop service stopped.")
	default:
	}
	p, ok := s.provider.(*MacDesktop)
	if !ok || !p.settings.Enabled {
		return problem("desktop_setup_required", "Enable desktop access from the Mindwire CLI on your Mac.")
	}
	session := s.sessions[id]
	if session == nil || session.Actor != "user" || !session.authorized || s.now().Sub(session.lastSeen) > 20*time.Second {
		return problem("not_found", "Open a desktop view session before connecting its display.")
	}
	return nil
}

// ServeViewer forwards compressed RFB frames without decoding/re-encoding them.
// The Mac login terminates here. The phone sees None auth only inside its pinned,
// device-authorized SSH channel. Input messages cannot bypass Service.Apply.
func (s *Service) ServeViewer(ctx context.Context, id string, viewer io.ReadWriteCloser) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer viewer.Close()
	s.mu.Lock()
	if err := s.authorizeViewerLocked(id); err != nil {
		s.mu.Unlock()
		return err
	}
	p := s.provider.(*MacDesktop)
	previous := s.viewers[id]
	s.viewers[id] = viewer
	s.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	defer func() {
		s.mu.Lock()
		if s.viewers[id] == viewer {
			delete(s.viewers, id)
		}
		s.mu.Unlock()
	}()
	stop := context.AfterFunc(ctx, func() { _ = viewer.Close() })
	defer stop()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		for {
			s.mu.Lock()
			changed := s.changed
			valid := s.authorizeViewerLocked(id) == nil && s.provider == p && s.viewers[id] == viewer
			s.mu.Unlock()
			if !valid {
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				cancel()
				return
			case <-changed:
			}
		}
	}()
	defer func() { cancel(); <-watchDone }()
	upstream, geometry, err := p.openViewer(ctx)
	if err != nil {
		return err
	}
	defer upstream.Close()
	stopUpstream := context.AfterFunc(ctx, func() { _ = upstream.Close() })
	defer stopUpstream()
	if err = viewerHandshake(ctx, viewer, geometry); err != nil {
		return err
	}
	errors := make(chan error, 2)
	go func() { _, err := io.Copy(viewer, upstream); errors <- err }()
	go func() { errors <- forwardViewerRequests(upstream, viewer) }()
	err = <-errors
	_ = viewer.Close()
	_ = upstream.Close()
	<-errors
	return err
}

func (p *MacDesktop) openViewer(ctx context.Context) (net.Conn, Geometry, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	conn, err := p.dialAuthenticated(ctx)
	if err != nil {
		return nil, Geometry{}, problem("unavailable", "Mac Screen Sharing is unavailable.")
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	cfg := vnc.NewClientConfig("")
	cfg.Auth = p.authentication(conn)
	cfg.Exclusive = false
	client, err := connectDesktopRFB(ctx, conn, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, Geometry{}, macConnectionError(ctx, err)
	}
	geometry := Geometry{Width: int(client.FramebufferWidth()), Height: int(client.FramebufferHeight())}
	if geometry.Width < 1 || geometry.Height < 1 || geometry.Width*geometry.Height > 16<<20 {
		_ = conn.Close()
		return nil, Geometry{}, problem("display_limit", "This display exceeds the supported size.")
	}
	if err = setDesktopPixelFormat(conn); err != nil {
		_ = conn.Close()
		return nil, Geometry{}, err
	}
	if err = ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, Geometry{}, err
	}
	return conn, geometry, nil
}

func viewerHandshake(ctx context.Context, conn io.ReadWriteCloser, geometry Geometry) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if _, err := io.WriteString(conn, "RFB 003.008\n"); err != nil {
		return err
	}
	var version [12]byte
	if _, err := io.ReadFull(conn, version[:]); err != nil {
		return err
	}
	if string(version[:]) != "RFB 003.008\n" {
		return problem("invalid_request", "The desktop viewer requires RFB 3.8.")
	}
	if _, err := conn.Write([]byte{1, 1}); err != nil {
		return err
	}
	var method [1]byte
	if _, err := io.ReadFull(conn, method[:]); err != nil {
		return err
	}
	if method[0] != 1 {
		return problem("invalid_request", "Unsupported desktop viewer authentication.")
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, method[:]); err != nil {
		return err
	}
	// The upstream is always shared, regardless of the viewer's Exclusive flag.
	var header bytes.Buffer
	_ = binary.Write(&header, binary.BigEndian, uint16(geometry.Width))
	_ = binary.Write(&header, binary.BigEndian, uint16(geometry.Height))
	// The library's PixelFormat.Marshal incorrectly rejects the standard 24-bit
	// depth in a 32-bit pixel. Use the same fixed wire format as our decoder.
	if err := binary.Write(&header, binary.BigEndian, vnc.PixelFormat32bit); err != nil {
		return err
	}
	name := "Mindwire desktop"
	_ = binary.Write(&header, binary.BigEndian, uint32(len(name)))
	header.WriteString(name)
	_, err := conn.Write(header.Bytes())
	return err
}

func forwardViewerRequests(upstream io.Writer, viewer io.Reader) error {
	for {
		var kind [1]byte
		if _, err := io.ReadFull(viewer, kind[:]); err != nil {
			return err
		}
		var length int
		switch kind[0] {
		case 0:
			length = 19 // SetPixelFormat
		case 2:
			length = 3 // SetEncodings + bounded array below
		case 3:
			length = 9 // FramebufferUpdateRequest
		case 150:
			length = 9 // EnableContinuousUpdates
		case 248:
			length = 8 // Fence + bounded payload below
		default:
			return problem("forbidden", "Desktop input must use the shared controller.")
		}
		message := make([]byte, length+1)
		message[0] = kind[0]
		if _, err := io.ReadFull(viewer, message[1:]); err != nil {
			return err
		}
		extra := 0
		if kind[0] == 2 {
			count := int(binary.BigEndian.Uint16(message[2:4]))
			if count > 128 {
				return problem("invalid_request", "Too many desktop encodings.")
			}
			extra = count * 4
		} else if kind[0] == 248 {
			extra = int(message[8])
			if extra > 64 {
				return problem("invalid_request", "Desktop fence payload is too large.")
			}
		}
		if extra > 0 {
			payload := make([]byte, extra)
			if _, err := io.ReadFull(viewer, payload); err != nil {
				return err
			}
			message = append(message, payload...)
		}
		if _, err := upstream.Write(message); err != nil {
			return err
		}
	}
}
