package surface

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net"
	"testing"

	vnc "github.com/kward/go-vnc"
)

func TestRichCursorPreservesColorTransparencyAndHotspot(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	go func() {
		// Two BGRA pixels, followed by a mask exposing only the first.
		_, _ = remote.Write([]byte{40, 30, 200, 0, 2, 3, 4, 0, 0x80})
	}()
	reader := &cursorEncoding{reader: &frameReader{conn: local, remainingPixels: 100}}
	decoded, err := reader.Read(nil, &vnc.Rectangle{X: 1, Width: 2, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	cursor := decoded.(*cursorEncoding).cursor
	if cursor == nil || cursor.Width != 2 || cursor.Height != 1 || cursor.HotspotX != 1 || cursor.HotspotY != 0 {
		t.Fatalf("missing cursor geometry: %+v", cursor)
	}
	data, err := base64.StdEncoding.DecodeString(cursor.PNG)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r != 200*257 || g != 30*257 || b != 40*257 || a != 65535 {
		t.Fatal("cursor colors did not follow the negotiated wire format")
	}
	_, _, _, a = img.At(1, 0).RGBA()
	if a != 0 {
		t.Fatal("cursor transparency mask was lost")
	}
	for _, rect := range []vnc.Rectangle{{Width: 257, Height: 1}, {Width: 2, Height: 1, X: 2}, {Width: 256, Height: 256}} {
		if _, err := reader.Read(nil, &rect); err == nil {
			t.Fatal("unsafe cursor dimensions or frame allocation accepted")
		}
	}
}

func TestCursorReplyCannotAcknowledgeCaptureAndDuplicateShapesDoNotEmit(t *testing.T) {
	o := &frameObservation{full: true}
	o.reset(Geometry{130, 3, 1})
	o.sent = true
	client := &rfbClient{frame: image.NewRGBA(image.Rect(0, 0, 130, 3)), size: o.size,
		observation: o, changed: make(chan struct{}), done: make(chan struct{})}
	updates := 0
	client.onCursor = func(*Cursor) { updates++ }
	for range 2 {
		messages := make(chan vnc.ServerMessage, 1)
		messages <- &vnc.FramebufferUpdate{Rects: []vnc.Rectangle{{Enc: &cursorEncoding{cursor: &Cursor{ID: "text"}}}}}
		close(messages)
		client.receive(messages)
	}
	if o.remaining != 390 || !client.observedAt.IsZero() {
		t.Fatal("a cursor update acknowledged screenshot pixels or fresh geometry")
	}
	if updates != 1 {
		t.Fatalf("unchanged cursor emitted %d updates", updates)
	}
	if o.sent {
		t.Fatal("cursor-only reply did not rearm the missing screenshot request")
	}
	o.sent = true
	for _, rect := range []image.Rectangle{image.Rect(0, 0, 65, 3), image.Rect(64, 0, 130, 2), image.Rect(0, 0, 100, 3)} {
		o.cover(rect)
	}
	if o.remaining != 30 {
		t.Fatalf("overlapping tiles falsely acknowledged missing pixels: %d", o.remaining)
	}
	o.cover(image.Rect(100, 2, 130, 3))
	if o.remaining != 0 {
		t.Fatal("complete tiled image was not acknowledged")
	}
}

type cursorProvider struct {
	Provider
	onCursor func(*Cursor)
}

func (p *cursorProvider) SetCursorObserver(fn func(*Cursor)) { p.onCursor = fn }

func TestCursorObserverRejectsReplacedProviderAndSnapshotsAreImmutable(t *testing.T) {
	s, p, _ := testService(t)
	defer s.Close()
	first := &cursorProvider{Provider: p}
	if err := s.Configure(first); err != nil {
		t.Fatal(err)
	}
	first.onCursor(&Cursor{ID: "first"})
	copy := s.Snapshot()
	copy.Cursor.ID = "mutated copy"
	if s.Snapshot().Cursor.ID != "first" {
		t.Fatal("a caller mutated the shared cursor")
	}
	second := &cursorProvider{Provider: p}
	if err := s.Configure(second); err != nil {
		t.Fatal(err)
	}
	first.onCursor(&Cursor{ID: "late"})
	if s.Snapshot().Cursor != nil {
		t.Fatal("old desktop supplied the replacement's cursor")
	}
	second.onCursor(&Cursor{ID: "current"})
	if s.Snapshot().Cursor.ID != "current" {
		t.Fatal("current desktop cursor was not forwarded")
	}
}
