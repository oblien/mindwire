package computer

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestConnectionPolicyPersistsWithRoutesAndKeepsPairingIdentity(t *testing.T) {
	s, directory := fixture(t)
	fingerprint := s.Fingerprint()
	mux := http.NewServeMux()
	s.Register(mux)
	put := func(body string) int {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/computer/routes", bytes.NewBufferString(body)))
		return w.Code
	}
	if code := put(`{"routes":[{"kind":"websocket","url":"wss://computer.example/ssh"}],"connection":{"provider":"oblien","address":"persistent"}}`); code != http.StatusOK {
		t.Fatalf("publish returned %d", code)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/computer", nil))
	var info struct {
		Connection map[string]string `json:"connection"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info.Connection, map[string]string{"provider": "oblien", "address": "persistent"}) {
		t.Fatal("public policy should contain only the provider and address lifetime")
	}
	restored, err := New(directory, s.apiAddress, s.apiToken, s.registryID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.Fingerprint() != fingerprint || !reflect.DeepEqual(restored.state.Connection, s.state.Connection) || !reflect.DeepEqual(restored.state.Routes, s.state.Routes) {
		t.Fatal("connection restart lost policy, routes, or host identity")
	}
	before := append([]Route(nil), s.state.Routes...)
	for _, policy := range []string{`{"provider":"untrusted-provider","address":"persistent"}`, `{"provider":"oblien","address":"forever"}`} {
		if code := put(`{"routes":[],"connection":` + policy + `}`); code != http.StatusBadRequest {
			t.Fatalf("invalid policy accepted: %d", code)
		}
		if !reflect.DeepEqual(s.state.Routes, before) || s.state.Connection.Provider != "oblien" {
			t.Fatal("invalid policy partially changed routes")
		}
	}
	// A failed disk write rolls back both halves, without invalidating a phone.
	originalPath := s.path
	s.path = filepath.Join(directory, "missing-directory", "computer.json")
	code := put(`{"routes":[],"connection":{"provider":"cloudflare","address":"temporary"}}`)
	s.path = originalPath
	if code != http.StatusInternalServerError || !reflect.DeepEqual(s.state.Routes, before) || s.state.Connection.Provider != "oblien" {
		t.Fatal("failed persistence did not roll back routes and policy together")
	}
	if code := put(`{"routes":[]}`); code != http.StatusOK || s.state.Connection.Provider != "oblien" {
		t.Fatal("a legacy controller erased the saved connection policy")
	}
}

func TestControllerWithdrawsOfflineRoutesWithoutLosingIdentity(t *testing.T) {
	s, _ := fixture(t)
	inv := invite(t, s)
	key := clientKey(t)
	req := signPairRequest(t, inv, key, PairRequest{ID: "fixture-route-phone", Name: "Phone", PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))})
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
