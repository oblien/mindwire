package computer

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func approveForwardDevice(t *testing.T, s *Server, ws bool) (*ssh.Client, string) {
	t.Helper()
	key := clientKey(t)
	inv := invite(t, s)
	req := PairRequest{ID: randomID(12), Name: "Forward test", PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))}
	if _, err := s.Request(inv.PairingID, req); err != nil {
		t.Fatal(err)
	}
	device, err := s.Decide(inv.PairingID, req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.offers[inv.PairingID].Completed = true
	s.mu.Unlock()
	return connect(t, s, "device", ssh.PublicKeys(key), ws), device.ID
}

func TestForwardGrantsArePrivateRenewableAndRevocable(t *testing.T) {
	for _, ws := range []bool{false, true} {
		t.Run(strconv.FormatBool(ws), func(t *testing.T) {
			s, _ := fixture(t)
			owner, deviceID := approveForwardDevice(t, s, ws)
			other, _ := approveForwardDevice(t, s, ws)
			echo, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer echo.Close()
			go func() {
				for {
					conn, err := echo.Accept()
					if err != nil {
						return
					}
					go func() { defer conn.Close(); _, _ = io.Copy(conn, conn) }()
				}
			}()
			port := echo.Addr().(*net.TCPAddr).Port
			address := echo.Addr().String()
			if conn, err := owner.Dial("tcp", address); err == nil {
				conn.Close()
				t.Fatal("port opened without authorization")
			}
			req := ForwardRequest{ID: "forward-test", DeviceID: deviceID, Port: port}
			first, err := s.openForward(req)
			if err != nil {
				t.Fatal(err)
			}
			renewed, err := s.openForward(req)
			if err != nil || renewed.ID != first.ID || len(s.forwards) != 1 {
				t.Fatal("renewal duplicated a forward", err)
			}
			if s.PendingActivity() != 1 {
				t.Fatal("forward must reserve the service from idle updates")
			}
			if conn, err := other.Dial("tcp", address); err == nil {
				conn.Close()
				t.Fatal("another device used the grant")
			}
			if conn, err := owner.Dial("tcp", "localhost:"+strconv.Itoa(port)); err == nil {
				conn.Close()
				t.Fatal("a hostname bypassed loopback policy")
			}
			conn, err := owner.Dial("tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			payload := []byte("private preview\x00\xff")
			if _, err = conn.Write(payload); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(payload))
			if _, err = io.ReadFull(conn, got); err != nil || !bytes.Equal(got, payload) {
				t.Fatal("forward corrupted bytes", err)
			}
			s.mu.Lock()
			s.closeForwardLocked(req.ID)
			s.mu.Unlock()
			if _, err = conn.Read(make([]byte, 1)); err == nil {
				t.Fatal("closed grant left the channel alive")
			}
			if conn, err := owner.Dial("tcp", address); err == nil {
				conn.Close()
				t.Fatal("closed grant reopened")
			}
			if s.PendingActivity() != 0 {
				t.Fatal("closed forward still blocks update")
			}
		})
	}
}

func TestForwardAPIValidationExpiryAndDeviceRemoval(t *testing.T) {
	s, _ := fixture(t)
	_, deviceID := approveForwardDevice(t, s, false)
	mux := http.NewServeMux()
	s.Register(mux)
	for _, body := range []string{
		`{"id":"forward-test","deviceId":"unknown","port":3000}`,
		`{"id":"forward-test","deviceId":"` + deviceID + `","port":8790}`,
		`{"id":"forward-test","deviceId":"` + deviceID + `","port":8793}`,
		`{"id":"forward-test","deviceId":"` + deviceID + `","port":65536}`,
		`{"id":"../unsafe","deviceId":"` + deviceID + `","port":3000}`,
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("POST", "/computer/forwards", strings.NewReader(body)))
		if response.Code < 400 {
			t.Fatal("invalid forwarding grant accepted")
		}
	}
	req := ForwardRequest{ID: "forward-test", DeviceID: deviceID, Port: 3000}
	if _, err := s.openForward(req); err != nil {
		t.Fatal(err)
	}
	changed := req
	changed.Port = 3001
	if _, err := s.openForward(changed); err == nil {
		t.Fatal("ID changed its target")
	}
	changed = req
	changed.ID = "duplicate-forward"
	if _, err := s.openForward(changed); err == nil {
		t.Fatal("same port acquired twice")
	}
	s.mu.Lock()
	s.forwards[req.ID].ExpiresAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, ok := s.forwardedPort(deviceID, 3000); ok {
		t.Fatal("expired grant accepted")
	}
	if _, err := s.openForward(req); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/computer/forwards", nil))
	if response.Code != 200 || !bytes.Contains(response.Body.Bytes(), []byte(req.ID)) {
		t.Fatal("grant not listed")
	}
	if err := s.Revoke(deviceID); err != nil {
		t.Fatal(err)
	}
	if len(s.forwards) != 0 {
		t.Fatal("revocation retained forwards")
	}
	if _, err := s.openForward(req); err == nil {
		t.Fatal("revoked device authorized")
	}
	for range 2 {
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("DELETE", "/computer/forwards/"+req.ID, nil))
		if response.Code != 200 {
			t.Fatal("close not idempotent")
		}
	}
}
