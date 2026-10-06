package artifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

const MaxAttachmentBytes = 16 << 20
const MaxUploadChunk = 384 << 10
const UploadVersion = 1

var ErrUpload = errors.New("invalid attachment upload")
var ErrUploadConflict = errors.New("attachment upload differs from its saved content")

// UploadChunk is replayable after a lost response. Bytes never enter the registry
// or a turn request; only the completed, verified reference does.
type UploadChunk struct {
	Name   string `json:"name"`
	Mime   string `json:"mime"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Offset int    `json:"offset"`
	Data   []byte `json:"data"`
}

type UploadState struct {
	Record
	Offset   int  `json:"offset"`
	Complete bool `json:"complete"`
}

// FileName also describes the portable artifact entry. Old captures retain their
// original .png layout. A safe extension lets native tools recognize uploaded files.
func (r Record) FileName() string {
	if r.Kind != "attachment" {
		return r.ID + ".png"
	}
	ext := strings.ToLower(filepath.Ext(r.Name))
	if len(ext) > 16 {
		ext = ""
	}
	for _, c := range strings.TrimPrefix(ext, ".") {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			ext = ""
			break
		}
	}
	return r.ID + ext
}

func (s *Store) recordPath(r Record) string { return filepath.Join(s.dir, r.FileName()) }

func (s *Store) Reference(chatID, id string) (Record, string, error) {
	if !validID.MatchString(id) {
		return Record{}, "", ErrUpload
	}
	var rec Record
	if err := s.db.SurfaceGet("artifact", id, &rec); err != nil {
		return rec, "", err
	}
	if rec.Kind != "attachment" || rec.ChatID != chatID {
		return rec, "", ErrUploadConflict
	}
	path := s.recordPath(rec)
	info, err := os.Lstat(path)
	if err != nil {
		return rec, "", err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(rec.Bytes) {
		return rec, "", ErrUpload
	}
	return rec, path, nil
}

func (s *Store) ResolveInputs(chatID string, attachments []agent.Attachment) ([]agent.Attachment, error) {
	if len(attachments) > 8 {
		return nil, ErrUpload
	}
	result := append([]agent.Attachment(nil), attachments...)
	for i, attachment := range result {
		if attachment.ArtifactID == "" {
			continue
		}
		if len(attachment.Data) > 0 {
			return nil, ErrUpload
		}
		rec, path, err := s.Reference(chatID, attachment.ArtifactID)
		if err != nil {
			return nil, err
		}
		result[i] = agent.Attachment{ArtifactID: rec.ID, Name: rec.Name, Path: path, Mime: rec.Mime, Bytes: rec.Bytes}
	}
	return result, nil
}

func (s *Store) Upload(chatID, id string, req UploadChunk) (UploadState, error) {
	if !validID.MatchString(id) || strings.TrimSpace(chatID) == "" || len(chatID) > 256 ||
		req.Name == "" || len(req.Name) > 240 || strings.ContainsAny(req.Name, "/\\\x00\r\n") ||
		req.Bytes < 0 || req.Bytes > MaxAttachmentBytes || req.Offset < 0 ||
		len(req.Data) > MaxUploadChunk || req.Offset > req.Bytes-len(req.Data) {
		return UploadState{}, ErrUpload
	}
	digest, err := hex.DecodeString(req.SHA256)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != req.SHA256 {
		return UploadState{}, ErrUpload
	}
	media, _, err := mime.ParseMediaType(req.Mime)
	if err != nil || media != req.Mime || len(req.Mime) > 128 {
		return UploadState{}, ErrUpload
	}
	want := Record{ID: id, ChatID: chatID, Kind: "attachment", Name: req.Name,
		Mime: req.Mime, Bytes: req.Bytes, SHA256: req.SHA256}
	s.mu.Lock()
	defer s.mu.Unlock()
	var rec Record
	err = s.db.SurfaceGet("artifact", id, &rec)
	if err == nil {
		if !sameUpload(rec, want) {
			return UploadState{}, ErrUploadConflict
		}
		if _, _, err = s.Reference(chatID, id); err != nil {
			return UploadState{}, err
		}
		return UploadState{Record: rec, Offset: rec.Bytes, Complete: true}, nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return UploadState{}, err
	}
	metaPath, partPath := filepath.Join(s.dir, id+".upload.json"), filepath.Join(s.dir, id+".part")
	data, err := os.ReadFile(metaPath)
	if errors.Is(err, os.ErrNotExist) {
		if req.Offset != 0 {
			return UploadState{}, fmt.Errorf("%w: restart this upload from its first chunk", ErrUploadConflict)
		}
		if err = s.reserveUpload(want.Bytes); err != nil {
			return UploadState{}, err
		}
		want.CreatedAt = time.Now().UTC()
		data, _ = json.Marshal(want)
		file, openErr := os.CreateTemp(s.dir, id+".upload-*")
		if openErr != nil {
			return UploadState{}, openErr
		}
		defer os.Remove(file.Name())
		_, err = file.Write(data)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(file.Name(), metaPath)
		}
		if err != nil {
			return UploadState{}, err
		}
	} else if err != nil {
		return UploadState{}, err
	}
	if json.Unmarshal(data, &rec) != nil || !sameUpload(rec, want) {
		return UploadState{}, ErrUploadConflict
	}
	// The service may stop after the final rename but before the registry commit.
	// Recover that completed file instead of asking the caller to start over.
	if info, statErr := os.Lstat(s.recordPath(rec)); statErr == nil {
		if !info.Mode().IsRegular() || info.Size() != int64(rec.Bytes) {
			return UploadState{}, ErrUploadConflict
		}
		completed, readErr := os.ReadFile(s.recordPath(rec))
		if readErr != nil {
			return UploadState{}, readErr
		}
		hash := sha256.Sum256(completed)
		if hex.EncodeToString(hash[:]) != rec.SHA256 {
			return UploadState{}, ErrUploadConflict
		}
		if err = s.db.SurfacePut("artifact", id, rec); err != nil {
			return UploadState{}, err
		}
		_ = os.Remove(metaPath)
		return UploadState{Record: rec, Offset: rec.Bytes, Complete: true}, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return UploadState{}, statErr
	}
	file, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return UploadState{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return UploadState{}, err
	}
	offset := int(info.Size())
	if req.Offset > offset || offset > rec.Bytes {
		return UploadState{}, ErrUploadConflict
	}
	// A response can be lost after the append. Verify an overlapping retry before
	// accepting it; never append the same chunk twice or silently replace bytes.
	overlap := min(len(req.Data), offset-req.Offset)
	if overlap > 0 {
		prior := make([]byte, overlap)
		if _, err = file.ReadAt(prior, int64(req.Offset)); err != nil || !bytes.Equal(prior, req.Data[:overlap]) {
			return UploadState{}, ErrUploadConflict
		}
	}
	if len(req.Data) > overlap {
		if _, err = file.WriteAt(req.Data[overlap:], int64(req.Offset+overlap)); err != nil {
			return UploadState{}, err
		}
		if err = file.Sync(); err != nil {
			return UploadState{}, err
		}
		offset = req.Offset + len(req.Data)
	}
	state := UploadState{Record: rec, Offset: offset}
	if offset != rec.Bytes {
		return state, nil
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return UploadState{}, err
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return UploadState{}, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != rec.SHA256 {
		_ = file.Close()
		_ = os.Remove(partPath)
		_ = os.Remove(metaPath)
		return UploadState{}, fmt.Errorf("%w: checksum failed; attach the file again", ErrUploadConflict)
	}
	if err = file.Close(); err != nil {
		return UploadState{}, err
	}
	if err = os.Rename(partPath, s.recordPath(rec)); err != nil {
		return UploadState{}, err
	}
	if err = s.db.SurfacePut("artifact", id, rec); err != nil {
		_ = os.Rename(s.recordPath(rec), partPath)
		return UploadState{}, err
	}
	_ = os.Remove(metaPath)
	state.Complete = true
	return state, nil
}

func sameUpload(a, b Record) bool {
	return a.ID == b.ID && a.ChatID == b.ChatID && a.Kind == b.Kind && a.Name == b.Name &&
		a.Mime == b.Mime && a.Bytes == b.Bytes && a.SHA256 == b.SHA256
}

// Reserve against completed files and unfinished uploads. Cleanup runs only on
// new uploads, with no background polling or work while the daemon is idle.
func (s *Store) reserveUpload(size int) error {
	rows, err := s.db.SurfaceList("artifact")
	if err != nil {
		return err
	}
	total := size
	for _, raw := range rows {
		var rec Record
		if json.Unmarshal(raw, &rec) == nil {
			if rec.ChatID == "" && time.Since(rec.CreatedAt) > 24*time.Hour {
				_ = os.Remove(s.recordPath(rec))
				_ = s.db.SurfaceDelete("artifact", rec.ID)
			} else {
				total += rec.Bytes
			}
		}
	}
	files, err := filepath.Glob(filepath.Join(s.dir, "*.upload.json"))
	if err != nil {
		return err
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var rec Record
		if json.Unmarshal(data, &rec) != nil || !validID.MatchString(rec.ID) {
			continue
		}
		if time.Since(rec.CreatedAt) > 24*time.Hour {
			_ = os.Remove(filepath.Join(s.dir, rec.ID+".part"))
			_ = os.Remove(path)
		} else {
			total += rec.Bytes
		}
	}
	if total > maxWorkspaceBytes {
		return ErrStorageFull
	}
	return nil
}
