package surface

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	vnc "github.com/kward/go-vnc"
	"golang.org/x/crypto/ssh"
)

type wireInput struct {
	kind byte
	data []byte
}
type rfbFixture struct {
	mu             sync.Mutex
	inputs         []wireInput
	resize         bool
	largeFrame     bool
	tiledFrame     bool
	debug          bool
	frameRequests  int
	formats16      int
	formats24      int
	authentication func(io.ReadWriteCloser) bool
}

func (f *rfbFixture) serve(conn io.ReadWriteCloser) {
	defer conn.Close()
	if f.debug {
		fmt.Println("RFB_FIXTURE connected")
	}
	read := func(n int) ([]byte, error) { b := make([]byte, n); _, err := io.ReadFull(conn, b); return b, err }
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return
	}
	if _, err := read(12); err != nil {
		return
	}
	if f.authentication != nil {
		if !f.authentication(conn) {
			return
		}
	} else {
		_, _ = conn.Write([]byte{1, 1})
		if _, err := read(1); err != nil {
			return
		}
		_, _ = conn.Write([]byte{0, 0, 0, 0})
	}
	if _, err := read(1); err != nil {
		return
	}
	var init bytes.Buffer
	_ = binary.Write(&init, binary.BigEndian, uint16(2))
	_ = binary.Write(&init, binary.BigEndian, uint16(2))
	init.Write([]byte{32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0})
	_ = binary.Write(&init, binary.BigEndian, uint32(4))
	init.WriteString("test")
	if _, err := conn.Write(init.Bytes()); err != nil {
		return
	}
	if f.debug {
		fmt.Println("RFB_FIXTURE initialized")
	}
	resized := false
	sentPixels := false
	pixel := []byte{40, 30, 200, 0}
	debugMessages := 0
	for {
		typeByte, err := read(1)
		if err != nil {
			return
		}
		if f.debug && debugMessages < 10 {
			fmt.Println("RFB_FIXTURE client message", typeByte[0])
			debugMessages++
		}
		switch kind := typeByte[0]; kind {
		case 0:
			format, err := read(19)
			if err != nil {
				return
			}
			switch {
			case bytes.Equal(format[3:], []byte{32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}):
				pixel = []byte{40, 30, 200, 0}
				f.mu.Lock()
				f.formats24++
				f.mu.Unlock()
			case bytes.Equal(format[3:], []byte{16, 16, 0, 1, 0, 31, 0, 31, 0, 31, 10, 5, 0, 0, 0, 0}):
				// RoyalVNCKit's reduced-color format uses RGB555 in 16 bits.
				value := uint16(200>>3)<<10 | uint16(30>>3)<<5 | uint16(40>>3)
				pixel = []byte{byte(value), byte(value >> 8)}
				f.mu.Lock()
				f.formats16++
				f.mu.Unlock()
			default:
				return // TigerVNC rejects an invalid format immediately, before any frame request.
			}
		case 2:
			header, err := read(3)
			if err != nil {
				return
			}
			if _, err := read(int(binary.BigEndian.Uint16(header[1:])) * 4); err != nil {
				return
			}
		case 3:
			f.mu.Lock()
			f.frameRequests++
			f.mu.Unlock()
			request, err := read(9)
			if err != nil {
				return
			}
			if f.largeFrame && sentPixels && request[0] != 0 {
				// Keep the synthetic desktop still after its first complete frame.
				time.Sleep(50 * time.Millisecond)
				if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
					return
				}
				continue
			}
			w, h := uint16(2), uint16(2)
			encoding := int32(0)
			if f.resize {
				w = 3
				if f.largeFrame {
					w, h = 1536, 1024
				}
				if !resized {
					encoding = -223
					resized = true
				}
			}
			var frame bytes.Buffer
			if encoding == 0 && f.tiledFrame {
				// Apple/ZRLE-style batches expose hundreds of dirty regions to the
				// native viewer. Full-screen copies per region used to stall it.
				const tile = 64
				frame.Write([]byte{0, 0})
				_ = binary.Write(&frame, binary.BigEndian, uint16(((int(w)+tile-1)/tile)*((int(h)+tile-1)/tile)))
				for y := 0; y < int(h); y += tile {
					for x := 0; x < int(w); x += tile {
						tw, th := min(tile, int(w)-x), min(tile, int(h)-y)
						for _, value := range []uint16{uint16(x), uint16(y), uint16(tw), uint16(th)} {
							_ = binary.Write(&frame, binary.BigEndian, value)
						}
						_ = binary.Write(&frame, binary.BigEndian, int32(0))
						frame.Write(bytes.Repeat(pixel, tw*th))
					}
				}
				sentPixels = true
				if _, err := conn.Write(frame.Bytes()); err != nil {
					return
				}
				continue
			}
			frame.Write([]byte{0, 0, 0, 1})
			for _, v := range []uint16{0, 0, w, h} {
				_ = binary.Write(&frame, binary.BigEndian, v)
			}
			_ = binary.Write(&frame, binary.BigEndian, encoding)
			if encoding == 0 {
				frame.Write(bytes.Repeat(pixel, int(w)*int(h)))
				sentPixels = true
			}
			if _, err := conn.Write(frame.Bytes()); err != nil {
				return
			}
		case 4, 5:
			length := 7
			if kind == 5 {
				length = 5
			}
			data, err := read(length)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.inputs = append(f.inputs, wireInput{kind, data})
			f.mu.Unlock()
		case 6:
			header, err := read(7)
			if err != nil {
				return
			}
			if _, err := read(int(binary.BigEndian.Uint32(header[3:]))); err != nil {
				return
			}
		default:
			return
		}
	}
}

func TestDesktopPixelFormatUsesNetworkByteOrder(t *testing.T) {
	var wire bytes.Buffer
	if err := setDesktopPixelFormat(&wire); err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0, 0, 0, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
	if !bytes.Equal(wire.Bytes(), want) {
		t.Fatalf("nonstandard RFB pixel format: %x", wire.Bytes())
	}
}

func TestRFBHandshakeCorrectionPreservesAuthenticationAndViewerTraffic(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	_ = local.SetDeadline(time.Now().Add(3 * time.Second))
	_ = remote.SetDeadline(time.Now().Add(3 * time.Second))
	var standard bytes.Buffer
	if err := setDesktopPixelFormat(&standard); err != nil {
		t.Fatal(err)
	}
	malformed := bytes.Clone(standard.Bytes())
	for offset := 8; offset < 14; offset += 2 {
		malformed[offset], malformed[offset+1] = malformed[offset+1], malformed[offset]
	}
	raw := []byte{2, 0, 0, 1, 0, 0, 0, 0}
	// Bytes resembling a format before initialization must not be rewritten.
	// Afterwards the native viewer sends already-correct wire bytes itself.
	requests := [][]byte{[]byte("RFB 003.008\n"), malformed, raw, malformed, standard.Bytes()}
	want := bytes.Join([][]byte{requests[0], malformed, raw, standard.Bytes(), standard.Bytes()}, nil)
	received := make(chan []byte, 1)
	go func() {
		data := make([]byte, len(want))
		_, _ = io.ReadFull(remote, data)
		received <- data
	}()
	conn := &rfbHandshakeConn{Conn: local}
	for _, request := range requests {
		before := bytes.Clone(request)
		if _, err := conn.Write(request); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(request, before) {
			t.Fatal("handshake modified the caller's bytes")
		}
	}
	if got := <-received; !bytes.Equal(got, want) {
		t.Fatal("correction altered authentication or subsequent native viewer traffic")
	}
}

func TestRFBSizeAnnouncementOnlyInvalidatesChangedGeometry(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
	frame.Pix[0] = 123
	client := &rfbClient{frame: frame, size: Geometry{2, 2, 10}, changed: make(chan struct{}), done: make(chan struct{})}
	announce := func(width uint16) {
		messages := make(chan vnc.ServerMessage, 1)
		messages <- &vnc.FramebufferUpdate{Rects: []vnc.Rectangle{{Width: width, Height: 2, Enc: &vnc.DesktopSizePseudoEncoding{}}}}
		close(messages)
		client.receive(messages)
	}
	announce(2)
	if client.geometry() != (Geometry{2, 2, 10}) || client.frame != frame || client.frame.Pix[0] != 123 {
		t.Fatal("an unchanged size invalidated pointer coordinates or erased the existing frame")
	}
	announce(3)
	if client.geometry() != (Geometry{3, 2, 11}) || client.frame.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatal("a real resize did not invalidate the old geometry")
	}
}

func TestDesktopExtendedKeysAndKeyboardTextValidation(t *testing.T) {
	for name, want := range map[string]uint32{
		"F1": 0xffbe, "f12": 0xffc9, "F24": 0xffd5, "CapsLock": 0xffe5,
		"Insert": 0xff63, "Delete": 0xffff, "PrintScreen": 0xff61,
		"Pause": 0xff13, "Menu": 0xff67, "NumLock": 0xff7f, "ScrollLock": 0xff14,
	} {
		got, err := parseKey(name)
		if err != nil || uint32(got) != want {
			t.Errorf("%s = %x, %v; want %x", name, got, err, want)
		}
	}
	for _, name := range []string{"F0", "F25", "F01", "Fn"} {
		if _, err := parseKey(name); err == nil {
			t.Errorf("unsupported key %q accepted", name)
		}
	}
	if err := validateAction(Action{Kind: "text", TextMode: "keyboard", Text: "print(\"Hi!\")"}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []Action{
		{Kind: "text", TextMode: "keyboard", Text: "\n"},
		{Kind: "text", TextMode: "keyboard", Text: "你好"},
		{Kind: "text", TextMode: "keyboard", Text: strings.Repeat("x", 4097)},
		{Kind: "key", TextMode: "keyboard", Keys: []string{"a"}},
		{Kind: "text", TextMode: "unknown", Text: "x"},
	} {
		if err := validateAction(action); err == nil {
			t.Error("invalid keyboard text mode accepted")
		}
	}
}

func TestLiveKeystrokesDoNotWaitForGeometryOrReplaceClipboard(t *testing.T) {
	local, remote := net.Pipe()
	fixture := &rfbFixture{}
	go fixture.serve(remote)
	client, err := newRFB(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	defer client.close()
	if _, _, err := client.capture(t.Context()); err != nil {
		t.Fatal(err)
	}
	provider := &Oblien{client: client, target: "macos", status: ProviderStatus{OS: "darwin"},
		binding: Binding{Connection: SSHConnection{ExpiresAt: time.Now().Add(time.Hour)}}}
	fixture.mu.Lock()
	before := fixture.frameRequests
	fixture.mu.Unlock()
	if _, err := provider.Apply(t.Context(), Action{Kind: "text", TextMode: "keyboard", Text: "aB!"}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Apply(t.Context(), Action{Kind: "key", Keys: []string{"F12"}}); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.frameRequests != before {
		t.Fatal("typing waited for an unnecessary screen refresh")
	}
	var downs []uint32
	for _, event := range fixture.inputs {
		if event.kind == 4 && event.data[0] == 1 {
			downs = append(downs, binary.BigEndian.Uint32(event.data[3:]))
		}
	}
	want := []uint32{'a', 0xffe1, 'b', 0xffe1, '1', 0xffc9}
	if fmt.Sprint(downs) != fmt.Sprint(want) {
		t.Fatalf("physical keyboard events %x, want %x", downs, want)
	}
}

func TestRFBCaptureResizeColorsAndReleasedInput(t *testing.T) {
	local, remote := net.Pipe()
	fixture := &rfbFixture{resize: true}
	go fixture.serve(remote)
	client, err := newRFB(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	defer client.close()
	img, size, err := client.capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if size.Width != 3 || img.Bounds().Dx() != 3 {
		t.Fatalf("resize not reconciled: %v", size)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r != 200*257 || g != 30*257 || b != 40*257 || a != 65535 {
		t.Fatalf("pixel channels wrong: %d %d %d %d", r, g, b, a)
	}
	x, y, toX, toY := 0, 0, 2, 1
	if err = client.apply(t.Context(), Action{Kind: "drag", X: &x, Y: &y, ToX: &toX, ToY: &toY}); err != nil {
		t.Fatal(err)
	}
	if err = client.chord(t.Context(), []string{"Meta", "v"}); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	var lastPointer []byte
	metaDown, metaUp := false, false
	for _, input := range fixture.inputs {
		if input.kind == 5 {
			lastPointer = input.data
		}
		if input.kind == 4 && binary.BigEndian.Uint32(input.data[3:]) == 0xffe7 {
			if input.data[0] == 1 {
				metaDown = true
			} else {
				metaUp = true
			}
		}
	}
	if len(lastPointer) == 0 || lastPointer[0] != 0 || binary.BigEndian.Uint16(lastPointer[1:]) != 2 || binary.BigEndian.Uint16(lastPointer[3:]) != 1 {
		t.Fatalf("drag did not release at destination: %v", lastPointer)
	}
	if !metaDown || !metaUp {
		t.Fatal("Mac command chord did not press and release Meta_L")
	}
}

// Opt-in local SSH+RFB fixture for the iOS native integration test. It is entirely
// synthetic; it cannot reach or control a real desktop. No workspace credentials.
func TestNativeViewerFixture(t *testing.T) {
	path := os.Getenv("MINDWIRE_NATIVE_FIXTURE_FILE")
	if path == "" {
		t.Skip("native viewer integration fixture")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	config.AddHostKey(signer)
	data, _ := json.Marshal(map[string]any{"port": listener.Addr().(*net.TCPAddr).Port, "fingerprint": ssh.FingerprintSHA256(signer.PublicKey())})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.AfterFunc(180*time.Second, func() { _ = listener.Close() })
	defer deadline.Stop()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			fmt.Println("SSH_FIXTURE connected")
			server, channels, requests, err := ssh.NewServerConn(conn, config)
			if err != nil {
				conn.Close()
				return
			}
			fmt.Println("SSH_FIXTURE authenticated")
			defer server.Close()
			go ssh.DiscardRequests(requests)
			for pending := range channels {
				if pending.ChannelType() != "direct-tcpip" {
					_ = pending.Reject(ssh.Prohibited, "forwarding only")
					continue
				}
				channel, requests, err := pending.Accept()
				if err != nil {
					continue
				}
				go ssh.DiscardRequests(requests)
				go (&rfbFixture{resize: true, debug: true}).serve(channel)
			}
		}()
	}
}
