package computer

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestControllerWithdrawsOfflineRoutesWithoutLosingIdentity(t *testing.T) {
	s, _ := fixture(t)
	inv := invite(t, s)
	key := clientKey(t)
	req := PairRequest{ID: "fixture-route-phone", Name: "Phone", PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))}
	if _, err := s.Request(inv.PairingID, req); err != nil {
		t.Fatal(err)
	}
	device, err := s.Decide(inv.PairingID, req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/computer/routes", bytes.NewBufferString(`{"routes":[]}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("withdrawal returned %d", w.Code)
	}
	s.mu.Lock()
	if len(s.state.Routes) != 0 || s.state.ID != inv.ComputerID || s.state.Devices[device.ID].Revoked {
		t.Error("withdrawing stale addresses changed pairing identity")
	}
	s.mu.Unlock()
	if _, err := s.Invite(nil); err == nil {
		t.Fatal("pairing must still require a reachable address")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/computer/routes", bytes.NewBufferString(`{"routes":[{"kind":"websocket","url":"http://unsafe.example"}]}`)))
	if w.Code < 400 {
		t.Fatal("route withdrawal relaxed relay validation")
	}
}
