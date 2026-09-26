package projectsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const projectMarker = "__MINDWIRE_PROJECT_ROOT__"

// Normalize structural path fields only. Prompt text, tool arguments as opaque
// strings, unknown records, compaction payloads and image bytes are preserved.
func remapJSON(r io.Reader, w io.Writer, from, to string) error {
	d := json.NewDecoder(r)
	d.UseNumber()
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, value := range x {
				if p, ok := value.(string); ok && (k == "cwd" || k == "workdir" || k == "working_directory" || k == "path" || k == "file_path" || k == "filePath" || k == "image_path") {
					if p == from || strings.HasPrefix(p, from+"/") {
						x[k] = to + strings.TrimPrefix(p, from)
					}
				} else {
					visit(value)
				}
			}
		case []any:
			for _, value := range x {
				visit(value)
			}
		}
	}
	for {
		var value any
		err := d.Decode(&value)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("native conversation contains an incomplete or unsupported JSON record")
		}
		visit(value)
		if err = e.Encode(value); err != nil {
			return err
		}
	}
}

func (s *Service) capture(ctx context.Context, path, cwd string, jsonLines bool) (*Entry, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e := Entry{Mode: uint32(info.Mode().Perm())}
	switch {
	case info.IsDir():
		e.Kind = "directory"
	case info.Mode()&os.ModeSymlink != 0:
		e.Kind = "symlink"
		// Symlink permission bits differ by OS and are not portable. Compare the
		// link itself, so a macOS → Linux → macOS retry remains idempotent.
		e.Mode = 0777
		e.Link, err = os.Readlink(path)
		if err != nil {
			return nil, err
		}
		if cwd != "" && (e.Link == cwd || strings.HasPrefix(e.Link, cwd+"/")) {
			e.Link = projectMarker + strings.TrimPrefix(e.Link, cwd)
		}
	case info.Mode().IsRegular():
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		var blob Entry
		if jsonLines {
			// A temporary stream avoids holding multi-gigabyte transcripts in RAM.
			tmp, err := os.CreateTemp(s.root, "native-")
			if err != nil {
				return nil, err
			}
			defer os.Remove(tmp.Name())
			defer tmp.Close()
			if err = remapJSON(f, tmp, cwd, projectMarker); err != nil {
				return nil, err
			}
			if _, err = tmp.Seek(0, 0); err != nil {
				return nil, err
			}
			blob, err = s.blob(ctx, tmp)
			if err != nil {
				return nil, err
			}
		} else {
			blob, err = s.blob(ctx, f)
			if err != nil {
				return nil, err
			}
		}
		e.Kind, e.Size, e.Chunks, e.JSONLines = blob.Kind, blob.Size, blob.Chunks, jsonLines
		end, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if end.Size() != info.Size() || !end.ModTime().Equal(info.ModTime()) {
			return nil, fmt.Errorf("a file changed during synchronization; retry when work is saved")
		}
	default:
		return nil, fmt.Errorf("cannot synchronize socket, device or other special file: %s", filepath.Base(path))
	}
	return &e, nil
}

func (s *Service) scanFiles(ctx context.Context, root string) (map[string]Entry, error) {
	out := map[string]Entry{}
	if workspacepath.Contains(root, s.root) {
		return nil, invalid("the project includes Mindwire's private data directory; select the project folder itself")
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if path == root {
			if !d.IsDir() {
				return invalid("project path is not a directory")
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if d.Name() == ".git" {
			if name != ".git" {
				return invalid("nested Git repositories require their own project sync")
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) >= maxEntries {
			return invalid("project has too many files for this sync version")
		}
		e, err := s.capture(ctx, path, root, false)
		if err != nil {
			return err
		}
		if e == nil {
			return fmt.Errorf("a file disappeared during synchronization")
		}
		out["files/"+name] = *e
		return nil
	})
	return out, err
}

func (s *Service) validateManifest(m Manifest) error {
	if m.Version != Version || !hashOK(m.ProjectID) || m.Name == "" || len(m.Name) > 512 || len(m.Entries) > maxEntries || len(m.Parents) > 2 {
		return invalid("unsupported project checkpoint")
	}
	validateEntry := func(e Entry) error {
		if e.Mode&^uint32(0777) != 0 || e.Size < 0 || e.Size > 1<<40 {
			return invalid("invalid file mode or size")
		}
		switch e.Kind {
		case "file":
			if e.Link != "" {
				return invalid("invalid file entry")
			}
		case "directory":
			if e.Size != 0 || len(e.Chunks) != 0 || e.Link != "" {
				return invalid("invalid directory entry")
			}
		case "symlink":
			if e.Link == "" || strings.ContainsRune(e.Link, 0) || e.Size != 0 || len(e.Chunks) != 0 {
				return invalid("invalid symlink entry")
			}
		default:
			return invalid("unsupported file type")
		}
		var size int64
		for i, id := range e.Chunks {
			if !hashOK(id) {
				return invalid("invalid object hash")
			}
			info, err := os.Stat(s.objectPath(id))
			if err != nil {
				return err
			}
			if i < len(e.Chunks)-1 && info.Size() != ChunkSize {
				return invalid("noncanonical chunk size")
			}
			size += info.Size()
		}
		if size != e.Size {
			return invalid("file size does not match its objects")
		}
		return nil
	}
	caseNames := map[string]string{}
	owned := map[string]bool{}
	if err := s.validateArtifacts(m); err != nil {
		return err
	}
	for id := range m.Artifacts {
		owned["artifacts/"+id+".png"] = true
	}
	for id, c := range m.Chats {
		if !hashOK(id) || id != c.ID || c.ArchiveVersion != 1 || !agent.ValidNativeSessionID(c.Harness) || len(c.Title) > 16384 || len(c.NativeFiles) > maxEntries {
			return invalid("unsupported conversation archive")
		}
		porter := s.porters[c.Harness]
		if porter == nil {
			return invalid("this service cannot import " + c.Harness + " native conversations")
		}
		if c.SessionID != "" && !agent.ValidNativeSessionID(c.SessionID) {
			return invalid("invalid native session ID")
		}
		owned["records/"+id] = true
		if _, ok := m.Entries["records/"+id]; !ok {
			return invalid("conversation history is missing")
		}
		for _, name := range c.NativeFiles {
			prefix := "native/" + c.Harness + "/"
			dataPrefix := "data/" + c.Harness + "/"
			if strings.HasPrefix(name, dataPrefix) {
				dataPorter, ok := porter.(agent.SessionDataPorter)
				if !ok || !dataPorter.ValidSessionDataName(strings.TrimPrefix(name, dataPrefix)) {
					return invalid("unsupported native database artifact")
				}
			} else if !strings.HasPrefix(name, prefix) {
				return invalid("conversation archive has an unrelated file")
			} else if _, err := porter.ImportSessionPath("/mindwire-project", c.SessionID, strings.TrimPrefix(name, prefix)); err != nil {
				return invalid(err.Error())
			}
			if _, ok := m.Entries[name]; !ok {
				return invalid("native conversation file is missing")
			}
			owned[name] = true
		}
	}
	for name, e := range m.Entries {
		if !agent.SafeArchiveName(name) {
			return invalid("unsafe archive path")
		}
		for parent := filepath.ToSlash(filepath.Dir(name)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			if p, ok := m.Entries[parent]; ok && p.Kind != "directory" {
				return invalid("file path traverses a non-directory")
			}
		}
		if strings.HasPrefix(name, "files/") {
			for _, p := range strings.Split(name, "/")[1:] {
				if strings.EqualFold(p, ".git") {
					return invalid("Git internals cannot be installed as project files")
				}
			}
		} else if !owned[name] || e.Kind != "file" {
			return invalid("unowned native or history entry")
		}
		lower := cases.Fold().String(norm.NFD.String(name))
		if prior, ok := caseNames[lower]; ok && prior != name {
			return invalid("paths differ only in case or Unicode spelling; synchronization would be unsafe on this filesystem")
		}
		caseNames[lower] = name
		if err := validateEntry(e); err != nil {
			return err
		}
	}
	if m.Git != nil {
		if err := validateGit(m.Git); err != nil {
			return err
		}
		for _, e := range m.Git.Index {
			if err := validateEntry(e); err != nil {
				return err
			}
		}
		if m.Git.Bundle != nil {
			if err := validateEntry(*m.Git.Bundle); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) expanded(e Entry, cwd string) ([]byte, error) {
	b, err := s.read(e, 64<<20)
	if err != nil {
		return nil, err
	}
	if e.JSONLines {
		var out bytes.Buffer
		err = remapJSON(bytes.NewReader(b), &out, projectMarker, cwd)
		return out.Bytes(), err
	}
	return b, nil
}
