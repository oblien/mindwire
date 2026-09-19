package gitaccess

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type savedCredential struct {
	Connection Connection `json:"connection"`
	Auth       Auth       `json:"auth"`
}

type diskState struct {
	Default     *Connection                `json:"default,omitempty"`
	Credentials map[string]savedCredential `json:"credentials"`
}

// State is safe for API responses and caches. It never contains credentials.
type State struct {
	Default             *Connection  `json:"default,omitempty"`
	StoredConnectionIDs []string     `json:"storedConnectionIds"`
	StoredConnections   []Connection `json:"storedConnections"`
	ActiveOperations    int          `json:"activeOperations"`
}

type Service struct {
	mu       sync.Mutex
	path     string
	state    diskState
	server   *http.Server
	endpoint string
	cleanup  func()
	leases   map[string]credentialLease
	closed   bool
}

func New(directory string) (*Service, error) {
	path, err := filepath.Abs(filepath.Join(directory, "git-access.json"))
	if err != nil {
		return nil, err
	}
	s := &Service{path: path, leases: map[string]credentialLease{}}
	s.state, err = readState(path)
	if err != nil {
		return nil, err
	}
	listener, endpoint, cleanup, err := listenBroker()
	if err != nil {
		return nil, err
	}
	s.endpoint, s.cleanup = endpoint, cleanup
	s.server = &http.Server{Handler: http.HandlerFunc(s.credential), ReadHeaderTimeout: helperTimeout, ReadTimeout: helperTimeout, WriteTimeout: helperTimeout}
	go func() { _ = s.server.Serve(listener) }()
	return s, nil
}

func readState(path string) (diskState, error) {
	state := diskState{Credentials: map[string]savedCredential{}}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return state, fmt.Errorf("Git credential storage must be a private regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Mode().Perm()&0077 != 0 {
		return state, fmt.Errorf("Git credential storage changed while opening it")
	}
	data, err := io.ReadAll(io.LimitReader(file, 10<<20))
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("could not read Git credential storage")
	}
	if state.Credentials == nil {
		state.Credentials = map[string]savedCredential{}
	}
	return state, nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.leases = map[string]credentialLease{}
	s.mu.Unlock()
	_ = s.server.Close()
	s.cleanup()
}

func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := State{StoredConnectionIDs: []string{}, StoredConnections: []Connection{}}
	if s.state.Default != nil {
		value := *s.state.Default
		result.Default = &value
	}
	for id := range s.state.Credentials {
		result.StoredConnectionIDs = append(result.StoredConnectionIDs, id)
	}
	sort.Strings(result.StoredConnectionIDs)
	for _, id := range result.StoredConnectionIDs {
		result.StoredConnections = append(result.StoredConnections, s.state.Credentials[id].Connection)
	}
	return result
}

// Save only writes a credential when the selected lifetime explicitly allows it.
// An absent auth preserves an existing saved credential (for a second device).
func (s *Service) Save(c Connection, auth *Auth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := c.Validate(); err != nil {
		return err
	}
	if auth != nil && *auth != (Auth{}) {
		if err := auth.Validate(c); err != nil {
			return err
		}
	}
	if c.Lifetime != "workspace" || c.Mode == "native" || c.Mode == "ghCli" {
		return nil
	}
	if auth == nil {
		value, ok := s.state.Credentials[c.ID]
		if !ok || value.Connection.Mode != c.Mode {
			return fmt.Errorf("%w: supply this connection's credential before saving it on the workspace", ErrCredential)
		}
		return value.Auth.Validate(c)
	}
	if err := auth.Validate(c); err != nil {
		return err
	}
	previous, existed := s.state.Credentials[c.ID]
	s.state.Credentials[c.ID] = savedCredential{Connection: c, Auth: *auth}
	if err := s.persist(); err != nil {
		if existed {
			s.state.Credentials[c.ID] = previous
		} else {
			delete(s.state.Credentials, c.ID)
		}
		return err
	}
	return nil
}

func (s *Service) SetDefault(c *Connection, auth *Auth) error {
	if c != nil {
		if err := s.Save(*c, auth); err != nil {
			return err
		}
	}
	return s.RestoreDefault(c)
}

// RestoreDefault restores a previously validated reference after configuration
// fails. A revoked credential must not prevent rolling metadata back.
func (s *Service) RestoreDefault(c *Connection) error {
	if c != nil {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state.Default
	s.state.Default = nil
	if c != nil {
		value := *c
		s.state.Default = &value
	}
	if err := s.persist(); err != nil {
		s.state.Default = previous
		return err
	}
	return nil
}

// Forget removes the saved secret and invalidates its live helper leases. Project
// references remain visible so another account is never silently substituted.
func (s *Service) Forget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.state.Credentials[id]
	delete(s.state.Credentials, id)
	if err := s.persist(); err != nil {
		if existed {
			s.state.Credentials[id] = previous
		}
		return err
	}
	for key, lease := range s.leases {
		if lease.connection.ID == id {
			delete(s.leases, key)
		}
	}
	return nil
}

func (s *Service) Resolve(c *Connection, supplied *Auth) (Auth, error) {
	if c == nil || c.Mode == "native" {
		if supplied != nil && *supplied != (Auth{}) {
			return Auth{}, fmt.Errorf("%w: select a connection before forwarding credentials", ErrCredential)
		}
		return Auth{}, nil
	}
	if err := c.Validate(); err != nil {
		return Auth{}, err
	}
	auth := Auth{}
	if c.Mode == "ghCli" {
		auth.Kind = "gh"
	}
	if supplied != nil {
		auth = *supplied
	} else if c.Lifetime == "workspace" {
		s.mu.Lock()
		value, ok := s.state.Credentials[c.ID]
		s.mu.Unlock()
		if ok && value.Connection.Mode == c.Mode {
			auth = value.Auth
		}
	}
	if err := auth.Validate(*c); err != nil {
		return Auth{}, err
	}
	if c.Mode == "oblienApp" {
		auth.ReadOnly = true
	}
	return auth, nil
}

// EffectiveConnection applies a workspace GitHub default only to GitHub
// repositories. Other hosts retain their server authentication. Explicit project
// bindings remain visible and will report a mismatched remote instead of falling
// back to another account.
func (s *Service) EffectiveConnection(explicit *Connection, repo string) *Connection {
	if explicit != nil {
		return explicit
	}
	c := s.State().Default
	if c != nil && c.Mode != "native" && repo != "" {
		if _, err := Repository(repo); err != nil {
			return nil
		}
	}
	return c
}

func (s *Service) persist() error {
	if s.closed {
		return fmt.Errorf("Git credential service is closed")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".git-access-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.path)
}

func randomID() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
