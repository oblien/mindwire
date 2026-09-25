package computer

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Update struct {
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	Status    string    `json:"status"`
	Force     bool      `json:"force,omitempty"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

func (s *Server) updatePath() string {
	return filepath.Join(filepath.Dir(s.path), "computer-update.json")
}

// Called under s.mu. The controller writes progress; the daemon alone owns
// manual intent in computer.json, so status writes cannot undo a promotion.
func (s *Server) update() (Update, error) {
	data, err := os.ReadFile(s.updatePath())
	if os.IsNotExist(err) {
		return Update{Status: "idle"}, nil
	}
	if err != nil {
		return Update{}, err
	}
	var update Update
	err = json.Unmarshal(data, &update)
	update.Force = update.ID != "" && update.ID == s.state.ManualUpdateID
	return update, err
}

func (u Update) active() bool {
	return u.Status == "queued" || u.Status == "downloading" || u.Status == "waiting" || u.Status == "restarting"
}

// Only this accepted, still-active request may bypass idle admission. The normal
// exclusive lease still prevents two controllers/installers replacing a service.
func (s *Server) ForceUpdatePending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	update, err := s.update()
	return err == nil && update.Force && update.active()
}

func (s *Server) setManualUpdate(id string) error {
	previous := s.state.ManualUpdateID
	s.state.ManualUpdateID = id
	if err := s.persist(); err != nil {
		s.state.ManualUpdateID = previous
		return err
	}
	return nil
}

func (s *Server) updateRoutes(register func(string, http.HandlerFunc)) {
	register("GET /computer/update", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		update, err := s.update()
		if err != nil {
			reject(w, err)
			return
		}
		send(w, 200, update)
	})
	register("POST /computer/update", func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("MINDWIRE_COMPUTER_MANAGED") != "1" {
			reject(w, errors.New("start the computer with mindwire connect to enable managed updates"))
			return
		}
		var req struct {
			Version string `json:"version"`
			Force   bool   `json:"force"`
		}
		if !decode(w, r, &req) {
			return
		}
		if !releaseVersion.MatchString(req.Version) {
			reject(w, errors.New("invalid release version"))
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		previous, err := s.update()
		if err != nil {
			reject(w, err)
			return
		}
		if previous.active() {
			if previous.Version != req.Version {
				send(w, 409, map[string]string{"error": "another service update is in progress"})
				return
			}
			if req.Force && !previous.Force {
				if err := s.setManualUpdate(previous.ID); err != nil {
					reject(w, err)
					return
				}
				previous.Force = true
			}
			send(w, 202, previous)
			return
		}
		update := Update{ID: randomID(18), Version: req.Version, Status: "queued", Force: req.Force, UpdatedAt: time.Now().UTC()}
		manualID := ""
		if req.Force {
			manualID = update.ID
		}
		if err := s.setManualUpdate(manualID); err != nil {
			reject(w, err)
			return
		}
		data, _ := json.Marshal(update)
		if err := os.WriteFile(s.updatePath()+".tmp", data, 0600); err != nil {
			reject(w, err)
			return
		}
		if err := os.Rename(s.updatePath()+".tmp", s.updatePath()); err != nil {
			reject(w, err)
			return
		}
		send(w, 202, update)
	})
}
