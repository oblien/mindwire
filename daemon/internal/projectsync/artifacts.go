package projectsync

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

func artifactID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && hex.EncodeToString(b) == id
}

func (s *Service) captureArtifacts(ctx context.Context, m *Manifest, chats map[string]string) error {
	rows, err := s.reg.SurfaceList("artifact")
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var row artifact.Record
		if err = json.Unmarshal(raw, &row); err != nil {
			return err
		}
		logical, ok := chats[row.ChatID]
		if !ok {
			continue
		}
		if !artifactID(row.ID) {
			return invalid("invalid selected chat artifact")
		}
		path := filepath.Join(s.reg.Directory(), "artifacts", row.ID+".png")
		e, err := s.capture(ctx, path, "", false)
		if err != nil {
			return err
		}
		if e == nil {
			return fmt.Errorf("a selected chat's image artifact is missing")
		}
		row.ChatID = logical
		if m.Artifacts == nil {
			m.Artifacts = map[string]artifact.Record{}
		}
		m.Artifacts[row.ID] = row
		m.Entries["artifacts/"+row.ID+".png"] = *e
	}
	return nil
}

func (s *Service) artifactMetadata(m Manifest, batch registry.Import) (map[string]json.RawMessage, error) {
	ids := map[string]string{}
	for _, chat := range batch.Chats {
		ids[chat.SyncID] = chat.ID
	}
	out := map[string]json.RawMessage{}
	for id, row := range m.Artifacts {
		row.ChatID = ids[row.ChatID]
		if row.ChatID == "" {
			return nil, invalid("artifact has no destination conversation")
		}
		var old artifact.Record
		err := s.reg.SurfaceGet("artifact", id, &old)
		if err == nil && !equal(old, row) {
			return nil, fmt.Errorf("a tool image belongs to a different local conversation")
		}
		if err != nil && !errors.Is(err, registry.ErrNotFound) {
			return nil, err
		}
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		out[id] = data
	}
	return out, nil
}

// Retain attachments materialized outside the project as inline history data.
// Missing references fail export instead of yielding an incomplete success.
func portableAttachments(data *session.PortableChat, cwd string) error {
	for i := range data.Messages {
		for j := range data.Messages[i].Attachments {
			a := &data.Messages[i].Attachments[j]
			if len(a.Data) > 0 || a.Path == "" {
				continue
			}
			path := a.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}
			if workspacepath.Contains(cwd, path) {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("restore this chat's external attachment before synchronizing: %s", filepath.Base(path))
			}
			if info.Size() > artifact.MaxBytes {
				return fmt.Errorf("move the large attachment %s into the project before synchronizing", filepath.Base(path))
			}
			a.Data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
			a.Path = ""
		}
	}
	return nil
}

func (s *Service) validateArtifacts(m Manifest) error {
	for id, row := range m.Artifacts {
		if !artifactID(id) || id != row.ID || row.Mime != "image/png" || row.Bytes < 0 || row.Bytes > artifact.MaxBytes || row.Width < 1 || row.Height < 1 || row.Width*row.Height > 16<<20 {
			return invalid("invalid chat image artifact")
		}
		if _, ok := m.Chats[row.ChatID]; !ok {
			return invalid("image artifact belongs to an unknown conversation")
		}
		e, ok := m.Entries["artifacts/"+id+".png"]
		if !ok || e.Kind != "file" || e.Size != int64(row.Bytes) {
			return invalid("image artifact content is missing")
		}
	}
	for key := range m.Entries {
		if strings.HasPrefix(key, "artifacts/") {
			id := strings.TrimSuffix(strings.TrimPrefix(key, "artifacts/"), ".png")
			if _, ok := m.Artifacts[id]; !ok {
				return invalid("unowned image artifact")
			}
		}
	}
	return nil
}
