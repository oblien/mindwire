package computer

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func directoryPhone(t *testing.T, s *Server, name string) Device {
	t.Helper()
	inv := invite(t, s)
	key := clientKey(t)
	req := signPairRequest(t, inv, key, PairRequest{ID: "directory-request-" + name, Name: name, PublicKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))})
	if _, err := s.Request(inv.PairingID, req); err != nil {
		t.Fatal(err)
	}
	device, err := s.Decide(inv.PairingID, req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func decodeDirectory(t *testing.T, s *Server, domain string, envelope signedDirectoryEnvelope, value any) {
	t.Helper()
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || s.signer.PublicKey().Verify([]byte(domain+"\n"+envelope.Payload), &ssh.Signature{Format: ssh.KeyAlgoED25519, Blob: signature}) != nil {
		t.Fatal("invalid directory signature")
	}
	data, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil || json.Unmarshal(data, value) != nil {
		t.Fatal("invalid directory payload")
	}
}

func TestDirectoryEncryptsPerPhoneAndReusesDurablePendingRevision(t *testing.T) {
	s, dir := fixture(t)
	one := directoryPhone(t, s, "first")
	two := directoryPhone(t, s, "second")
	s.state.Discovery = &discoveryState{URL: "https://directory.example/v1", Enabled: true}
	now := time.Now()
	first, err := s.prepareDiscoveryLocked(now)
	if err != nil {
		t.Fatal(err)
	}
	var publication directoryPublication
	decodeDirectory(t, s, "mindwire-directory-publish-v1", *first.Pending, &publication)
	if len(publication.Readers) != 2 || publication.Sequence != 1 {
		t.Fatal("missing encrypted readers")
	}
	for _, reader := range publication.Readers {
		var record directoryRecord
		decodeDirectory(t, s, "mindwire-directory-record-v1", reader.Record, &record)
		ciphertext, err := base64.StdEncoding.DecodeString(record.Ciphertext)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := aes.NewCipher(s.directoryKey(first.URL, reader.DeviceID))
		gcm, _ := cipher.NewGCM(block)
		plaintext, err := gcm.Open(nil, ciphertext[:12], ciphertext[12:], directoryAAD(first.URL, publication.DirectoryID, reader.DeviceID, first.Sequence))
		if err != nil {
			t.Fatal(err)
		}
		var addresses struct {
			Routes []Route `json:"routes"`
		}
		if json.Unmarshal(plaintext, &addresses) != nil || !reflect.DeepEqual(addresses.Routes, s.state.Routes) {
			t.Fatal("encrypted routes lost data")
		}
		other := one.ID
		if other == reader.DeviceID {
			other = two.ID
		}
		block, _ = aes.NewCipher(s.directoryKey(first.URL, other))
		gcm, _ = cipher.NewGCM(block)
		if _, err := gcm.Open(nil, ciphertext[:12], ciphertext[12:], directoryAAD(first.URL, publication.DirectoryID, reader.DeviceID, first.Sequence)); err == nil {
			t.Fatal("another phone decrypted this record")
		}
	}
	retry, err := s.prepareDiscoveryLocked(now.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatal("retry re-encrypted or changed the pending revision")
	}
	var disk state
	data, err := os.ReadFile(filepath.Join(dir, "computer.json"))
	if err != nil || json.Unmarshal(data, &disk) != nil || !reflect.DeepEqual(disk.Discovery, first) {
		t.Fatal("revision was not durable before publish")
	}
	// A restarted worker must use the exact pending ciphertext/signature.
	s.state.Discovery = disk.Discovery
	retry, err = s.prepareDiscoveryLocked(now.Add(2 * time.Hour))
	if err != nil || !reflect.DeepEqual(first.Pending, retry.Pending) {
		t.Fatal("restart changed the unacknowledged publication")
	}
	refreshed, err := s.prepareDiscoveryLocked(now.Add(discoveryRenewAfter))
	if err != nil || refreshed.Sequence != first.Sequence+1 || refreshed.Pending.Payload == first.Pending.Payload {
		t.Fatal("expiry renewal did not advance")
	}
}

func TestDirectoryFailedPersistenceDoesNotPublishOrAdvance(t *testing.T) {
	s, dir := fixture(t)
	directoryPhone(t, s, "phone")
	s.state.Discovery = &discoveryState{URL: "https://directory.example/v1", Enabled: true}
	initial, err := s.prepareDiscoveryLocked(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.state.Routes = []Route{{Kind: "websocket", URL: "wss://new.example/ssh"}}
	s.path = filepath.Join(dir, "not-created", "computer.json")
	if _, err := s.prepareDiscoveryLocked(time.Now()); err == nil {
		t.Fatal("expected disk failure")
	}
	if !reflect.DeepEqual(s.state.Discovery, initial) {
		t.Fatal("failed write advanced discovery state")
	}
}

func TestDirectoryOfflineWithdrawalRenewsUntilAckThenDoesNoIdlePublication(t *testing.T) {
	s, _ := fixture(t)
	s.state.Discovery = &discoveryState{URL: "https://directory.example/v1", Enabled: false}
	now := time.Now()
	initial, err := s.prepareDiscoveryLocked(now)
	if err != nil {
		t.Fatal(err)
	}
	// A laptop can be offline beyond the withdrawal's signature lifetime. Its
	// next attempt needs a fresh revision, not an expired proof forever.
	refreshed, err := s.prepareDiscoveryLocked(now.Add(25 * time.Hour))
	if err != nil || refreshed.Sequence != initial.Sequence+1 {
		t.Fatal("offline withdrawal was not renewed")
	}
	s.state.Discovery.PublishedSequence = refreshed.Sequence
	s.state.Routes = []Route{{Kind: "websocket", URL: "wss://unrelated-new-carrier.example/ssh"}}
	idle, err := s.prepareDiscoveryLocked(now.Add(100 * time.Hour))
	if err != nil || idle.Sequence != refreshed.Sequence {
		t.Fatal("disabled discovery published an unrelated route or idle renewal")
	}
}

func TestDirectoryPublishesRevocationAndNeverExposesEncryptionKeysInInfo(t *testing.T) {
	s, _ := fixture(t)
	device := directoryPhone(t, s, "revoked")
	s.state.Discovery = &discoveryState{URL: "https://directory.example/v1", Enabled: true}
	before, err := s.prepareDiscoveryLocked(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	secret := base64.StdEncoding.EncodeToString(s.directoryKey(before.URL, device.ID))
	info, _ := json.Marshal(s.Info())
	if bytes.Contains(info, []byte(secret)) || bytes.Contains(info, []byte("privateKey")) {
		t.Fatal("info leaked a secret")
	}
	if err := s.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	after, err := s.prepareDiscoveryLocked(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var publication directoryPublication
	decodeDirectory(t, s, "mindwire-directory-publish-v1", *after.Pending, &publication)
	if publication.Sequence <= before.Sequence || len(publication.Readers) != 0 {
		t.Fatal("revocation did not remove reader atomically")
	}
	mux := http.NewServeMux()
	s.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/computer/discovery/devices/"+device.ID, nil))
	if w.Code != 404 {
		t.Fatal("revoked phone obtained an encryption key")
	}
}

func TestDirectoryWorkerRetriesLostReceiptAndCoalescesConcurrentUpdates(t *testing.T) {
	s, _ := fixture(t)
	directoryPhone(t, s, "live")
	var mu sync.Mutex
	var received []signedDirectoryEnvelope
	hosted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope signedDirectoryEnvelope
		if json.NewDecoder(r.Body).Decode(&envelope) != nil {
			w.WriteHeader(400)
			return
		}
		var publication directoryPublication
		decodeDirectory(t, s, "mindwire-directory-publish-v1", envelope, &publication)
		mu.Lock()
		received = append(received, envelope)
		count := len(received)
		mu.Unlock()
		if count == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		send(w, 200, map[string]uint64{"sequence": publication.Sequence})
	}))
	defer hosted.Close()
	mux := http.NewServeMux()
	s.Register(mux)
	put := func(path string, body any) int {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("PUT", path, bytes.NewReader(data)))
		return w.Code
	}
	if put("/computer/discovery", map[string]any{"url": hosted.URL, "enabled": true}) != 200 {
		t.Fatal("enable failed")
	}
	await := func() {
		t.Helper()
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			s.mu.Lock()
			d := *s.state.Discovery
			s.mu.Unlock()
			if d.Sequence > 0 && d.PublishedSequence == d.Sequence {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("directory failed to acknowledge")
	}
	await()
	mu.Lock()
	if len(received) < 2 || !reflect.DeepEqual(received[0], received[1]) {
		t.Error("lost receipt retry changed encrypted bytes")
	}
	mu.Unlock()
	var writers sync.WaitGroup
	for i := 0; i < 16; i++ {
		writers.Go(func() {
			if put("/computer/routes", map[string]any{"routes": []Route{{Kind: "websocket", URL: "wss://replacement.example/ssh"}}}) != 200 {
				t.Error("route write failed")
			}
		})
	}
	writers.Wait()
	// A queued wake may not yet have allocated its next revision.
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		s.mu.Lock()
		d := *s.state.Discovery
		s.mu.Unlock()
		if d.Sequence > 1 && d.PublishedSequence == d.Sequence {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	await()
	s.mu.Lock()
	final := *s.state.Discovery
	s.mu.Unlock()
	if final.Sequence != 2 {
		t.Fatalf("identical concurrent writes created %d revisions", final.Sequence)
	}
	s.Close()
}
