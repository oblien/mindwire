package computer

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func signedPairRequest(t *testing.T, invitation Invitation, key ssh.Signer) PairRequest {
	t.Helper()
	req := PairRequest{ID: randomID(18), Name: "iPhone", PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))}
	return signPairRequest(t, invitation, key, req)
}

func signPairRequest(t *testing.T, invitation Invitation, key ssh.Signer, req PairRequest) PairRequest {
	t.Helper()
	proof, err := key.Sign(rand.Reader, pairingProof(invitation.PairingID, req))
	if err != nil {
		t.Fatal(err)
	}
	req.Signature = base64.StdEncoding.EncodeToString(proof.Blob)
	return req
}

func approveRecoveryPhone(t *testing.T, s *Server, key ssh.Signer) Device {
	t.Helper()
	invitation := invite(t, s)
	req := signedPairRequest(t, invitation, key)
	if offer, err := s.Request(invitation.PairingID, req); err != nil || offer.Status != "pending" {
		t.Fatalf("a fresh key needs owner approval: %v", err)
	}
	device, err := s.Decide(invitation.PairingID, req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func TestSavedPhoneUsesSignedQRWithoutAnotherApprovalOrDevice(t *testing.T) {
	s, _ := fixture(t)
	key := clientKey(t)
	device := approveRecoveryPhone(t, s, key)
	invitation := invite(t, s)
	req := signedPairRequest(t, invitation, key)
	client := connect(t, s, "pair:"+invitation.PairingID, ssh.Password(invitation.Secret), true)
	for range 2 {
		status, body := request(t, client, PairingPort, "POST", "/pair", req)
		var response struct {
			Status string `json:"status"`
			Device Device `json:"device"`
		}
		if status != 200 || json.Unmarshal(body, &response) != nil || response.Status != "approved" || response.Device != device {
			t.Fatal("the saved key did not resume its existing approval")
		}
	}
	s.mu.Lock()
	count, completed := len(s.state.Devices), s.offers[invitation.PairingID].Completed
	s.mu.Unlock()
	if count != 1 || completed {
		t.Fatal("resuming must keep one device and still wait for the phone's saved receipt")
	}
	if s.Info()["pairingVersion"] != PairingVersion || PairingVersion < 3 {
		t.Fatal("the client cannot discover signed pairing support")
	}
	s.mu.Lock()
	s.offers[invitation.PairingID].Completed = true
	s.mu.Unlock()
	if status, _ := request(t, client, PairingPort, "POST", "/pair", req); status != 200 {
		t.Fatal("rescanning a completed invitation failed for its original device")
	}
}

func TestPairingProofCannotImpersonateOrReviveAnApproval(t *testing.T) {
	s, _ := fixture(t)
	key := clientKey(t)
	device := approveRecoveryPhone(t, s, key)
	invitation := invite(t, s)
	req := signedPairRequest(t, invitation, key)
	tampered := req
	tampered.Name = "different name"
	if _, err := s.Request(invitation.PairingID, tampered); err == nil {
		t.Fatal("the proof did not bind the name and request")
	}
	tampered = req
	tampered.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	if _, err := s.Request(invitation.PairingID, tampered); err == nil {
		t.Fatal("a forged proof was accepted")
	}
	other := invite(t, s)
	if _, err := s.Request(other.PairingID, req); err == nil {
		t.Fatal("a proof was replayed into a different QR invitation")
	}
	req.Signature = ""
	if _, err := s.Request(invitation.PairingID, req); err == nil {
		t.Fatal("a known public key without proof was accepted")
	}
	if err := s.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	req = signedPairRequest(t, other, key)
	if offer, err := s.Request(other.PairingID, req); err != nil || offer.Status != "pending" {
		t.Fatal("a revoked phone regained access without approval")
	}
	unknown := invite(t, s)
	if offer, err := s.Request(unknown.PairingID, signedPairRequest(t, unknown, clientKey(t))); err != nil || offer.Status != "pending" {
		t.Fatal("a new key with the same phone name bypassed approval")
	}
}

func TestReplacingLostPhoneKeysIsAtomicAndDisconnectsOnlyThoseKeys(t *testing.T) {
	s, directory := fixture(t)
	oldKey, duplicateKey, otherKey, newKey := clientKey(t), clientKey(t), clientKey(t), clientKey(t)
	old := approveRecoveryPhone(t, s, oldKey)
	duplicate := approveRecoveryPhone(t, s, duplicateKey)
	other := approveRecoveryPhone(t, s, otherKey)
	oldLink := connect(t, s, "device", ssh.PublicKeys(oldKey), false)
	otherLink := connect(t, s, "device", ssh.PublicKeys(otherKey), false)
	closed := make(chan struct{})
	go func() { _ = oldLink.Wait(); close(closed) }()
	invitation := invite(t, s)
	req := signedPairRequest(t, invitation, newKey)
	if _, err := s.Request(invitation.PairingID, req); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	body, _ := json.Marshal(map[string]any{"requestId": req.ID, "approve": true, "replaceDeviceIds": []string{old.ID, duplicate.ID}})
	for range 2 {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("POST", "/computer/pairings/"+invitation.PairingID+"/decision", bytes.NewReader(body)))
		if response.Code != 200 {
			t.Fatal("replacement or its retry failed")
		}
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the replaced key kept an active connection")
	}
	if status, _ := request(t, otherLink, APIPort, "GET", "/healthz", nil); status != 200 {
		t.Fatal("replacing one phone disconnected another approved phone")
	}
	restored, err := New(directory, s.apiAddress, s.apiToken, s.registryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.state.Devices) != 2 || restored.state.Devices[other.ID] != other || restored.state.Devices[keyID(newKey.PublicKey())].ID == "" {
		t.Fatal("the durable device registry did not contain exactly the replacement and unrelated phone")
	}
	for _, key := range []ssh.Signer{oldKey, duplicateKey} {
		config := &ssh.ClientConfig{User: "device", Auth: []ssh.AuthMethod{ssh.PublicKeys(key)}, HostKeyCallback: ssh.FixedHostKey(s.signer.PublicKey()), Timeout: 2 * time.Second}
		if client, err := ssh.Dial("tcp", s.sshListener.Addr().String(), config); err == nil {
			client.Close()
			t.Fatal("a replaced phone key still authenticated")
		}
	}
}

func TestFailedReplacementPreservesTheOldApprovalAndPendingRequest(t *testing.T) {
	s, directory := fixture(t)
	old := approveRecoveryPhone(t, s, clientKey(t))
	invitation := invite(t, s)
	req := signedPairRequest(t, invitation, clientKey(t))
	if _, err := s.Request(invitation.PairingID, req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(invitation.PairingID, req.ID, true, "missing-key"); err == nil {
		t.Fatal("a stale replacement decision was accepted")
	}
	s.path = directory // Renaming a file onto this directory must fail.
	if _, err := s.Decide(invitation.PairingID, req.ID, true, old.ID); err == nil {
		t.Fatal("persistence failure was ignored")
	}
	s.mu.Lock()
	unchanged := len(s.state.Devices) == 1 && s.state.Devices[old.ID] == old && s.offers[invitation.PairingID].Status == "pending"
	s.mu.Unlock()
	if !unchanged {
		t.Fatal("failed replacement changed the old approval")
	}
	s.path = filepath.Join(directory, "computer.json")
	if _, err := s.Decide(invitation.PairingID, req.ID, true, old.ID); err != nil {
		t.Fatal("the same decision could not be retried")
	}
}

func TestClosingAnUnusedQRDoesNotDiscardASimultaneousPhoneRequest(t *testing.T) {
	s, _ := fixture(t)
	mux := http.NewServeMux()
	s.Register(mux)
	closeQR := func(id string) bool {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("DELETE", "/computer/pairings/"+id, nil))
		var result struct{ Cancelled bool }
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatal("could not close the unused invitation")
		}
		return result.Cancelled
	}
	unused := invite(t, s)
	if !closeQR(unused.PairingID) || !closeQR(unused.PairingID) || s.PendingActivity() != 0 {
		t.Fatal("an abandoned QR still reserves the service")
	}
	active := invite(t, s)
	req := signedPairRequest(t, active, clientKey(t))
	if _, err := s.Request(active.PairingID, req); err != nil {
		t.Fatal(err)
	}
	if closeQR(active.PairingID) || s.PendingActivity() == 0 {
		t.Fatal("a submitted phone request was discarded")
	}
}
