package surface

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kward/go-vnc/keys"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func TestMacKeyboardTextAndShiftedChordsPreserveCharacters(t *testing.T) {
	s, store, fixture, _ := macTestService(t)
	enableTestMac(t, s, store)
	view := mustOpen(t, s, Actor{Kind: "user"}, "control")
	p := s.provider.(*MacDesktop)
	p.clipboard = func(context.Context, *string) (string, error) {
		t.Error("keyboard input touched the clipboard")
		return "", nil
	}
	chord := []string{"Shift", "m"}
	for i, action := range []Action{
		{Kind: "text", TextMode: "keyboard", Text: "aB!"},
		{Kind: "key", Keys: chord},
	} {
		_, err := s.Apply(t.Context(), Actor{Kind: "user"}, ActionRequest{RequestID: fmt.Sprint("mac-symbols-", i), SessionID: view.ID, ControlGeneration: view.Controller.Generation, Action: action})
		if err != nil {
			t.Fatal(err)
		}
	}
	// A framebuffer response is also a barrier: the fixture consumed all input.
	if _, err := p.client.refreshGeometry(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	var downs []uint32
	for _, input := range fixture.inputs {
		if input.kind == 4 && input.data[0] == 1 {
			downs = append(downs, binary.BigEndian.Uint32(input.data[3:]))
		}
	}
	if want := []uint32{'a', 'B', '!', 0xffe1, 'M'}; !slices.Equal(downs, want) {
		t.Fatalf("Mac characters = %x, want %x", downs, want)
	}
	if !slices.Equal(chord, []string{"Shift", "m"}) {
		t.Fatal("input normalization changed the original request")
	}
}

func TestMacInitialInputWaitsAndCanBeCancelled(t *testing.T) {
	for _, cancelInput := range []bool{false, true} {
		t.Run(fmt.Sprint("cancel=", cancelInput), func(t *testing.T) {
			local, remote := net.Pipe()
			fixture := &rfbFixture{}
			go fixture.serve(remote)
			client, err := newRFB(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			defer client.close()
			ready := time.Now().Add(80 * time.Millisecond)
			p := &MacDesktop{client: client, inputReadyAt: ready}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := p.Apply(ctx, Action{Kind: "text", TextMode: "keyboard", Text: "X"}); done <- err }()
			select {
			case err := <-done:
				t.Fatalf("input did not wait: %v", err)
			case <-time.After(10 * time.Millisecond):
			}
			fixture.mu.Lock()
			before := len(fixture.inputs)
			fixture.mu.Unlock()
			if before != 0 {
				t.Fatal("input reached Mac before preparation completed")
			}
			if cancelInput {
				cancel()
			}
			err = <-done
			if cancelInput {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation", err)
				}
			} else if err != nil || time.Now().Before(ready) {
				t.Fatal("input readiness", err)
			}
			if _, err = client.refreshGeometry(t.Context()); err != nil {
				t.Fatal(err)
			}
			fixture.mu.Lock()
			after := len(fixture.inputs)
			fixture.mu.Unlock()
			if cancelInput && after != 0 {
				t.Fatal("cancelled input was dispatched")
			}
			if !cancelInput && after == 0 {
				t.Fatal("ready input was not dispatched")
			}
		})
	}
}

func TestMacInputPreparationStopsOnDisplayClose(t *testing.T) {
	client := &rfbClient{done: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- waitMacInput(t.Context(), client, time.Now().Add(time.Hour)) }()
	close(client.done)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed display accepted input")
		}
	case <-time.After(time.Second):
		t.Fatal("input waited after display closed")
	}
}

func TestMacControlPreparationKeepsViewAndCancellationPreservesLease(t *testing.T) {
	s, store, _, _ := macTestService(t)
	enableTestMac(t, s, store)
	owner := mustOpen(t, s, Actor{Kind: "user"}, "control")
	viewer := mustOpen(t, s, Actor{Kind: "user"}, "view")
	p := s.provider.(*MacDesktop)
	p.mu.Lock()
	p.inputReadyAt = time.Now().Add(time.Hour)
	p.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := s.Control(ctx, viewer.ID, Actor{Kind: "user"}, ControlRequest{Action: "takeover"})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("control did not wait: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	before := s.Snapshot()
	if before.State != "connected" || before.Controller == nil || before.Controller.SessionID != owner.ID {
		t.Fatal("preparation changed the view or previous controller")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("control cancellation", err)
	}
	after := s.Snapshot()
	if after.Controller == nil || after.Controller.SessionID != owner.ID {
		t.Fatal("cancelled preparation revoked the previous controller")
	}
	p.mu.Lock()
	p.inputReadyAt = time.Time{}
	p.mu.Unlock()
	if _, err := s.Control(t.Context(), viewer.ID, Actor{Kind: "user"}, ControlRequest{Action: "takeover"}); err != nil {
		t.Fatal(err)
	}
}

// Independent ARD server side of the test. A fixed RFC DH group keeps the
// fixture deterministic in size; every connection uses a fresh private key.
func fixtureMacAuth(username, password string, accepted *atomic.Int32) func(io.ReadWriteCloser) bool {
	return func(conn io.ReadWriteCloser) bool {
		if _, err := conn.Write([]byte{1, 30}); err != nil {
			return false
		}
		var selected [1]byte
		if _, err := io.ReadFull(conn, selected[:]); err != nil || selected[0] != 30 {
			return false
		}
		prime, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE65381FFFFFFFFFFFFFFFF", 16)
		private, _ := rand.Int(rand.Reader, new(big.Int).Sub(prime, big.NewInt(4)))
		private.Add(private, big.NewInt(2))
		public := new(big.Int).Exp(big.NewInt(2), private, prime)
		size := (prime.BitLen() + 7) / 8
		var offer bytes.Buffer
		_ = binary.Write(&offer, binary.BigEndian, uint16(2))
		_ = binary.Write(&offer, binary.BigEndian, uint16(size))
		offer.Write(prime.FillBytes(make([]byte, size)))
		offer.Write(public.FillBytes(make([]byte, size)))
		if _, err := conn.Write(offer.Bytes()); err != nil {
			return false
		}
		response := make([]byte, 128+size)
		if _, err := io.ReadFull(conn, response); err != nil {
			return false
		}
		peer := new(big.Int).SetBytes(response[128:])
		shared := new(big.Int).Exp(peer, private, prime).FillBytes(make([]byte, size))
		key := md5.Sum(shared)
		cipher, _ := aes.NewCipher(key[:])
		plain := make([]byte, 128)
		for i := 0; i < 128; i += 16 {
			cipher.Decrypt(plain[i:i+16], response[i:i+16])
		}
		cstring := func(value []byte) string {
			at := bytes.IndexByte(value, 0)
			if at < 0 {
				return ""
			}
			return string(value[:at])
		}
		if cstring(plain[:64]) != username || cstring(plain[64:]) != password {
			_ = binary.Write(conn, binary.BigEndian, uint32(1))
			_ = binary.Write(conn, binary.BigEndian, uint32(6))
			_, _ = conn.Write([]byte("denied"))
			return false
		}
		accepted.Add(1)
		_ = binary.Write(conn, binary.BigEndian, uint32(0))
		return true
	}
}

func macTestService(t *testing.T) (*Service, *session.Store, *rfbFixture, *atomic.Int32) {
	t.Helper()
	s, _, _ := testService(t)
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	accepted := &atomic.Int32{}
	fixture := &rfbFixture{authentication: fixtureMacAuth("owner", " test password Ω ", accepted)}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go fixture.serve(conn)
		}
	}()
	s.macFactory = func(settings LocalDesktopSettings) (*MacDesktop, error) {
		return &MacDesktop{settings: settings, username: "owner", dial: func(ctx context.Context) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
		},
			console: func(context.Context) bool { return true }, clipboard: func(context.Context, *string) (string, error) { return "test clipboard", nil }}, nil
	}
	if err := s.EnableLocalDesktop(store); err != nil {
		t.Fatal(err)
	}
	return s, store, fixture, accepted
}

func enableTestMac(t *testing.T, s *Service, store *session.Store) {
	t.Helper()
	if _, err := s.ConfigureLocalDesktop(t.Context(), LocalDesktopSettings{Enabled: true, Username: "owner", Password: " test password Ω "}, store); err != nil {
		t.Fatal(err)
	}
}

func TestMacDesktopOptInAuthenticationAndIdempotentSetup(t *testing.T) {
	s, store, fixture, accepted := macTestService(t)
	before, _ := s.Refresh(t.Context())
	if !before.Supported || before.Enabled || before.Setup == nil || before.Setup.Command != "mindwire desktop enable" {
		t.Fatalf("wrong disabled state: %+v", before)
	}
	if accepted.Load() != 0 {
		t.Fatal("status authenticated without local opt-in")
	}
	if _, err := s.Open(t.Context(), Actor{Kind: "user"}, OpenRequest{RequestID: "before", Mode: "view"}); err == nil {
		t.Fatal("desktop opened before opt-in")
	}
	if _, err := s.ConfigureLocalDesktop(t.Context(), LocalDesktopSettings{Enabled: true, Username: "owner", Password: "wrong"}, store); asError(err).Code != "desktop_authentication" {
		t.Fatal("bad Mac login did not return a retryable authentication error", err)
	}
	if store.Get(localDesktopKey) != "" {
		t.Fatal("failed authentication persisted settings")
	}
	enableTestMac(t, s, store)
	viewer := mustOpen(t, s, Actor{Kind: "user"}, "control")
	count := accepted.Load()
	enableTestMac(t, s, store)
	if accepted.Load() != count || s.Snapshot().Controller == nil || s.Snapshot().Controller.SessionID != viewer.ID {
		t.Fatal("identical setup restarted authentication or lost control")
	}
	saved := store.Get(localDesktopKey)
	if _, err := s.ConfigureLocalDesktop(t.Context(), LocalDesktopSettings{Enabled: true, Username: "owner", Password: "wrong again"}, store); asError(err).Code != "desktop_authentication" {
		t.Fatal("failed reconfiguration did not preserve the login error", err)
	}
	if saved != store.Get(localDesktopKey) || s.Snapshot().Controller == nil || s.Snapshot().Controller.SessionID != viewer.ID {
		t.Fatal("rejected password changed working credentials or dropped control")
	}
	capture, err := s.Capture(t.Context(), viewer.ID, Actor{Kind: "user"})
	if err != nil || capture.Geometry.Width != 2 {
		t.Fatal("Mac capture", err)
	}
	input := ActionRequest{RequestID: "mac-key", SessionID: viewer.ID, ControlGeneration: viewer.Controller.Generation, GeometryRevision: s.Snapshot().Geometry.Revision, Action: Action{Kind: "key", Keys: []string{"Escape"}}}
	if _, err := s.Apply(t.Context(), Actor{Kind: "user"}, input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(t.Context(), Actor{Kind: "user"}, input); err != nil {
		t.Fatal(err)
	}
	// Input writes return before the fixture's reader consumes them. A frame
	// response on the same ordered connection observes both requests first.
	if _, err := s.provider.(*MacDesktop).client.refreshGeometry(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	inputs := 0
	for _, input := range fixture.inputs {
		if input.kind == 4 {
			inputs++
		}
	}
	fixture.mu.Unlock()
	if inputs != 2 {
		t.Fatalf("receipt replay duplicated a key press: %d", inputs)
	}
	public, _ := json.Marshal(s.Snapshot())
	if bytes.Contains(public, []byte("password")) || bytes.Contains(public, []byte("owner")) {
		t.Fatal("Mac login leaked in desktop snapshot")
	}
	if _, err := s.ConfigureLocalDesktop(t.Context(), LocalDesktopSettings{Enabled: false}, store); err != nil {
		t.Fatal(err)
	}
	if s.ActiveSessionCount() != 0 || s.Snapshot().Controller != nil || s.Snapshot().Enabled {
		t.Fatal("disable left a live desktop controller")
	}
	if strings.Contains(store.Get(localDesktopKey), "test password") {
		t.Fatal("disable retained the Mac password")
	}
	if err := s.AuthorizeViewer(viewer.ID); err == nil {
		t.Fatal("old view grant survived disabling")
	}
}

func TestMacConnectionErrorsDistinguishRejectedLoginFromBrokenConnection(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{errors.New("SecurityResult handshake failed: private upstream response"), "desktop_authentication"},
		{&rfbHandshakeFailure{cause: errors.New("SecurityResult handshake failed: private upstream response")}, "desktop_authentication"},
		{&rfbHandshakeFailure{cause: context.DeadlineExceeded}, "timeout"},
		{&rfbHandshakeFailure{cause: io.EOF}, "unavailable"},
		{io.ErrUnexpectedEOF, "unavailable"},
		{problem("desktop_auth_protocol", "Unsupported Screen Sharing authentication."), "desktop_auth_protocol"},
		{errors.New("Security handshake failed; no suitable auth schemes found; server supports: private upstream response"), "desktop_auth_protocol"},
	} {
		got := asError(macConnectionError(t.Context(), test.err))
		if got.Code != test.code || strings.Contains(got.Message, "private upstream") {
			t.Fatalf("wrong or unsanitized desktop failure: %+v", got)
		}
	}
}

func TestMacViewerSharesNativeFramesButCannotSendInput(t *testing.T) {
	s, store, fixture, _ := macTestService(t)
	enableTestMac(t, s, store)
	view := mustOpen(t, s, Actor{Kind: "user"}, "view")
	phone, server := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- s.ServeViewer(t.Context(), view.ID, server) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	native, err := newRFB(ctx, phone)
	if err != nil {
		select {
		case serverError := <-done:
			t.Fatal(err, "viewer:", serverError)
		case <-ctx.Done():
			t.Fatal(err, ctx.Err())
		}
	}
	defer native.close()
	frame, geometry, err := native.capture(ctx)
	if err != nil || geometry.Width != 2 {
		t.Fatal("native frame", err)
	}
	r, g, b, _ := frame.At(0, 0).RGBA()
	if r>>8 != 200 || g>>8 != 30 || b>>8 != 40 {
		t.Fatal("frame changed through tunnel")
	}
	_ = native.client.KeyEvent(keys.Escape, true)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("viewer input was accepted")
		}
	case <-ctx.Done():
		t.Fatal("input did not close the viewer")
	}
	fixture.mu.Lock()
	inputs := len(fixture.inputs)
	fixture.mu.Unlock()
	if inputs != 0 {
		t.Fatal("viewer bypassed shared input authorization")
	}
}

func TestMacViewerClosesOnDisableAndAgentCannotBorrowIt(t *testing.T) {
	s, store, _, _ := macTestService(t)
	enableTestMac(t, s, store)
	s.SetApproval(func(context.Context, Actor, string) error { return nil })
	agent := mustOpen(t, s, Actor{Kind: "agent", RunID: "run", Name: "Codex"}, "view")
	if err := s.AuthorizeViewer(agent.ID); err == nil {
		t.Fatal("agent borrowed raw human viewer")
	}
	view := mustOpen(t, s, Actor{Kind: "user"}, "view")
	phone, server := net.Pipe()
	defer phone.Close()
	done := make(chan error, 1)
	go func() { done <- s.ServeViewer(t.Context(), view.ID, server) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	native, err := newRFB(ctx, phone)
	if err != nil {
		select {
		case serverError := <-done:
			t.Fatal(err, "viewer:", serverError)
		case <-ctx.Done():
			t.Fatal(err, ctx.Err())
		}
	}
	defer native.close()
	if _, err := s.ConfigureLocalDesktop(ctx, LocalDesktopSettings{Enabled: false}, store); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("disable left the video stream open")
	}
}

func TestMacViewerProtocolRejectsControlAndBoundsMessages(t *testing.T) {
	for _, input := range [][]byte{{4}, {5}, {6}, {251}, {2, 0, 0, 129}, {248, 0, 0, 0, 0, 0, 0, 0, 65}} {
		var forwarded bytes.Buffer
		if err := forwardViewerRequests(&forwarded, bytes.NewReader(input)); err == nil || forwarded.Len() != 0 {
			t.Fatalf("unsafe viewer request passed: %v", input)
		}
	}
	var valid bytes.Buffer
	valid.Write([]byte{2, 0, 0, 2, 0, 0, 0, 0, 255, 255, 255, 33})
	valid.Write([]byte{3, 0, 0, 0, 0, 0, 0, 2, 0, 2})
	var forwarded bytes.Buffer
	err := forwardViewerRequests(&forwarded, bytes.NewReader(valid.Bytes()))
	if !errors.Is(err, io.EOF) || !bytes.Equal(valid.Bytes(), forwarded.Bytes()) {
		t.Fatal("frame negotiation was not preserved", err)
	}
}

func TestMacDesktopRejectsRFB33AuthenticationDowngrade(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() { _, _ = server.Write([]byte("RFB 003.003\n")) }()
	p := &MacDesktop{dial: func(context.Context) (net.Conn, error) { return client, nil }}
	if _, err := p.dialAuthenticated(t.Context()); err == nil {
		t.Fatal("legacy unauthenticated negotiation accepted")
	}
}
