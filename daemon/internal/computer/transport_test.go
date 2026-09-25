package computer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"
)

func fixture(t *testing.T) (*Server, string) {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(401)
			return
		}
		_, _ = io.WriteString(w, "workspace-ready")
	}))
	t.Cleanup(api.Close)
	dir := t.TempDir()
	s, err := New(dir, api.Listener.Addr().String(), "fixture-token", "registry-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start("127.0.0.1:0", "127.0.0.1:0", dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, dir
}
func clientKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func invite(t *testing.T, s *Server) Invitation {
	t.Helper()
	inv, err := s.Invite([]Route{{Kind: "ssh", Host: "127.0.0.1", Port: s.sshListener.Addr().(*net.TCPAddr).Port}})
	if err != nil {
		t.Fatal(err)
	}
	return inv
}
func connect(t *testing.T, s *Server, user string, auth ssh.AuthMethod, ws bool) *ssh.Client {
	t.Helper()
	config := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: ssh.FixedHostKey(s.signer.PublicKey()), Timeout: 2 * time.Second}
	var client *ssh.Client
	var err error
	if ws {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		url := "ws://" + s.wsListener.Addr().String() + "/ssh"
		web, _, e := websocket.Dial(ctx, url, nil)
		if e != nil {
			t.Fatal(e)
		}
		conn, channels, requests, e := ssh.NewClientConn(websocket.NetConn(ctx, web, websocket.MessageBinary), url, config)
		if e != nil {
			t.Fatal(e)
		}
		client = ssh.NewClient(conn, channels, requests)
	} else {
		client, err = ssh.Dial("tcp", s.sshListener.Addr().String(), config)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
func request(t *testing.T, client *ssh.Client, port int, method, path string, body any) (int, []byte) {
	t.Helper()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return client.Dial(network, "127.0.0.1:"+strconv.Itoa(port))
	}}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, "http://workspace"+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer fixture-token")
	response, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	out, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, out
}

func TestPairingCannotAccessWorkspaceUntilSpecificDeviceApproved(t *testing.T) {
	s, _ := fixture(t)
	inv := invite(t, s)
	key := clientKey(t)
	pair := connect(t, s, "pair:"+inv.PairingID, ssh.Password(inv.Secret), false)
	if c, err := pair.Dial("tcp", "127.0.0.1:8790"); err == nil {
		c.Close()
		t.Fatal("pairing connection reached workspace")
	}
	if session, err := pair.NewSession(); err == nil {
		session.Close()
		t.Fatal("pairing connection opened a shell")
	}
	req := PairRequest{ID: "fixture-request-1", Name: "Test iPhone", PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))}
	status, body := request(t, pair, PairingPort, "POST", "/pair", req)
	if status != 200 || !bytes.Contains(body, []byte(`"status":"pending"`)) || bytes.Contains(body, []byte("daemonToken")) {
		t.Fatalf("unexpected pending response: status=%d", status)
	}
	changed := req
	changed.ID = "different-request"
	if status, _ = request(t, pair, PairingPort, "POST", "/pair", changed); status < 400 {
		t.Fatal("invitation accepted a different device request")
	}
	if _, err := s.Decide(inv.PairingID, "wrong-request", true); err == nil {
		t.Fatal("approved an unseen request")
	}
	device, err := s.Decide(inv.PairingID, req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	pair.Close()
	// Lost acknowledgement: the same invitation and request can recover approval.
	retry := connect(t, s, "pair:"+inv.PairingID, ssh.Password(inv.Secret), false)
	status, body = request(t, retry, PairingPort, "POST", "/pair", req)
	if status != 200 || !bytes.Contains(body, []byte(`"status":"approved"`)) {
		t.Fatal("could not recover approval")
	}
	approved := connect(t, s, "mindwire", ssh.PublicKeys(key), false)
	status, body = request(t, approved, APIPort, "GET", "/healthz", nil)
	if status != 200 || string(body) != "workspace-ready" {
		t.Fatal("approved workspace not reachable")
	}
	if c, err := approved.Dial("tcp", "127.0.0.1:22"); err == nil {
		c.Close()
		t.Fatal("unrestricted port forwarding")
	}
	if err = s.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = approved.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("revocation left an authenticated connection alive")
	}
	config := &ssh.ClientConfig{User: "mindwire", Auth: []ssh.AuthMethod{ssh.PublicKeys(key)}, HostKeyCallback: ssh.FixedHostKey(s.signer.PublicKey()), Timeout: time.Second}
	if c, err := ssh.Dial("tcp", s.sshListener.Addr().String(), config); err == nil {
		c.Close()
		t.Fatal("revoked device reconnected")
	}
}

func TestWebSocketCarriesTheSamePinnedSSHProtocol(t *testing.T) {
	s, _ := fixture(t)
	inv := invite(t, s)
	key := clientKey(t)
	pair := connect(t, s, "pair:"+inv.PairingID, ssh.Password(inv.Secret), true)
	req := PairRequest{ID: "websocket-device", Name: "Android protocol client", PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))}
	if status, _ := request(t, pair, PairingPort, "POST", "/pair", req); status != 200 {
		t.Fatal(status)
	}
	if _, err := s.Decide(inv.PairingID, req.ID, true); err != nil {
		t.Fatal(err)
	}
	approved := connect(t, s, "mindwire", ssh.PublicKeys(key), true)
	if status, body := request(t, approved, APIPort, "GET", "/healthz", nil); status != 200 || string(body) != "workspace-ready" {
		t.Fatal("WebSocket workspace request failed")
	}
	config := &ssh.ClientConfig{User: "mindwire", Auth: []ssh.AuthMethod{ssh.PublicKeys(key)}, HostKeyCallback: ssh.FixedHostKey(clientKey(t).PublicKey()), Timeout: time.Second}
	if c, err := ssh.Dial("tcp", s.sshListener.Addr().String(), config); err == nil {
		c.Close()
		t.Fatal("wrong computer fingerprint was accepted")
	}
}

func TestIdentityAndRevocationsSurviveRestartAndExpiredInvitesFail(t *testing.T) {
	s, dir := fixture(t)
	inv := invite(t, s)
	key := clientKey(t)
	req := PairRequest{ID: "persisted-device", Name: "Phone", PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))}
	if _, err := s.Request(inv.PairingID, req); err != nil {
		t.Fatal(err)
	}
	device, err := s.Decide(inv.PairingID, req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.offers[inv.PairingID].ExpiresAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if s.authenticatePair(inv.PairingID, []byte(inv.Secret)) {
		t.Fatal("expired invitation authenticated")
	}
	restored, err := New(dir, s.apiAddress, s.apiToken, s.registryID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Fingerprint() != s.Fingerprint() || !restored.state.Devices[device.ID].Revoked {
		t.Fatal("computer identity or revocation was lost")
	}
	data, _ := json.Marshal(restored.Info())
	if bytes.Contains(data, []byte("privateKey")) || bytes.Contains(data, []byte(s.apiToken)) {
		t.Fatal("public info leaked credentials")
	}
}

func TestPairingReservesServiceUntilPhoneAcknowledgesApproval(t *testing.T) {
	s, _ := fixture(t)
	if s.PendingActivity() != 0 {
		t.Fatal("unused service is busy")
	}
	inv := invite(t, s)
	key := clientKey(t)
	req := PairRequest{ID: "approval-reservation", Name: "Phone", PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))}
	if _, err := s.Request(inv.PairingID, req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(inv.PairingID, req.ID, true); err != nil {
		t.Fatal(err)
	}
	if s.PendingActivity() == 0 {
		t.Fatal("update could discard an approval before the phone received it")
	}
	mux := http.NewServeMux()
	s.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("POST", "/computer/pairings/"+inv.PairingID+"/complete",
		bytes.NewBufferString(`{"requestId":"approval-reservation"}`)))
	if response.Code != 200 || s.PendingActivity() != 0 {
		t.Fatalf("completed pairing still reserves update: %d", response.Code)
	}
}

func TestComputerOwnershipLockRejectsDuplicateAndReleases(t *testing.T) {
	directory := t.TempDir()
	unlock, err := Lock(directory)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Lock(directory); err == nil {
		second()
		unlock()
		t.Fatal("two daemons could own the same computer identity")
	}
	unlock()
	next, err := Lock(directory)
	if err != nil {
		t.Fatal(err)
	}
	next()
}
