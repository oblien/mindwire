// Package artifact stores durable tool-output images beside the workspace registry.
package artifact

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

const MaxBytes = 8 << 20
const maxWorkspaceBytes = 512 << 20

var ErrStorageFull = errors.New("workspace capture storage is full; remove unneeded chats before capturing more images")
var ErrImageLimit = errors.New("capture exceeds the supported image size")

type Record struct {
	ID        string    `json:"id"`
	ChatID    string    `json:"chatId,omitempty"`
	Mime      string    `json:"mime"`
	Bytes     int       `json:"bytes"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	CreatedAt time.Time `json:"createdAt"`
}
type Content struct {
	Record
	Data []byte `json:"data"`
}
type Store struct {
	mu  sync.Mutex
	db  *registry.Store
	dir string
}

func New(db *registry.Store) (*Store, error) {
	dir := filepath.Join(db.Directory(), "artifacts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{db: db, dir: dir}, nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > MaxBytes {
		return 0, ErrImageLimit
	}
	return b.Buffer.Write(p)
}

func (s *Store) SaveImage(chatID string, img image.Image) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bounds := img.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 || bounds.Dx()*bounds.Dy() > 16<<20 {
		return Record{}, ErrImageLimit
	}
	var data limitedBuffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&data, img); err != nil {
		return Record{}, err
	}
	rows, err := s.db.SurfaceList("artifact")
	if err != nil {
		return Record{}, err
	}
	total := data.Len()
	for _, row := range rows {
		var rec Record
		if json.Unmarshal(row, &rec) == nil {
			// Unattached human captures expire. Chat artifacts persist until chat deletion.
			if rec.ChatID == "" && time.Since(rec.CreatedAt) > 24*time.Hour {
				_ = os.Remove(s.path(rec.ID))
				_ = s.db.SurfaceDelete("artifact", rec.ID)
			} else {
				total += rec.Bytes
			}
		}
	}
	if total > maxWorkspaceBytes {
		return Record{}, ErrStorageFull
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return Record{}, err
	}
	rec := Record{ID: hex.EncodeToString(key), ChatID: chatID, Mime: "image/png",
		Bytes: data.Len(), Width: bounds.Dx(), Height: bounds.Dy(), CreatedAt: time.Now().UTC()}
	file, err := os.OpenFile(s.path(rec.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Record{}, err
	}
	_, err = file.Write(data.Bytes())
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = s.db.SurfacePut("artifact", rec.ID, rec)
	}
	if err != nil {
		_ = os.Remove(s.path(rec.ID))
		return Record{}, err
	}
	return rec, nil
}

var validID = regexp.MustCompile("^[a-f0-9]{32}$")

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".png") }
func (s *Store) Get(id string) (Content, error) {
	if !validID.MatchString(id) {
		return Content{}, registry.ErrNotFound
	}
	var rec Record
	if err := s.db.SurfaceGet("artifact", id, &rec); err != nil {
		return Content{}, err
	}
	info, err := os.Lstat(s.path(id))
	if err != nil {
		return Content{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return Content{}, errors.New("invalid image artifact")
	}
	data, err := os.ReadFile(s.path(id))
	return Content{Record: rec, Data: data}, err
}
func (s *Store) DeleteChat(chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.SurfaceList("artifact")
	if err != nil {
		return err
	}
	for _, row := range rows {
		var rec Record
		if json.Unmarshal(row, &rec) == nil && rec.ChatID == chatID {
			if err := s.db.SurfaceDelete("artifact", rec.ID); err != nil {
				return err
			}
			_ = os.Remove(s.path(rec.ID))
		}
	}
	return nil
}

// Delete rolls back a capture whose metadata could not be committed.
func (s *Store) Delete(id string) error {
	if !validID.MatchString(id) {
		return registry.ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.db.SurfaceDelete("artifact", id)
}
