package computer

import (
	"errors"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const forwardLifetime = 2 * time.Minute

// Port forwards are explicit, short-lived grants to an approved device. They
// never bind a public listener or grant access to other machines on the network.
type ForwardRequest struct {
	ID       string `json:"id"`
	DeviceID string `json:"deviceId"`
	Port     int    `json:"port"`
}
type Forward struct {
	ForwardRequest
	ExpiresAt time.Time `json:"expiresAt"`
}
type portForward struct {
	Forward
	done  chan struct{}
	timer *time.Timer
}

func (s *Server) openForward(req ForwardRequest) (Forward, error) {
	if len(req.ID) < 8 || len(req.ID) > 128 || strings.IndexFunc(req.ID, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) >= 0 || req.Port < 1 || req.Port > 65535 {
		return Forward{}, errors.New("invalid forward ID or port")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	device, exists := s.state.Devices[req.DeviceID]
	if s.closed || !exists || device.Revoked {
		return Forward{}, errors.New("an approved device is required")
	}
	_, apiPort, _ := net.SplitHostPort(s.apiAddress)
	if req.Port == APIPort || req.Port == PairingPort || strconv.Itoa(req.Port) == apiPort ||
		(s.sshListener != nil && req.Port == s.sshListener.Addr().(*net.TCPAddr).Port) ||
		(s.wsListener != nil && req.Port == s.wsListener.Addr().(*net.TCPAddr).Port) {
		return Forward{}, errors.New("Mindwire control ports cannot be forwarded")
	}
	forward := s.forwards[req.ID]
	if forward != nil && forward.ForwardRequest != req {
		return Forward{}, errors.New("forward ID already belongs to another target")
	}
	if forward == nil {
		count := 0
		for _, item := range s.forwards {
			if item.DeviceID == req.DeviceID {
				count++
				if item.Port == req.Port {
					return Forward{}, errors.New("this port is already forwarded for this device")
				}
			}
		}
		if count >= 16 || len(s.forwards) >= 64 {
			return Forward{}, errors.New("close a port forward before opening another")
		}
		forward = &portForward{Forward: Forward{ForwardRequest: req}, done: make(chan struct{})}
		s.forwards[req.ID] = forward
	}
	forward.ExpiresAt = time.Now().UTC().Add(forwardLifetime)
	if forward.timer != nil {
		forward.timer.Stop()
	}
	forward.timer = time.AfterFunc(forwardLifetime, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if current := s.forwards[req.ID]; current == forward && !time.Now().Before(current.ExpiresAt) {
			s.closeForwardLocked(req.ID)
		}
	})
	return forward.Forward, nil
}

func (s *Server) closeForwardLocked(id string) {
	if forward := s.forwards[id]; forward != nil {
		delete(s.forwards, id)
		forward.timer.Stop()
		close(forward.done)
	}
}

func (s *Server) forwardedPort(deviceID string, port uint32) (<-chan struct{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, ok := s.state.Devices[deviceID]
	if s.closed || !ok || device.Revoked {
		return nil, false
	}
	for _, forward := range s.forwards {
		if forward.DeviceID == deviceID && uint32(forward.Port) == port && time.Now().Before(forward.ExpiresAt) {
			return forward.done, true
		}
	}
	return nil, false
}

func (s *Server) forwardRoutes(register func(string, http.HandlerFunc)) {
	register("GET /computer/forwards", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		values := []Forward{}
		for _, forward := range s.forwards {
			if time.Now().Before(forward.ExpiresAt) {
				values = append(values, forward.Forward)
			}
		}
		s.mu.Unlock()
		sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
		send(w, 200, values)
	})
	register("POST /computer/forwards", func(w http.ResponseWriter, r *http.Request) {
		var req ForwardRequest
		if !decode(w, r, &req) {
			return
		}
		forward, err := s.openForward(req)
		if err != nil {
			reject(w, err)
			return
		}
		send(w, 200, forward)
	})
	register("DELETE /computer/forwards/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.closeForwardLocked(r.PathValue("id"))
		s.mu.Unlock()
		send(w, 200, map[string]bool{"ok": true})
	})
}
