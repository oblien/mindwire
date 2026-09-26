package projectsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func hashOK(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func (s *Service) objectPath(id string) string { return filepath.Join(s.root, "objects", id[:2], id) }

// atomicFile synchronizes the file and its parent before reporting a checkpoint
// or journal durable. Every temporary file is on the destination filesystem.
func atomicFile(path string, b []byte, mode os.FileMode) error {
	if err := durableMkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mindwire-sync-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// objectReader keeps large staged blobs bounded to one verified chunk in memory.
type objectReader struct {
	service *Service
	entry   Entry
	index   int
	read    int64
	current *bytes.Reader
}

func (r *objectReader) Read(p []byte) (int, error) {
	for {
		if r.current != nil && r.current.Len() > 0 {
			n, err := r.current.Read(p)
			r.read += int64(n)
			return n, err
		}
		if r.index == len(r.entry.Chunks) {
			if r.read != r.entry.Size {
				return 0, invalid("artifact length does not match its manifest")
			}
			return 0, io.EOF
		}
		data, err := r.service.GetObject(r.entry.Chunks[r.index])
		if err != nil {
			return 0, err
		}
		if int64(len(data)) > r.entry.Size-r.read {
			return 0, invalid("artifact exceeds its declared length")
		}
		r.index++
		r.current = bytes.NewReader(data)
	}
}

func (s *Service) PutObject(id string, b []byte) error {
	if !hashOK(id) || len(b) > ChunkSize || digest(b) != id {
		return invalid("invalid synchronization object checksum or size")
	}
	path := s.objectPath(id)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	return atomicFile(path, b, 0600)
}
func (s *Service) GetObject(id string) ([]byte, error) {
	if !hashOK(id) {
		return nil, invalid("invalid object ID")
	}
	b, err := os.ReadFile(s.objectPath(id))
	if os.IsNotExist(err) {
		return nil, registry.ErrNotFound
	}
	if err == nil && (len(b) > ChunkSize || digest(b) != id) {
		return nil, fmt.Errorf("synchronization object is corrupt")
	}
	return b, err
}
func (s *Service) Missing(ids []string) ([]string, error) {
	if len(ids) > 1024 {
		return nil, invalid("at most 1024 object IDs per request")
	}
	out := []string{}
	for _, id := range ids {
		if !hashOK(id) {
			return nil, invalid("invalid object ID")
		}
		if _, err := s.GetObject(id); err != nil {
			out = append(out, id)
		}
	}
	return out, nil
}
func (s *Service) blob(ctx context.Context, r io.Reader) (Entry, error) {
	e := Entry{Kind: "file", Mode: 0600}
	buf := make([]byte, ChunkSize)
	for {
		if err := ctx.Err(); err != nil {
			return e, err
		}
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			b := buf[:n]
			id := digest(b)
			if e2 := s.PutObject(id, b); e2 != nil {
				return e, e2
			}
			e.Chunks = append(e.Chunks, id)
			e.Size += int64(n)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return e, nil
		}
		if err != nil {
			return e, err
		}
	}
}
func (s *Service) read(e Entry, max int64) ([]byte, error) {
	if e.Size < 0 || e.Size > max {
		return nil, invalid("synchronization artifact exceeds its size limit")
	}
	var out bytes.Buffer
	if err := s.copy(&out, e); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func (s *Service) copy(w io.Writer, e Entry) error {
	var n int64
	for _, id := range e.Chunks {
		b, err := s.GetObject(id)
		if err != nil {
			return err
		}
		if int64(len(b)) > e.Size-n {
			return invalid("artifact exceeds its declared length")
		}
		count, err := w.Write(b)
		if err != nil {
			return err
		}
		n += int64(count)
	}
	if n != e.Size {
		return invalid("artifact length does not match its manifest")
	}
	return nil
}

func (s *Service) seal(ctx context.Context, m Manifest) (Checkpoint, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return Checkpoint{}, err
	}
	if len(b) > maxManifest {
		return Checkpoint{}, invalid("project manifest is too large")
	}
	e, err := s.blob(ctx, bytes.NewReader(b))
	if err != nil {
		return Checkpoint{}, err
	}
	c := Checkpoint{ID: digest(b), ProjectID: m.ProjectID, Parents: m.Parents, Manifest: e}
	ids := manifestObjects(m, e)
	c.ObjectCount = len(ids)
	for _, id := range ids {
		info, err := os.Stat(s.objectPath(id))
		if err != nil {
			return c, err
		}
		c.Bytes += info.Size()
	}
	return c, s.reg.SyncPut("checkpoint", c.ID, c)
}
func (s *Service) Checkpoint(id string) (Checkpoint, error) {
	if !hashOK(id) {
		return Checkpoint{}, invalid("invalid checkpoint ID")
	}
	var c Checkpoint
	err := s.reg.SyncGet("checkpoint", id, &c)
	return c, err
}
func (s *Service) manifest(id string) (Manifest, error) {
	c, err := s.Checkpoint(id)
	if err != nil {
		return Manifest{}, err
	}
	b, err := s.read(c.Manifest, maxManifest)
	if err != nil {
		return Manifest{}, err
	}
	if digest(b) != id {
		return Manifest{}, invalid("checkpoint checksum mismatch")
	}
	var m Manifest
	err = json.Unmarshal(b, &m)
	return m, err
}
func (s *Service) AcceptCheckpoint(ctx context.Context, c Checkpoint) error {
	if !hashOK(c.ID) || !hashOK(c.ProjectID) {
		return invalid("invalid checkpoint identity")
	}
	b, err := s.read(c.Manifest, maxManifest)
	if err != nil {
		return err
	}
	if digest(b) != c.ID {
		return invalid("checkpoint checksum mismatch")
	}
	var m Manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return invalid("invalid checkpoint manifest")
	}
	if m.Version != Version || m.ProjectID != c.ProjectID || !equal(m.Parents, c.Parents) {
		return invalid("unsupported or inconsistent checkpoint")
	}
	if err = s.validateManifest(m); err != nil {
		return err
	}
	for _, id := range m.Parents {
		if id == c.ID {
			return invalid("cyclic checkpoint")
		}
		p, err := s.Checkpoint(id)
		if err != nil {
			return fmt.Errorf("missing parent checkpoint: %w", err)
		}
		if p.ProjectID != m.ProjectID {
			return invalid("checkpoint parent belongs to another project")
		}
	}
	ids := manifestObjects(m, c.Manifest)
	c.ObjectCount = len(ids)
	c.Bytes = 0
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return err
		}
		b, err := s.GetObject(id)
		if err != nil {
			return err
		}
		c.Bytes += int64(len(b))
	}
	return s.reg.SyncPut("checkpoint", c.ID, c)
}
func manifestObjects(m Manifest, root Entry) []string {
	set := map[string]bool{}
	add := func(e Entry) {
		for _, id := range e.Chunks {
			set[id] = true
		}
	}
	add(root)
	for _, e := range m.Entries {
		add(e)
	}
	if m.Git != nil {
		for _, e := range m.Git.Index {
			add(e)
		}
		if m.Git.Bundle != nil {
			add(*m.Git.Bundle)
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func (s *Service) Objects(id string, cursor int) (ObjectPage, error) {
	c, err := s.Checkpoint(id)
	if err != nil {
		return ObjectPage{}, err
	}
	m, err := s.manifest(id)
	if err != nil {
		return ObjectPage{}, err
	}
	ids := manifestObjects(m, c.Manifest)
	if cursor < 0 || cursor > len(ids) {
		return ObjectPage{}, invalid("invalid object cursor")
	}
	end := min(cursor+512, len(ids))
	p := ObjectPage{Objects: ids[cursor:end]}
	if end < len(ids) {
		p.Next = end
	}
	return p, nil
}
