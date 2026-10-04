package projectsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

type PreviewRequest struct {
	ProjectID      string   `json:"projectId"`
	IncludeIgnored []string `json:"includeIgnored,omitempty"`
	Cursor         int      `json:"cursor,omitempty"`
}
type IgnoredPath struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Included bool   `json:"included"`
}
type Preview struct {
	Version        int           `json:"version"`
	FileCount      int           `json:"fileCount"`
	Bytes          int64         `json:"bytes"` // working files only; Git history/chats are additional
	IgnoredCount   int           `json:"ignoredCount"`
	Ignored        []IgnoredPath `json:"ignored"`
	IncludeIgnored []string      `json:"includeIgnored"`
	Next           int           `json:"next,omitempty"`
	SelectionToken string        `json:"selectionToken"`
}

// Literal, portable relative paths. These are not shell arguments or glob rules.
func NormalizeIncludes(paths []string) ([]string, error) {
	if len(paths) > 1024 {
		return nil, invalid("select at most 1024 ignored paths")
	}
	set := pathMask{}
	for _, path := range paths {
		path = strings.TrimSuffix(path, "/")
		if !agent.SafeArchiveName(path) || len(path) > 4096 {
			return nil, invalid("select a project-relative file or folder")
		}
		for _, part := range strings.Split(path, "/") {
			if strings.EqualFold(part, ".git") {
				return nil, invalid("Git internals cannot be selected as project files")
			}
		}
		set[path] = true
	}
	return set.roots(), nil
}

type pathMask map[string]bool

func mask(paths []string) pathMask {
	out := pathMask{}
	for _, p := range paths {
		out[p] = true
	}
	return out
}
func (m pathMask) covers(path string) bool {
	for path != "." && path != "" {
		if m[path] {
			return true
		}
		path = filepath.ToSlash(filepath.Dir(path))
	}
	return false
}
func (m pathMask) roots() []string {
	var out []string
	for path := range m {
		if !m.covers(filepath.ToSlash(filepath.Dir(path))) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}
func (m pathMask) ancestors() pathMask {
	out := pathMask{}
	for path := range m {
		for p := filepath.ToSlash(filepath.Dir(path)); p != "."; p = filepath.ToSlash(filepath.Dir(p)) {
			out[p] = true
		}
	}
	return out
}

type fileSelection struct {
	root       string
	gitArgs    []string
	cleanup    func()
	include    pathMask
	exact      pathMask
	parents    pathMask
	ignored    pathMask
	omitted    pathMask
	candidates []IgnoredPath
}

// Git resolves its own ignore syntax, nested rules, negations, local/global
// excludes and tracked exceptions. --directory prunes ignored dependency trees.
// Plain folders use a disposable Git directory; the source is never initialized.
func (s *Service) selection(ctx context.Context, root string, includes, exact []string) (*fileSelection, error) {
	includes, err := NormalizeIncludes(includes)
	if err != nil {
		return nil, err
	}
	f := &fileSelection{root: root, cleanup: func() {}, include: mask(includes), exact: mask(exact), ignored: pathMask{}}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		f.parents = f.include.ancestors()
		return f, nil
	}
	if workspacepath.Contains(root, s.root) {
		return nil, invalid("the project includes Mindwire's private data directory; select the project folder itself")
	}
	if _, err := os.Lstat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		if top, e := gitText(ctx, root, "rev-parse", "--show-toplevel"); e == nil && top != "" {
			return nil, invalid("synchronize the repository root, not a subfolder")
		}
		dir, e := os.MkdirTemp(s.root, "ignore-rules-")
		if e != nil {
			return nil, e
		}
		f.cleanup = func() { _ = os.RemoveAll(dir) }
		if _, e = gitCommand(ctx, root, "", nil, "init", "--bare", "--quiet", "--template=", dir); e != nil {
			f.cleanup()
			return nil, e
		}
		f.gitArgs = []string{"--git-dir=" + dir, "--work-tree=" + root, "-c", "core.bare=false"}
	}
	tracked, err := f.git(ctx, nil, "ls-files", "--cached", "-z")
	if err != nil {
		f.cleanup()
		return nil, err
	}
	for _, path := range nulPaths(tracked) {
		f.exact[path] = true
	}
	f.parents = f.exact.ancestors()
	for path := range f.include.ancestors() {
		f.parents[path] = true
	}
	ignored, err := f.git(ctx, nil, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		f.cleanup()
		return nil, err
	}
	for _, path := range nulPaths(ignored) {
		kind := "file"
		if strings.HasSuffix(path, "/") {
			kind = "directory"
		}
		path = strings.TrimSuffix(path, "/")
		f.ignored[path] = true
		f.candidates = append(f.candidates, IgnoredPath{path, kind, f.include.covers(path)})
	}
	sort.Slice(f.candidates, func(i, j int) bool { return f.candidates[i].Path < f.candidates[j].Path })
	return f, nil
}
func nulPaths(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
}
func (f *fileSelection) git(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	c := gitProcess(ctx, f.root, "", bytes.NewReader(input), append(append([]string{}, f.gitArgs...), args...)...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	output, err := c.Output()
	// Git can exit successfully after warning that an ignore file was unreadable.
	// Never treat that as permission to include the files it should have excluded.
	if err != nil || stderr.Len() > 0 {
		return nil, invalid("Git could not read the project's file selection rules")
	}
	return output, nil
}
func (f *fileSelection) selected(path string, directory bool) bool {
	return f.include.covers(path) || f.exact[path] || (directory && f.parents[path])
}
func (f *fileSelection) skip(path string, directory bool) bool {
	return f.omitted.covers(path) || (f.ignored.covers(path) && !f.selected(path, directory))
}

func (f *fileSelection) token() string {
	data, _ := json.Marshal(struct {
		Root              string
		Included, Tracked []string
		Ignored           []IgnoredPath
	}{f.root, f.include.roots(), f.exact.roots(), f.candidates})
	return digest(data)
}

// Include missing historical paths in the ignore decision too. Otherwise an
// omitted .env/node_modules entry could be misread as a deletion on the return.
func (f *fileSelection) excludedKnown(ctx context.Context, m Manifest) ([]string, error) {
	paths := pathMask{}
	for key, e := range m.Entries {
		if strings.HasPrefix(key, "files/") {
			path := strings.TrimPrefix(key, "files/")
			if !f.selected(path, e.Kind == "directory") {
				if e.Kind == "directory" {
					path += "/"
				}
				paths[path] = true
			}
		}
	}
	for _, path := range m.Excluded {
		if !f.selected(path, true) {
			paths[path] = true
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	if _, err := os.Stat(f.root); os.IsNotExist(err) {
		return nil, nil
	}
	var input bytes.Buffer
	for path := range paths {
		input.WriteString(path)
		input.WriteByte(0)
	}
	c := gitProcess(ctx, f.root, "", &input, append(append([]string{}, f.gitArgs...), "check-ignore", "-z", "--stdin")...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	output, err := c.Output()
	if stderr.Len() > 0 {
		return nil, invalid("Git could not read the project's historical file selection rules")
	}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return nil, invalid("could not read Git ignore rules")
		}
	}
	out := pathMask{}
	for _, path := range nulPaths(output) {
		out[strings.TrimSuffix(path, "/")] = true
	}
	// A directory match must not hide a tracked or negated child. Git's
	// check-ignore can match the directory spelling even when such a child is
	// included; ls-files and the current tree walk correctly retain that child.
	keepParents := pathMask{}
	for key := range m.Entries {
		if !strings.HasPrefix(key, "files/") {
			continue
		}
		path := strings.TrimPrefix(key, "files/")
		if !out[path] {
			keepParents[path] = true
		}
	}
	for parent := range keepParents.ancestors() {
		delete(out, parent)
	}
	return out.roots(), nil
}

func (s *Service) Preview(ctx context.Context, req PreviewRequest) (Preview, error) {
	p, err := s.reg.Project(req.ProjectID)
	if err != nil {
		return Preview{}, err
	}
	root, err := workspacepath.Canonical(p.Path)
	if err != nil {
		return Preview{}, err
	}
	f, err := s.selection(ctx, root, req.IncludeIgnored, nil)
	if err != nil {
		return Preview{}, err
	}
	defer f.cleanup()
	out := Preview{Version: Version, IncludeIgnored: f.include.roots(), IgnoredCount: len(f.candidates), Ignored: []IgnoredPath{}}
	out.SelectionToken = f.token()
	if out.IncludeIgnored == nil {
		out.IncludeIgnored = []string{}
	}
	if req.Cursor < 0 || req.Cursor > len(f.candidates) {
		return out, invalid("invalid ignored-files cursor")
	}
	end := min(req.Cursor+200, len(f.candidates))
	out.Ignored = append(out.Ignored, f.candidates[req.Cursor:end]...)
	if end < len(f.candidates) {
		out.Next = end
	}
	_, err = s.walkSelected(ctx, root, f, func(path, _ string, d fs.DirEntry) error {
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		out.FileCount++
		if info.Mode().IsRegular() {
			out.Bytes += info.Size()
		}
		return nil
	})
	return out, err
}

func (s *Service) walkSelected(ctx context.Context, root string, f *fileSelection, visit func(string, string, fs.DirEntry) error) ([]string, error) {
	excluded := pathMask{}
	entries := 0
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
		if f.skip(name, d.IsDir()) {
			excluded[name] = true
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entries++
		if entries > maxEntries {
			return invalid("project has too many selected files for this sync version")
		}
		return visit(path, name, d)
	})
	return excluded.roots(), err
}

func knownFiles(m Manifest) []string {
	out := []string{}
	for key := range m.Entries {
		if strings.HasPrefix(key, "files/") {
			out = append(out, strings.TrimPrefix(key, "files/"))
		}
	}
	return out
}

// Persisted operations retain their exact selection, including after a restart.
func validateSelectionRequest(req *Request) error {
	if req.SelectionToken != "" && !hashOK(req.SelectionToken) {
		return invalid("invalid file selection token")
	}
	paths, err := NormalizeIncludes(req.IncludeIgnored)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		paths = nil
	}
	req.IncludeIgnored = paths
	return nil
}
