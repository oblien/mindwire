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
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

func (s *Server) updatePath() string {
	return filepath.Join(filepath.Dir(s.path), "computer-update.json")
}
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
	return update, err
}

func (s *Server) updateRoutes(register func(string, http.HandlerFunc)) {
	register("GET /computer/update", func(w http.ResponseWriter, r *http.Request) {
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
		if previous.Status == "queued" || previous.Status == "downloading" || previous.Status == "waiting" || previous.Status == "restarting" {
			if previous.Version != req.Version {
				send(w, 409, map[string]string{"error": "another service update is in progress"})
				return
			}
			send(w, 202, previous)
			return
		}
		update := Update{ID: randomID(18), Version: req.Version, Status: "queued", UpdatedAt: time.Now().UTC()}
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
