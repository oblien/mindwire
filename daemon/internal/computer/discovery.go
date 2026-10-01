package computer

// The directory handles address discovery only. It receives signed ciphertext,
// never provider credentials, SSH private keys or workspace traffic.
import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const discoveryLifetime = 24 * time.Hour
const discoveryRenewAfter = 12 * time.Hour
const discoveryMaxBody = 1 << 20
const maxDiscoverySequence = 9007199254740991

type signedDirectoryEnvelope struct {
	PublicKey string `json:"publicKey"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}
type discoveryState struct {
	URL               string                   `json:"url"`
	Enabled           bool                     `json:"enabled"`
	Sequence          uint64                   `json:"sequence"`
	PublishedSequence uint64                   `json:"publishedSequence"`
	PublishedAt       int64                    `json:"publishedAt,omitempty"`
	IssuedAt          int64                    `json:"issuedAt"`
	InputHash         string                   `json:"inputHash,omitempty"`
	Pending           *signedDirectoryEnvelope `json:"pending,omitempty"`
}
type directoryHeader struct {
	Version     int    `json:"version"`
	Audience    string `json:"audience"`
	DirectoryID string `json:"directoryId"`
	ComputerID  string `json:"computerId"`
	IssuedAt    int64  `json:"issuedAt"`
	ExpiresAt   int64  `json:"expiresAt"`
}
type directoryRecord struct {
	directoryHeader
	RegistryID string `json:"registryId"`
	Sequence   uint64 `json:"sequence"`
	DeviceID   string `json:"deviceId"`
	Ciphertext string `json:"ciphertext"`
}
type directoryReader struct {
	DeviceID  string                  `json:"deviceId"`
	PublicKey string                  `json:"publicKey"`
	Record    signedDirectoryEnvelope `json:"record"`
}
type directoryPublication struct {
	directoryHeader
	Sequence uint64            `json:"sequence"`
	Enabled  bool              `json:"enabled"`
	Readers  []directoryReader `json:"readers"`
}

func validDirectoryURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		!strings.HasSuffix(value, "/") && !strings.ContainsAny(value, "\r\n\x00") &&
		(u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}

func (s *Server) directoryKey(audience, deviceID string) []byte {
	private, _ := base64.StdEncoding.DecodeString(s.state.PrivateKey)
	mac := hmac.New(sha256.New, ed25519.PrivateKey(private).Seed())
	_, _ = mac.Write([]byte("mindwire-directory-key-v1\n" + audience + "\n" + deviceID))
	return mac.Sum(nil)
}
func directoryAAD(audience, directoryID, deviceID string, sequence uint64) []byte {
	return []byte("mindwire-directory-address-v1\n" + audience + "\n" + directoryID + "\n" + deviceID + "\n" + strconv.FormatUint(sequence, 10))
}
func (s *Server) signDirectory(domain string, payload any) (signedDirectoryEnvelope, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return signedDirectoryEnvelope{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	signature, err := s.signer.Sign(rand.Reader, []byte(domain+"\n"+encoded))
	if err != nil {
		return signedDirectoryEnvelope{}, err
	}
	key := s.signer.PublicKey().(ssh.CryptoPublicKey).CryptoPublicKey().(ed25519.PublicKey)
	return signedDirectoryEnvelope{base64.StdEncoding.EncodeToString(key), encoded, base64.StdEncoding.EncodeToString(signature.Blob)}, nil
}

func (s *Server) discoveryStatusLocked() map[string]any {
	status := map[string]any{"version": 1, "enabled": false, "directoryId": keyID(s.signer.PublicKey())}
	if d := s.state.Discovery; d != nil {
		status["enabled"] = d.Enabled
		status["url"] = d.URL
		status["sequence"] = d.Sequence
		status["publishedSequence"] = d.PublishedSequence
		if d.PublishedAt != 0 {
			status["publishedAt"] = d.PublishedAt
		}
		if s.discoveryError != "" {
			status["error"] = s.discoveryError
		}
	}
	return status
}

func (s *Server) discoveryRoutes(register func(string, http.HandlerFunc)) {
	register("POST /computer/discovery/devices/{id}/ready", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		if !decode(w, r, &req) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		device, exists := s.state.Devices[r.PathValue("id")]
		d := s.state.Discovery
		if !exists || device.Revoked || d == nil || !d.Enabled || d.URL != req.URL {
			send(w, 409, map[string]string{"error": "address recovery changed"})
			return
		}
		if device.DirectoryURL != req.URL {
			previous := device
			device.DirectoryURL = req.URL
			s.state.Devices[device.ID] = device
			if err := s.persist(); err != nil {
				s.state.Devices[device.ID] = previous
				send(w, 500, map[string]string{"error": "could not save address recovery receipt"})
				return
			}
		}
		reply := map[string]bool{"ok": true}
		send(w, 200, reply)
	})
	register("GET /computer/discovery", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		send(w, 200, s.discoveryStatusLocked())
	})
	register("POST /computer/discovery/enrollment", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL       string `json:"url"`
			Challenge string `json:"challenge"`
		}
		if !decode(w, r, &req) {
			return
		}
		if !validDirectoryURL(req.URL) || len(req.Challenge) < 16 || len(req.Challenge) > 2048 || strings.ContainsAny(req.Challenge, "\r\n\x00") {
			reject(w, errors.New("invalid directory enrollment"))
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now().Unix()
		proof, err := s.signDirectory("mindwire-directory-enroll-v1", struct {
			directoryHeader
			Challenge string `json:"challenge"`
		}{directoryHeader{1, req.URL, keyID(s.signer.PublicKey()), s.state.ID, now, now + 300}, req.Challenge})
		if err != nil {
			send(w, 500, map[string]string{"error": "could not prepare directory enrollment"})
			return
		}
		send(w, 200, proof)
	})
	register("PUT /computer/discovery", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
		}
		if !decode(w, r, &req) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		previous := s.state.Discovery
		if previous == nil && !req.Enabled {
			send(w, 200, s.discoveryStatusLocked())
			return
		}
		if req.URL == "" && previous != nil {
			req.URL = previous.URL
		}
		if !validDirectoryURL(req.URL) {
			reject(w, errors.New("an HTTPS directory address is required"))
			return
		}
		if previous != nil && previous.URL != req.URL && (previous.Enabled || previous.Sequence != previous.PublishedSequence) {
			reject(w, errors.New("disable the previous directory and wait for withdrawal before changing its address"))
			return
		}
		next := discoveryState{URL: req.URL, Enabled: req.Enabled}
		if previous != nil {
			next = *previous
			next.URL = req.URL
			next.Enabled = req.Enabled
		}
		s.state.Discovery = &next
		if _, err := s.prepareDiscoveryLocked(time.Now()); err != nil {
			s.state.Discovery = previous
			send(w, 500, map[string]string{"error": "could not save address recovery settings"})
			return
		}
		s.discoveryError = ""
		s.startDiscoveryLocked()
		send(w, 200, s.discoveryStatusLocked())
	})
	register("GET /computer/discovery/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		device, exists := s.state.Devices[r.PathValue("id")]
		d := s.state.Discovery
		if d == nil || !d.Enabled || !exists || device.Revoked {
			send(w, 404, map[string]string{"error": "address recovery unavailable"})
			return
		}
		// This response travels only inside authenticated, pinned SSH. Do not add
		// the secret to /computer, invitations, device lists or bootstrap files.
		send(w, 200, map[string]any{"version": 1, "url": d.URL, "directoryId": keyID(s.signer.PublicKey()),
			"deviceId": device.ID, "sequence": d.Sequence, "secret": base64.StdEncoding.EncodeToString(s.directoryKey(d.URL, device.ID))})
	})
}

// prepareDiscoveryLocked persists both the revision and exact encrypted bytes
// before any network request. A crash/lost acknowledgement replays those bytes.
func (s *Server) prepareDiscoveryLocked(now time.Time) (*discoveryState, error) {
	d := s.state.Discovery
	if d == nil {
		return nil, nil
	}
	devices := []Device{}
	if d.Enabled {
		for _, device := range s.state.Devices {
			if !device.Revoked {
				devices = append(devices, device)
			}
		}
	}
	slices.SortFunc(devices, func(a, b Device) int { return strings.Compare(a.ID, b.ID) })
	type binding struct {
		ID        string
		PublicKey string
	}
	bindings := []binding{}
	for _, device := range devices {
		bindings = append(bindings, binding{device.ID, device.PublicKey})
	}
	routes, connection := s.state.Routes, s.state.Connection
	if !d.Enabled {
		routes = nil
		connection = nil
	}
	input, err := json.Marshal(struct {
		URL        string
		Enabled    bool
		Registry   string
		Routes     []Route
		Connection *ConnectionMetadata
		Devices    []binding
	}{d.URL, d.Enabled, s.registryID, routes, connection, bindings})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(input)
	hash := base64.StdEncoding.EncodeToString(digest[:])
	if d.Pending != nil && d.InputHash == hash && (!d.Enabled && d.PublishedSequence == d.Sequence || now.Unix() < d.IssuedAt+int64(discoveryRenewAfter.Seconds())) {
		copy := *d
		return &copy, nil
	}
	if d.Sequence >= maxDiscoverySequence {
		return nil, errors.New("directory revision exhausted")
	}
	sequence := d.Sequence + 1
	header := directoryHeader{1, d.URL, keyID(s.signer.PublicKey()), s.state.ID, now.Unix(), now.Add(discoveryLifetime).Unix()}
	publication := directoryPublication{header, sequence, d.Enabled, []directoryReader{}}
	addresses, err := json.Marshal(struct {
		Routes     []Route             `json:"routes"`
		Connection *ConnectionMetadata `json:"connection,omitempty"`
	}{append([]Route{}, s.state.Routes...), s.state.Connection})
	if err != nil || len(addresses) > 32*1024 {
		return nil, errors.New("directory address record too large")
	}
	for _, device := range devices {
		public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(device.PublicKey))
		if err != nil {
			return nil, errors.New("invalid approved device key")
		}
		cryptoPublic, ok := public.(ssh.CryptoPublicKey)
		if !ok {
			return nil, errors.New("invalid approved device key")
		}
		raw, ok := cryptoPublic.CryptoPublicKey().(ed25519.PublicKey)
		if !ok {
			return nil, errors.New("invalid approved device key")
		}
		block, err := aes.NewCipher(s.directoryKey(d.URL, device.ID))
		if err != nil {
			return nil, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return nil, err
		}
		ciphertext := gcm.Seal(nonce, nonce, addresses, directoryAAD(d.URL, header.DirectoryID, device.ID, sequence))
		record, err := s.signDirectory("mindwire-directory-record-v1", directoryRecord{header, s.registryID, sequence, device.ID, base64.StdEncoding.EncodeToString(ciphertext)})
		if err != nil {
			return nil, err
		}
		publication.Readers = append(publication.Readers, directoryReader{device.ID, base64.StdEncoding.EncodeToString(raw), record})
	}
	envelope, err := s.signDirectory("mindwire-directory-publish-v1", publication)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil || len(encoded) > discoveryMaxBody {
		return nil, errors.New("directory publication too large")
	}
	next := *d
	next.Sequence = sequence
	next.InputHash = hash
	next.Pending = &envelope
	next.IssuedAt = header.IssuedAt
	s.state.Discovery = &next
	if err = s.persist(); err != nil {
		s.state.Discovery = d
		return nil, err
	}
	copy := next
	return &copy, nil
}

func (s *Server) startDiscoveryLocked() {
	if s.discoveryWake == nil {
		ctx, cancel := context.WithCancel(context.Background())
		s.discoveryCancel = cancel
		s.discoveryWake = make(chan struct{}, 1)
		s.discoveryDone = make(chan struct{})
		go func() { defer close(s.discoveryDone); s.runDiscovery(ctx, s.discoveryWake) }()
	}
	select {
	case s.discoveryWake <- struct{}{}:
	default:
	}
}

func publishDirectory(ctx context.Context, d *discoveryState) error {
	if !validDirectoryURL(d.URL) || d.Pending == nil {
		return errors.New("invalid address recovery configuration")
	}
	data, err := json.Marshal(d.Pending)
	if err != nil {
		return err
	}
	var header directoryHeader
	payload, err := base64.StdEncoding.DecodeString(d.Pending.Payload)
	if err != nil || json.Unmarshal(payload, &header) != nil {
		return errors.New("invalid saved address publication")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "PUT", d.URL+"/records/"+header.DirectoryID, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid address recovery configuration")
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("Address recovery is waiting for its directory. Saved addresses still work.")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 404 {
		return errors.New("Address recovery needs enrollment. Run mindwire discovery enable on this computer.")
	}
	if response.StatusCode == 409 {
		return errors.New("The directory rejected an older revision. Check this computer's saved Mindwire state.")
	}
	if response.StatusCode != 200 && response.StatusCode != 201 {
		return fmt.Errorf("Address directory unavailable (HTTP %d). Retrying automatically.", response.StatusCode)
	}
	var receipt struct {
		Sequence uint64 `json:"sequence"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&receipt) != nil || receipt.Sequence != d.Sequence {
		return errors.New("The directory did not acknowledge this address revision. Retrying automatically.")
	}
	return nil
}

func (s *Server) runDiscovery(ctx context.Context, wake <-chan struct{}) {
	failures := 0
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-wake:
		default:
		}
		s.mu.Lock()
		d, err := s.prepareDiscoveryLocked(time.Now())
		s.mu.Unlock()
		wait := discoveryRenewAfter
		if err == nil && d != nil && d.PublishedSequence != d.Sequence {
			err = publishDirectory(ctx, d)
			if err == nil {
				s.mu.Lock()
				current := s.state.Discovery
				if current != nil && current.Sequence == d.Sequence && current.URL == d.URL {
					next := *current
					next.PublishedSequence = d.Sequence
					next.PublishedAt = time.Now().Unix()
					s.state.Discovery = &next
					if err = s.persist(); err != nil {
						s.state.Discovery = current
					}
				}
				s.mu.Unlock()
			}
		}
		if err != nil {
			failures++
			wait = time.Second * time.Duration(1<<min(6, failures-1))
		} else {
			failures = 0
			if d != nil && d.Enabled {
				wait = max(time.Second, time.Until(time.Unix(d.IssuedAt, 0).Add(discoveryRenewAfter)))
			}
		}
		s.mu.Lock()
		if err != nil {
			s.discoveryError = err.Error()
		} else {
			s.discoveryError = ""
		}
		s.mu.Unlock()
		if err == nil && d != nil && !d.Enabled {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				continue
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
