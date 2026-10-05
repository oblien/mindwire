package surface

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"io"

	vnc "github.com/kward/go-vnc"
	"github.com/kward/go-vnc/encodings"
)

// Cursor is a small, immutable remote cursor image, not a screenshot. Its ID
// changes only when the pixels or hotspot change; moving it sends no new image.
type Cursor struct {
	ID       string `json:"id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	HotspotX int    `json:"hotspotX"`
	HotspotY int    `json:"hotspotY"`
	PNG      string `json:"png"`
}

const cursorEncodingType encodings.Encoding = -239 // RFB RichCursor
const maxCursorSize = 256

type cursorEncoding struct {
	reader *frameReader
	cursor *Cursor
}

func (*cursorEncoding) Type() encodings.Encoding { return cursorEncodingType }
func (*cursorEncoding) String() string           { return "RichCursor" }
func (*cursorEncoding) Marshal() ([]byte, error) { return nil, nil }
func (e *cursorEncoding) Read(_ *vnc.ClientConn, rect *vnc.Rectangle) (vnc.Encoding, error) {
	w, h := int(rect.Width), int(rect.Height)
	if w > maxCursorSize || h > maxCursorSize || (w > 0 && int(rect.X) >= w) || (h > 0 && int(rect.Y) >= h) {
		return nil, errors.New("RFB cursor exceeds limit")
	}
	if w == 0 || h == 0 {
		// TigerVNC sends an empty cursor when another connection moves the mouse
		// and starts rendering it into that viewer's pixels. Keep our last shape.
		return &cursorEncoding{}, nil
	}
	if w*h > e.reader.remainingPixels {
		return nil, errors.New("RFB cursor exceeds frame limit")
	}
	e.reader.remainingPixels -= w * h
	rowBytes := (w + 7) / 8
	data := make([]byte, w*h*4+rowBytes*h)
	if _, err := io.ReadFull(e.reader.conn, data); err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	mask := data[w*h*4:]
	visible := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if mask[y*rowBytes+x/8]&(0x80>>uint(x%8)) == 0 {
				continue
			}
			i := (y*w + x) * 4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = data[i+2], data[i+1], data[i], 255
			visible = true
		}
	}
	if !visible {
		return &cursorEncoding{}, nil
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil, err
	}
	hash := sha256.New()
	hash.Write(encoded.Bytes())
	hash.Write([]byte{byte(rect.X), byte(rect.Y)})
	return &cursorEncoding{cursor: &Cursor{
		ID: hex.EncodeToString(hash.Sum(nil)), Width: w, Height: h,
		HotspotX: int(rect.X), HotspotY: int(rect.Y), PNG: base64.StdEncoding.EncodeToString(encoded.Bytes()),
	}}, nil
}

func (r *rfbClient) setCursorObserver(fn func(*Cursor)) {
	r.mu.Lock()
	r.onCursor = fn
	cursor := r.cursor
	r.mu.Unlock()
	if fn != nil && cursor != nil {
		fn(cursor)
	}
	r.wakeUpdates()
}

// The input connection is authoritative: TigerVNC can suppress cursor images
// on a separate video-only connection after the controller moves the pointer.
func (p *Oblien) SetCursorObserver(fn func(*Cursor)) {
	p.mu.Lock()
	p.onCursor = fn
	client := p.client
	p.mu.Unlock()
	if client != nil {
		client.setCursorObserver(fn)
	}
}

func (p *MacDesktop) SetCursorObserver(fn func(*Cursor)) {
	p.mu.Lock()
	p.onCursor = fn
	client := p.client
	p.mu.Unlock()
	if client != nil {
		client.setCursorObserver(fn)
	}
}

func (s *Service) observeProviderCursor(p Provider) {
	if source, ok := p.(interface{ SetCursorObserver(func(*Cursor)) }); ok {
		source.SetCursorObserver(func(cursor *Cursor) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.provider != p || cursor == nil || (s.snapshot.Cursor != nil && s.snapshot.Cursor.ID == cursor.ID) {
				return
			}
			s.snapshot.Cursor = cursor
			s.signalLocked()
		})
	}
}
