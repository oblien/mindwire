package surface

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type wireInput struct {
	kind byte
	data []byte
}
type rfbFixture struct {
	mu     sync.Mutex
	inputs []wireInput
	resize bool
	debug  bool
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
	_, _ = conn.Write([]byte{1, 1})
	if _, err := read(1); err != nil {
		return
	}
	_, _ = conn.Write([]byte{0, 0, 0, 0})
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
			if _, err := read(19); err != nil {
				return
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
			if _, err := read(9); err != nil {
				return
			}
			w, h := uint16(2), uint16(2)
			encoding := int32(0)
			if f.resize {
				w = 3
				if !resized {
					encoding = -223
					resized = true
				}
			}
			var frame bytes.Buffer
			frame.Write([]byte{0, 0, 0, 1})
			for _, v := range []uint16{0, 0, w, h} {
				_ = binary.Write(&frame, binary.BigEndian, v)
			}
			_ = binary.Write(&frame, binary.BigEndian, encoding)
			if encoding == 0 {
				for i := 0; i < int(w*h); i++ {
					frame.Write([]byte{40, 30, 200, 0})
				}
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
