package computer

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestPairingAcknowledgementRequiresTheApprovedKeyProof(t *testing.T) {
	s, _ := fixture(t)
	inv := invite(t, s)
	req := signedPairRequest(t, inv, clientKey(t))
	pair := connect(t, s, "pair:"+inv.PairingID, ssh.Password(inv.Secret), false)
	if status, _ := request(t, pair, PairingPort, "POST", "/pair", req); status != http.StatusOK {
		t.Fatal("could not request approval")
	}
	if _, err := s.Decide(inv.PairingID, req.ID, true); err != nil {
		t.Fatal(err)
	}
	t.Run("unsigned replay", func(t *testing.T) {
		unsigned := req
		unsigned.Signature = ""
		status, body := request(t, pair, PairingPort, "POST", "/pair", unsigned)
		if status < 400 || bytes.Contains(body, []byte("daemonToken")) {
			t.Fatal("public request fields without key proof released an approved credential")
		}
	})
	t.Run("status lookup", func(t *testing.T) {
		status, body := request(t, pair, PairingPort, "GET", "/pair/"+req.ID, nil)
		if status != http.StatusOK || !bytes.Contains(body, []byte(`"status":"approved"`)) {
			t.Fatal("pairing status is unavailable")
		}
		if bytes.Contains(body, []byte("daemonToken")) || bytes.Contains(body, []byte(s.apiToken)) {
			t.Fatal("a request ID alone released an approved credential")
		}
	})
	// A genuine phone can still recover a lost acknowledgement idempotently.
	for range 2 {
		status, body := request(t, pair, PairingPort, "POST", "/pair", req)
		if status != http.StatusOK || !bytes.Contains(body, []byte(`"daemonToken"`)) {
			t.Fatal("the original signed request could not recover its approval")
		}
	}
}

func TestPairingRequestIDsCannotInjectTheOwnerApprovalCommand(t *testing.T) {
	s, _ := fixture(t)
	key := clientKey(t)
	for _, id := range []string{"request;whoami", "$(whoami)-request", "`whoami`-request", "request id", "request/../id", "request\"id"} {
		inv := invite(t, s)
		req := signedPairRequest(t, inv, key)
		req.ID = id
		proof, err := key.Sign(rand.Reader, pairingProof(inv.PairingID, req))
		if err != nil {
			t.Fatal(err)
		}
		req.Signature = base64.StdEncoding.EncodeToString(proof.Blob)
		if _, err := s.Request(inv.PairingID, req); err == nil {
			t.Errorf("accepted an unsafe approval request ID: %q", id)
		}
	}
}

func TestPairingOwnerStatusDoesNotRetainAReplayableProof(t *testing.T) {
	s, _ := fixture(t)
	inv := invite(t, s)
	req := signedPairRequest(t, inv, clientKey(t))
	if _, err := s.Request(inv.PairingID, req); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/computer/pairings/"+inv.PairingID, nil))
	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte(req.Signature)) || bytes.Contains(response.Body.Bytes(), []byte(`"signature"`)) {
		t.Fatal("owner status exposed a reusable pairing proof to logs")
	}
}

func TestRevokingPhoneInvalidatesPairingRecoveryAndConnections(t *testing.T) {
	s, _ := fixture(t)
	key := clientKey(t)
	device := approveRecoveryPhone(t, s, key)
	inv := invite(t, s)
	req := signedPairRequest(t, inv, key)
	if offer, err := s.Request(inv.PairingID, req); err != nil || offer.Status != "approved" {
		t.Fatal("saved key could not request recovery")
	}
	pair := connect(t, s, "pair:"+inv.PairingID, ssh.Password(inv.Secret), false)
	if err := s.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	if s.authenticatePair(inv.PairingID, []byte(inv.Secret)) {
		t.Error("a revoked phone retained an approved pairing invitation")
	}
	done := make(chan struct{})
	go func() { _ = pair.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("revocation left the device's pairing transport open")
	}
	response := httptest.NewRecorder()
	s.PairHandler(inv.PairingID).ServeHTTP(response, httptest.NewRequest("GET", "/pair/"+req.ID, nil))
	if bytes.Contains(response.Body.Bytes(), []byte("daemonToken")) {
		t.Fatal("revoked phone recovered a credential")
	}
}

func TestPairingAcknowledgementCannotOutliveAReplacedDevice(t *testing.T) {
	s, _ := fixture(t)
	oldKey := clientKey(t)
	old := approveRecoveryPhone(t, s, oldKey)
	oldInvite := invite(t, s)
	oldRequest := signedPairRequest(t, oldInvite, oldKey)
	if _, err := s.Request(oldInvite.PairingID, oldRequest); err != nil {
		t.Fatal(err)
	}
	newInvite := invite(t, s)
	newRequest := signedPairRequest(t, newInvite, clientKey(t))
	if _, err := s.Request(newInvite.PairingID, newRequest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(newInvite.PairingID, newRequest.ID, true, old.ID); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.PairHandler(oldInvite.PairingID).ServeHTTP(response, httptest.NewRequest("GET", "/pair/"+oldRequest.ID, nil))
	if bytes.Contains(response.Body.Bytes(), []byte("daemonToken")) {
		t.Fatal("removing a replaced device left its credential acknowledgement usable")
	}
	if s.authenticatePair(oldInvite.PairingID, []byte(oldInvite.Secret)) {
		t.Fatal("a replaced key retained an approved pairing invitation")
	}
}

func TestPairingConnectionEndsWhenItsInvitationExpires(t *testing.T) {
	for _, ws := range []bool{false, true} {
		name := "direct"
		if ws {
			name = "websocket"
		}
		t.Run(name, func(t *testing.T) {
			s, _ := fixture(t)
			inv := invite(t, s)
			s.mu.Lock()
			s.offers[inv.PairingID].ExpiresAt = time.Now().Add(700 * time.Millisecond)
			s.mu.Unlock()
			pair := connect(t, s, "pair:"+inv.PairingID, ssh.Password(inv.Secret), ws)
			done := make(chan struct{})
			go func() { _ = pair.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("an expired pairing invitation kept an authenticated connection alive")
			}
		})
	}
}
