package surface

import (
	"context"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	vnc "github.com/kward/go-vnc"
	"github.com/kward/go-vnc/buttons"
	"github.com/kward/go-vnc/encodings"
	"github.com/kward/go-vnc/keys"
	"github.com/kward/go-vnc/messages"
)

func init() {
	// Explicit standard 24-bit true colour. The library's default maxima use the
	// bits-per-pixel value rather than the component depth.
	vnc.PixelFormat32bit = vnc.PixelFormat{BPP: 32, Depth: 24, TrueColor: true, RedMax: 255, GreenMax: 255, BlueMax: 255, RedShift: 16, GreenShift: 8, BlueShift: 0}
	vnc.SetSettle(0)
}

// RFB 3.8 sends SecurityResult even with None authentication. The upstream
// library skips it; its auth extension point lets us consume it correctly. Its
// 3.3 path uses its own None handler, where that result is correctly absent.
type desktopNoneAuth struct{ conn net.Conn }

func (*desktopNoneAuth) SecurityType() uint8 { return 1 }
func (a *desktopNoneAuth) Handshake(*vnc.ClientConn) error {
	var status uint32
	if err := binary.Read(a.conn, binary.BigEndian, &status); err != nil {
		return err
	}
	if status != 0 {
		return errors.New("VNC security negotiation failed")
	}
	return nil
}

type frameReader struct {
	conn            net.Conn
	remainingPixels int
}
type boundedUpdate struct{ reader *frameReader }

func (*boundedUpdate) Type() messages.ServerMessage { return messages.FramebufferUpdate }
func (m *boundedUpdate) Read(c *vnc.ClientConn) (vnc.ServerMessage, error) {
	m.reader.remainingPixels = 16 << 20
	return (&vnc.FramebufferUpdate{}).Read(c)
}

type boundedRaw struct {
	vnc.RawEncoding
	reader *frameReader
}

func (r *boundedRaw) Read(c *vnc.ClientConn, rect *vnc.Rectangle) (vnc.Encoding, error) {
	pixels := int(rect.Width) * int(rect.Height)
	if pixels > r.reader.remainingPixels {
		return nil, errors.New("RFB rectangle exceeds frame limit")
	}
	r.reader.remainingPixels -= pixels
	// SetPixelFormat below fixes this wire format. Read one bounded block instead
	// of upstream's one-byte-at-a-time decoder, which stalls large screenshots.
	data := make([]byte, pixels*4)
	if _, err := io.ReadFull(r.reader.conn, data); err != nil {
		return nil, err
	}
	colors := make([]vnc.Color, pixels)
	for i := range colors {
		colors[i] = vnc.Color{R: uint16(data[i*4+2]), G: uint16(data[i*4+1]), B: uint16(data[i*4])}
	}
	return &vnc.RawEncoding{Colors: colors}, nil
}

// Clipboard observation uses the native provider helper, not unsolicited VNC
// text. Consume bounded server updates without retaining their contents.
type boundedClipboard struct{ conn net.Conn }

func (*boundedClipboard) Type() messages.ServerMessage { return messages.ServerCutText }
func (m *boundedClipboard) Read(*vnc.ClientConn) (vnc.ServerMessage, error) {
	var header [7]byte
	if _, err := io.ReadFull(m.conn, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[3:])
	if size > MaxTextBytes {
		return nil, errors.New("VNC clipboard exceeds limit")
	}
	_, err := io.CopyN(io.Discard, m.conn, int64(size))
	return m, err
}

type rfbClient struct {
	write     sync.Mutex
	mu        sync.Mutex
	conn      net.Conn
	client    *vnc.ClientConn
	frame     *image.RGBA
	size      Geometry
	sequence  int64
	changed   chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	pressed   map[keys.Key]bool
	x, y      uint16
	mask      buttons.Button
}

func newRFB(ctx context.Context, conn net.Conn) (*rfbClient, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	cfg := vnc.NewClientConfig("")
	cfg.Auth = []vnc.ClientAuth{&desktopNoneAuth{conn: conn}}
	cfg.Exclusive = false
	cfg.ServerMessageCh = make(chan vnc.ServerMessage, 4)
	reader := &frameReader{conn: conn}
	cfg.ServerMessages = []vnc.ServerMessage{&boundedUpdate{reader: reader}, &vnc.SetColorMapEntries{}, &vnc.Bell{}, &boundedClipboard{conn: conn}}
	client, err := vnc.Connect(ctx, conn, cfg)
	if err != nil {
		conn.Close()
		return nil, problem("rfb_handshake", "The desktop VNC connection could not be opened.")
	}
	width, height := int(client.FramebufferWidth()), int(client.FramebufferHeight())
	if width < 1 || height < 1 || width*height > 16<<20 {
		conn.Close()
		return nil, problem("display_limit", "The desktop display exceeds the supported size.")
	}
	if err := client.SetPixelFormat(vnc.PixelFormat32bit); err != nil {
		conn.Close()
		return nil, err
	}
	if err := client.SetEncodings(vnc.Encodings{&boundedRaw{reader: reader}, &vnc.DesktopSizePseudoEncoding{}}); err != nil {
		conn.Close()
		return nil, err
	}
	r := &rfbClient{conn: conn, client: client, frame: image.NewRGBA(image.Rect(0, 0, width, height)), size: Geometry{width, height, time.Now().UnixMilli()},
		changed: make(chan struct{}), done: make(chan struct{}), pressed: map[keys.Key]bool{}}
	go func() { _ = client.ListenAndHandle(); r.close(); close(cfg.ServerMessageCh) }()
	go r.receive(cfg.ServerMessageCh)
	return r, nil
}
func (r *rfbClient) receive(messages <-chan vnc.ServerMessage) {
	for message := range messages {
		if !r.alive() {
			continue
		}
		{
			update, ok := message.(*vnc.FramebufferUpdate)
			if !ok {
				continue
			}
			r.mu.Lock()
			invalid := false
			for _, rect := range update.Rects {
				if rect.Enc.Type() == encodings.DesktopSizePseudo {
					w, h := int(rect.Width), int(rect.Height)
					if w < 1 || h < 1 || w*h > 16<<20 {
						invalid = true
						break
					}
					r.size = Geometry{w, h, r.size.Revision + 1}
					r.frame = image.NewRGBA(image.Rect(0, 0, w, h))
					continue
				}
				raw, ok := rect.Enc.(*vnc.RawEncoding)
				if !ok {
					invalid = true
					break
				}
				x, y, w, h := int(rect.X), int(rect.Y), int(rect.Width), int(rect.Height)
				if x+w > r.size.Width || y+h > r.size.Height || len(raw.Colors) != w*h {
					invalid = true
					break
				}
				for i, c := range raw.Colors {
					offset := r.frame.PixOffset(x+i%w, y+i/w)
					r.frame.Pix[offset] = byte(c.R)
					r.frame.Pix[offset+1] = byte(c.G)
					r.frame.Pix[offset+2] = byte(c.B)
					r.frame.Pix[offset+3] = 255
				}
			}
			r.sequence++
			close(r.changed)
			r.changed = make(chan struct{})
			r.mu.Unlock()
			if invalid {
				r.close()
			}
		}
	}
}
func (r *rfbClient) alive() bool {
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}
func (r *rfbClient) geometry() Geometry { r.mu.Lock(); defer r.mu.Unlock(); return r.size }
func (r *rfbClient) close()             { r.closeOnce.Do(func() { close(r.done); _ = r.conn.Close() }) }
func (r *rfbClient) capture(ctx context.Context) (image.Image, Geometry, error) {
	return r.observe(ctx, true)
}
func (r *rfbClient) refreshGeometry(ctx context.Context) (Geometry, error) {
	_, geometry, err := r.observe(ctx, false)
	return geometry, err
}

// A one-pixel nonincremental request also elicits pending DesktopSize updates.
// Validate pointer geometry without transferring a second full viewer stream.
func (r *rfbClient) requestFrame(size Geometry, full bool) error {
	width, height := uint16(1), uint16(1)
	if full {
		width, height = uint16(size.Width), uint16(size.Height)
	}
	r.write.Lock()
	defer r.write.Unlock()
	return r.client.FramebufferUpdateRequest(false, 0, 0, width, height)
}
func (r *rfbClient) observe(ctx context.Context, full bool) (image.Image, Geometry, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, r.close)
	defer stop()
	r.mu.Lock()
	sequence := r.sequence
	size := r.size
	r.mu.Unlock()
	err := r.requestFrame(size, full)
	if err != nil {
		r.close()
		return nil, Geometry{}, err
	}
	for {
		r.mu.Lock()
		if r.size.Revision != size.Revision {
			size = r.size
			sequence = r.sequence
			r.mu.Unlock()
			err := r.requestFrame(size, full)
			if err != nil {
				return nil, Geometry{}, err
			}
			continue
		}
		if r.sequence > sequence {
			if !full {
				g := r.size
				r.mu.Unlock()
				return nil, g, nil
			}
			img := image.NewRGBA(r.frame.Rect)
			copy(img.Pix, r.frame.Pix)
			g := r.size
			r.mu.Unlock()
			return img, g, nil
		}
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			r.close()
			return nil, Geometry{}, ctx.Err()
		case <-r.done:
			return nil, Geometry{}, problem("disconnected", "The VNC display disconnected.")
		case <-changed:
		}
	}
}
func (r *rfbClient) withWrite(ctx context.Context, fn func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r.write.Lock()
	defer r.write.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, r.close)
	defer stop()
	if !r.alive() {
		return problem("disconnected", "The VNC display disconnected.")
	}
	return fn()
}
func (r *rfbClient) pointer(mask int, x, y int) error {
	r.x, r.y, r.mask = uint16(x), uint16(y), buttons.Button(mask)
	return r.client.PointerEvent(r.mask, r.x, r.y)
}
func (r *rfbClient) release(ctx context.Context) error {
	return r.withWrite(ctx, func() error { return r.releaseLocked() })
}
func (r *rfbClient) releaseLocked() error {
	var first error
	for key := range r.pressed {
		if err := r.client.KeyEvent(key, false); first == nil {
			first = err
		}
		delete(r.pressed, key)
	}
	if err := r.client.PointerEvent(0, r.x, r.y); first == nil {
		first = err
	}
	r.mask = 0
	return first
}
func (r *rfbClient) key(key keys.Key, down bool) error {
	if down {
		r.pressed[key] = true
	} else {
		delete(r.pressed, key)
	}
	return r.client.KeyEvent(key, down)
}
func parseKey(name string) (keys.Key, error) {
	upper := strings.ToUpper(name)
	if len(upper) >= 2 && upper[0] == 'F' {
		if number, err := strconv.Atoi(upper[1:]); err == nil && number >= 1 && number <= 24 && upper == "F"+strconv.Itoa(number) {
			return keys.Key(0xffbd + number), nil
		}
	}
	switch strings.ToLower(name) {
	case "ctrl", "control":
		return keys.Key(0xffe3), nil
	case "shift":
		return keys.Key(0xffe1), nil
	case "alt", "option":
		return keys.Key(0xffe9), nil
	case "meta", "command", "cmd":
		return keys.Key(0xffe7), nil
	case "super":
		return keys.Key(0xffeb), nil
	case "enter", "return":
		return keys.Key(0xff0d), nil
	case "tab":
		return keys.Key(0xff09), nil
	case "escape", "esc":
		return keys.Key(0xff1b), nil
	case "backspace":
		return keys.Key(0xff08), nil
	case "delete":
		return keys.Key(0xffff), nil
	case "space":
		return keys.Key(0x20), nil
	case "up":
		return keys.Key(0xff52), nil
	case "down":
		return keys.Key(0xff54), nil
	case "left":
		return keys.Key(0xff51), nil
	case "right":
		return keys.Key(0xff53), nil
	case "home":
		return keys.Key(0xff50), nil
	case "end":
		return keys.Key(0xff57), nil
	case "pageup":
		return keys.Key(0xff55), nil
	case "pagedown":
		return keys.Key(0xff56), nil
	case "capslock":
		return keys.Key(0xffe5), nil
	case "insert":
		return keys.Key(0xff63), nil
	case "printscreen":
		return keys.Key(0xff61), nil
	case "pause":
		return keys.Key(0xff13), nil
	case "menu":
		return keys.Key(0xff67), nil
	case "numlock":
		return keys.Key(0xff7f), nil
	case "scrolllock":
		return keys.Key(0xff14), nil
	}
	runes := []rune(name)
	if len(runes) == 1 {
		if runes[0] <= 255 {
			return keys.Key(runes[0]), nil
		}
		return keys.Key(0x01000000 | runes[0]), nil
	}
	return 0, problem("invalid_request", "Unknown desktop key.")
}
func (r *rfbClient) chordLocked(names []string) error {
	parsed := make([]keys.Key, len(names))
	for i, name := range names {
		key, err := parseKey(name)
		if err != nil {
			return err
		}
		parsed[i] = key
	}
	defer r.releaseLocked()
	for _, key := range parsed {
		if err := r.key(key, true); err != nil {
			return err
		}
	}
	for i := len(parsed) - 1; i >= 0; i-- {
		if err := r.key(parsed[i], false); err != nil {
			return err
		}
	}
	return nil
}
func (r *rfbClient) chord(ctx context.Context, names []string) error {
	return r.withWrite(ctx, func() error { return r.chordLocked(names) })
}
func (r *rfbClient) typeText(ctx context.Context, text string, usOnly bool) error {
	if len(text) > 4096 {
		return problem("text_limit", "Use the desktop clipboard to enter longer text.")
	}
	if usOnly {
		for _, c := range text {
			if c < 32 || c > 126 {
				return problem("login_required", "Sign in to the desktop before entering Unicode or multiline text.")
			}
		}
	}
	return r.withWrite(ctx, func() error {
		defer r.releaseLocked()
		for _, c := range text {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := string(c)
			shift := false
			if usOnly {
				const upper = "~!@#$%^&*()_+{}|:\"<>?"
				const lower = "`1234567890-=[]\\;',./"
				if i := strings.IndexRune(upper, c); i >= 0 {
					name = string(lower[i])
					shift = true
				}
				if unicode.IsUpper(c) {
					name = strings.ToLower(name)
					shift = true
				}
			}
			if c == '\n' {
				name = "Enter"
			}
			if c == '\t' {
				name = "Tab"
			}
			names := []string{name}
			if shift {
				names = []string{"Shift", name}
			}
			if err := r.chordLocked(names); err != nil {
				return err
			}
		}
		return nil
	})
}
func (r *rfbClient) apply(ctx context.Context, a Action) error {
	return r.withWrite(ctx, func() error {
		switch a.Kind {
		case "release":
			return r.releaseLocked()
		case "key":
			return r.chordLocked(a.Keys)
		case "pointer":
			return r.pointer(a.Buttons, *a.X, *a.Y)
		case "click", "drag":
			button := 1
			switch a.Button {
			case "", "left":
			case "right":
				button = 4
			case "middle":
				button = 2
			default:
				return problem("invalid_request", "Choose left, middle or right mouse button.")
			}
			defer func() { _ = r.pointer(0, int(r.x), int(r.y)) }()
			count := a.Count
			if count < 1 {
				count = 1
			}
			for i := 0; i < count; i++ {
				if err := r.pointer(button, *a.X, *a.Y); err != nil {
					return err
				}
				if a.Kind == "drag" {
					if a.ToX == nil || a.ToY == nil {
						return problem("invalid_request", "A drag needs destination coordinates.")
					}
					for step := 1; step <= 8; step++ {
						if err := ctx.Err(); err != nil {
							return err
						}
						x := *a.X + (*a.ToX-*a.X)*step/8
						y := *a.Y + (*a.ToY-*a.Y)*step/8
						if err := r.pointer(button, x, y); err != nil {
							return err
						}
					}
				}
				if err := r.pointer(0, int(r.x), int(r.y)); err != nil {
					return err
				}
			}
			return nil
		case "scroll":
			for _, axis := range []struct{ delta, positive, negative int }{{a.DeltaY, 16, 8}, {a.DeltaX, 64, 32}} {
				steps := axis.delta
				mask := axis.positive
				if steps < 0 {
					steps = -steps
					mask = axis.negative
				}
				for i := 0; i < steps; i++ {
					if err := r.pointer(mask, *a.X, *a.Y); err != nil {
						return err
					}
					if err := r.pointer(0, *a.X, *a.Y); err != nil {
						return err
					}
				}
			}
			return nil
		default:
			return problem("unsupported", "The desktop action is unavailable.")
		}
	})
}
