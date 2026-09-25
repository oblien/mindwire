// Package computer adds authenticated transports to the existing workspace API.
// The relay sees an SSH ciphertext stream, never daemon HTTP or terminal content.
package computer

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const Version = 1
const APIPort = 8790
const PairingPort = 8793

type Route struct {
	Kind string `json:"kind"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	URL  string `json:"url,omitempty"`
}
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PublicKey string    `json:"publicKey"`
	CreatedAt time.Time `json:"createdAt"`
	Revoked   bool      `json:"revoked"`
}
type state struct {
	ID         string            `json:"id"`
	PrivateKey string            `json:"privateKey"`
	Devices    map[string]Device `json:"devices"`
	Routes     []Route           `json:"routes"`
}
type Invitation struct {
	Version     int       `json:"version"`
	ComputerID  string    `json:"computerId"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"`
	Routes      []Route   `json:"routes"`
	PairingID   string    `json:"pairingId"`
	Secret      string    `json:"secret"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
type PairRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}
type Offer struct {
	ID        string       `json:"id"`
	ExpiresAt time.Time    `json:"expiresAt"`
	Request   *PairRequest `json:"request,omitempty"`
	Status    string       `json:"status"`
	Completed bool         `json:"completed,omitempty"`
	secret    [32]byte
	deviceID  string
}

type Server struct {
	mu          sync.Mutex
	path        string
	state       state
	signer      ssh.Signer
	offers      map[string]*Offer
	connections map[*ssh.ServerConn]string
	limit       chan struct{}
	apiAddress  string
	apiToken    string
	sshListener net.Listener
	wsListener  net.Listener
	wsServer    *http.Server
	closed      bool
	registryID  string
	forwards    map[string]*portForward
}

func randomID(n int) string {
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}
func keyID(key ssh.PublicKey) string {
	hash := sha256.Sum256(key.Marshal())
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func New(directory, apiAddress, apiToken, registryID string) (*Server, error) {
	host, _, err := net.SplitHostPort(apiAddress)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || apiToken == "" {
		return nil, errors.New("computer API must use authenticated loopback HTTP")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	s := &Server{path: filepath.Join(directory, "computer.json"), offers: map[string]*Offer{}, connections: map[*ssh.ServerConn]string{}, forwards: map[string]*portForward{}, limit: make(chan struct{}, 64), apiAddress: apiAddress, apiToken: apiToken, registryID: registryID}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		s.state = state{ID: randomID(18), PrivateKey: base64.StdEncoding.EncodeToString(key), Devices: map[string]Device{}}
		if err = s.persist(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if err = json.Unmarshal(data, &s.state); err != nil {
		return nil, err
	}
	if s.state.Devices == nil {
		s.state.Devices = map[string]Device{}
	}
	key, err := base64.StdEncoding.DecodeString(s.state.PrivateKey)
	if err != nil || len(key) != ed25519.PrivateKeySize || s.state.ID == "" {
		return nil, errors.New("invalid saved computer identity")
	}
	s.signer, err = ssh.NewSignerFromKey(ed25519.PrivateKey(key))
	if err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Server) persist() error {
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".computer-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}
func (s *Server) Fingerprint() string { return ssh.FingerprintSHA256(s.signer.PublicKey()) }

func validRoutes(routes []Route) bool {
	if len(routes) == 0 || len(routes) > 12 {
		return false
	}
	for _, r := range routes {
		switch r.Kind {
		case "ssh":
			if r.Host == "" || strings.ContainsAny(r.Host, "/\\\r\n\x00") || len(r.Host) > 255 || r.Port < 1 || r.Port > 65535 {
				return false
			}
		case "websocket":
			u, e := url.Parse(r.URL)
			if e != nil || u.Scheme != "wss" && !(u.Scheme == "ws" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) || u.Host == "" || u.User != nil || u.Fragment != "" || len(r.URL) > 2048 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (s *Server) Invite(routes []Route) (Invitation, error) {
	if !validRoutes(routes) {
		return Invitation{}, errors.New("provide a reachable SSH address or secure WebSocket URL")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, offer := range s.offers {
		if time.Now().After(offer.ExpiresAt) {
			delete(s.offers, id)
		}
	}
	if len(s.offers) >= 8 {
		return Invitation{}, errors.New("too many pending pairing invitations")
	}
	secret := randomID(32)
	id := randomID(18)
	expiry := time.Now().Add(5 * time.Minute).UTC()
	previous := s.state.Routes
	s.state.Routes = routes
	if err := s.persist(); err != nil {
		s.state.Routes = previous
		return Invitation{}, err
	}
	s.offers[id] = &Offer{ID: id, ExpiresAt: expiry, Status: "waiting", secret: sha256.Sum256([]byte(secret))}
	name, _ := os.Hostname()
	return Invitation{Version, s.state.ID, name, s.Fingerprint(), routes, id, secret, expiry}, nil
}
func (s *Server) offer(id string) (*Offer, error) {
	o := s.offers[id]
	if o == nil || time.Now().After(o.ExpiresAt) {
		return nil, errors.New("pairing expired; create a new QR code on the computer")
	}
	return o, nil
}
func (s *Server) authenticatePair(id string, secret []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, e := s.offer(id)
	if e != nil {
		return false
	}
	hash := sha256.Sum256(secret)
	// Approval consumes the invitation for new keys. The same request can recover
	// its acknowledgement until expiry, including after a transport disconnect.
	return o.Status != "rejected" && subtle.ConstantTimeCompare(hash[:], o.secret[:]) == 1
}

func validateRequest(req PairRequest) (string, error) {
	if len(req.ID) < 8 || len(req.ID) > 128 || strings.TrimSpace(req.Name) == "" || len(req.Name) > 100 || strings.ContainsAny(req.Name, "\r\n\x00") || len(req.PublicKey) > 2048 {
		return "", errors.New("invalid device pairing request")
	}
	key, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(req.PublicKey))
	if err != nil || len(rest) != 0 || key.Type() != ssh.KeyAlgoED25519 {
		return "", errors.New("an Ed25519 device public key is required")
	}
	return keyID(key), nil
}

func (s *Server) Request(offerID string, req PairRequest) (*Offer, error) {
	deviceID, err := validateRequest(req)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.offer(offerID)
	if err != nil {
		return nil, err
	}
	if o.Request != nil {
		if *o.Request != req {
			return nil, errors.New("this invitation already has a different device request")
		}
		copy := *o
		return &copy, nil
	}
	o.Request = &req
	o.deviceID = deviceID
	o.Status = "pending"
	copy := *o
	return &copy, nil
}

func (s *Server) Decide(id, requestID string, approve bool) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.offer(id)
	if err != nil {
		return Device{}, err
	}
	if o.Request == nil || o.Request.ID != requestID {
		return Device{}, errors.New("the requested device changed; review the request again")
	}
	if o.Status == "approved" && approve {
		return s.state.Devices[o.deviceID], nil
	}
	if o.Status != "pending" {
		return Device{}, errors.New("this pairing decision is already final")
	}
	if !approve {
		o.Status = "rejected"
		return Device{}, nil
	}
	if len(s.state.Devices) >= 64 {
		if _, exists := s.state.Devices[o.deviceID]; !exists {
			return Device{}, errors.New("paired device limit reached")
		}
	}
	device := Device{ID: o.deviceID, Name: o.Request.Name, PublicKey: o.Request.PublicKey, CreatedAt: time.Now().UTC()}
	previous, exists := s.state.Devices[device.ID]
	s.state.Devices[device.ID] = device
	if err = s.persist(); err != nil {
		if exists {
			s.state.Devices[device.ID] = previous
		} else {
			delete(s.state.Devices, device.ID)
		}
		return Device{}, err
	}
	o.Status = "approved"
	return device, nil
}

func (s *Server) Revoke(id string) error {
	s.mu.Lock()
	device, exists := s.state.Devices[id]
	if !exists {
		s.mu.Unlock()
		return os.ErrNotExist
	}
	previous := device
	device.Revoked = true
	s.state.Devices[id] = device
	if err := s.persist(); err != nil {
		s.state.Devices[id] = previous
		s.mu.Unlock()
		return err
	}
	connections := []*ssh.ServerConn{}
	for key, forward := range s.forwards {
		if forward.DeviceID == id {
			s.closeForwardLocked(key)
		}
	}
	for conn, owner := range s.connections {
		if owner == id {
			connections = append(connections, conn)
		}
	}
	s.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
	return nil
}

func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func reject(w http.ResponseWriter, err error) {
	send(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		reject(w, errors.New("invalid request"))
		return false
	}
	return true
}

func (s *Server) PairHandler(offerID string) http.Handler {
	mux := http.NewServeMux()
	writeStatus := func(w http.ResponseWriter, o *Offer) {
		result := map[string]any{"status": o.Status, "requestId": o.Request.ID, "expiresAt": o.ExpiresAt}
		if o.Status == "approved" {
			s.mu.Lock()
			device := s.state.Devices[o.deviceID]
			routes := append([]Route{}, s.state.Routes...)
			s.mu.Unlock()
			if device.Revoked {
				reject(w, errors.New("this device was revoked"))
				return
			}
			result["device"] = device
			result["computerId"] = s.state.ID
			result["registryId"] = s.registryID
			result["routes"] = routes
			result["fingerprint"] = s.Fingerprint()
			result["daemonToken"] = s.apiToken
			result["apiPort"] = APIPort
		}
		send(w, 200, result)
	}
	mux.HandleFunc("POST /pair", func(w http.ResponseWriter, r *http.Request) {
		var req PairRequest
		if !decode(w, r, &req) {
			return
		}
		o, err := s.Request(offerID, req)
		if err != nil {
			reject(w, err)
			return
		}
		writeStatus(w, o)
	})
	mux.HandleFunc("GET /pair/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		o, err := s.offer(offerID)
		var copy Offer
		if err == nil {
			copy = *o
		}
		s.mu.Unlock()
		if err != nil {
			reject(w, err)
			return
		}
		if copy.Request == nil || copy.Request.ID != r.PathValue("id") {
			reject(w, errors.New("pairing request not found"))
			return
		}
		writeStatus(w, &copy)
	})
	return mux
}

func (s *Server) Register(mux *http.ServeMux, guards ...func(http.HandlerFunc) http.HandlerFunc) {
	s.routes(func(pattern string, handler http.HandlerFunc) {
		if len(guards) > 0 && !strings.HasPrefix(pattern, "GET ") && pattern != "POST /computer/update" {
			handler = guards[0](handler)
		}
		mux.HandleFunc(pattern, handler)
	})
}

// Active invitations reserve the service until expiry, including a lost approval
// acknowledgement. An idle update must not invalidate a QR halfway through pairing.
func (s *Server) PendingActivity() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, offer := range s.offers {
		if offer.Status != "rejected" && !offer.Completed && time.Now().Before(offer.ExpiresAt) {
			return 1
		}
	}
	return len(s.forwards)
}

// ControlPatterns is generated from the actual registration for OpenAPI parity.
func ControlPatterns() []string {
	var patterns []string
	(&Server{}).routes(func(pattern string, _ http.HandlerFunc) { patterns = append(patterns, pattern) })
	return patterns
}

func (s *Server) routes(register func(string, http.HandlerFunc)) {
	s.updateRoutes(register)
	s.forwardRoutes(register)
	register("POST /computer/pairings/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RequestID string `json:"requestId"`
		}
		if !decode(w, r, &req) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		offer, err := s.offer(r.PathValue("id"))
		if err != nil {
			reject(w, err)
			return
		}
		if offer.Status != "approved" || offer.Request == nil || offer.Request.ID != req.RequestID {
			reject(w, errors.New("this device has not completed pairing"))
			return
		}
		offer.Completed = true
		send(w, 200, map[string]bool{"ok": true})
	})
	register("GET /computer", func(w http.ResponseWriter, r *http.Request) { send(w, 200, s.Info()) })
	register("PUT /computer/routes", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Routes []Route `json:"routes"`
		}
		if !decode(w, r, &req) {
			return
		}
		if !validRoutes(req.Routes) {
			reject(w, errors.New("invalid computer routes"))
			return
		}
		s.mu.Lock()
		previous := s.state.Routes
		s.state.Routes = req.Routes
		err := s.persist()
		if err != nil {
			s.state.Routes = previous
		}
		s.mu.Unlock()
		if err != nil {
			reject(w, err)
			return
		}
		send(w, 200, s.Info())
	})
	register("POST /computer/pairings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Routes []Route `json:"routes"`
		}
		if !decode(w, r, &req) {
			return
		}
		invitation, err := s.Invite(req.Routes)
		if err != nil {
			reject(w, err)
			return
		}
		send(w, 201, invitation)
	})
	register("GET /computer/pairings/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		o, err := s.offer(r.PathValue("id"))
		if err != nil {
			reject(w, err)
			return
		}
		send(w, 200, o)
	})
	register("POST /computer/pairings/{id}/decision", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RequestID string `json:"requestId"`
			Approve   bool   `json:"approve"`
		}
		if !decode(w, r, &req) {
			return
		}
		device, err := s.Decide(r.PathValue("id"), req.RequestID, req.Approve)
		if err != nil {
			reject(w, err)
			return
		}
		send(w, 200, map[string]any{"ok": true, "device": device})
	})
	register("GET /computer/devices", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		devices := []Device{}
		for _, device := range s.state.Devices {
			devices = append(devices, device)
		}
		send(w, 200, devices)
	})
	register("DELETE /computer/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Revoke(r.PathValue("id")); err != nil {
			reject(w, err)
			return
		}
		send(w, 200, map[string]bool{"ok": true})
	})
}

func (s *Server) Info() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := map[string]any{"version": Version, "portForwardingVersion": 1, "computerId": s.state.ID, "registryId": s.registryID, "fingerprint": s.Fingerprint(), "routes": append([]Route{}, s.state.Routes...), "pid": os.Getpid(), "apiAddress": s.apiAddress}
	if s.sshListener != nil {
		info["sshPort"] = s.sshListener.Addr().(*net.TCPAddr).Port
	}
	if s.wsListener != nil {
		info["websocketPort"] = s.wsListener.Addr().(*net.TCPAddr).Port
	}
	return info
}

func (s *Server) bootstrap(directory string) error {
	data, err := json.Marshal(s.Info())
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "computer-runtime.json"), append(data, '\n'), 0600)
}
