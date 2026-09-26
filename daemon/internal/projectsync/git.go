package projectsync

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func gitCommand(ctx context.Context, cwd, index string, input []byte, args ...string) ([]byte, error) {
	return gitInput(ctx, cwd, index, bytes.NewReader(input), args...)
}

func gitProcess(ctx context.Context, cwd, index string, input io.Reader, args ...string) *exec.Cmd {
	argv := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-C", cwd}
	argv = append(argv, args...)
	c := exec.CommandContext(ctx, "git", argv...)
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if !strings.HasPrefix(key, "GIT_") {
			c.Env = append(c.Env, v)
		}
	}
	c.Env = append(c.Env, "GIT_TERMINAL_PROMPT=0")
	if index != "" {
		c.Env = append(c.Env, "GIT_INDEX_FILE="+index)
	}
	c.Stdin = input
	return c
}

func gitInput(ctx context.Context, cwd, index string, input io.Reader, args ...string) ([]byte, error) {
	c := gitProcess(ctx, cwd, index, input, args...)
	var errOut bytes.Buffer
	c.Stderr = &errOut
	b, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("Git %s could not complete during project synchronization", args[0])
	}
	return b, nil
}

func (s *Service) gitBlob(ctx context.Context, cwd, id string) (Entry, error) {
	c := gitProcess(ctx, cwd, "", nil, "cat-file", "blob", id)
	r, err := c.StdoutPipe()
	if err != nil {
		return Entry{}, err
	}
	if err = c.Start(); err != nil {
		return Entry{}, err
	}
	e, readErr := s.blob(ctx, r)
	if readErr != nil {
		_ = c.Process.Kill()
	}
	err = c.Wait()
	if readErr != nil {
		return e, readErr
	}
	if err != nil {
		return e, fmt.Errorf("could not capture a staged Git blob")
	}
	return e, nil
}
func gitText(ctx context.Context, cwd string, args ...string) (string, error) {
	b, err := gitCommand(ctx, cwd, "", nil, args...)
	return strings.TrimSpace(string(b)), err
}
func oidOK(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 20 && hex.EncodeToString(b) == s
}
func refOK(s string) bool {
	return strings.HasPrefix(s, "refs/") && agent.SafeArchiveName(s) && !strings.ContainsAny(s, " \n\r\t~^:?*[\\") && !strings.Contains(s, "..") && !strings.Contains(s, "@{") && !strings.HasSuffix(s, ".lock") && !strings.HasSuffix(s, ".")
}
func validateGit(g *Git) error {
	if !refOK(g.Head) && !oidOK(g.Head) {
		return invalid("invalid Git HEAD")
	}
	for ref, id := range g.Refs {
		if !refOK(ref) || !oidOK(id) {
			return invalid("invalid Git reference")
		}
	}
	for path, e := range g.Index {
		if !agent.SafeArchiveName(path) || (e.Kind != "file" && e.Kind != "symlink") || e.JSONLines {
			return invalid("unsupported Git index entry")
		}
		for _, p := range strings.Split(path, "/") {
			if strings.EqualFold(p, ".git") {
				return invalid("invalid Git index path")
			}
		}
	}
	return nil
}

// Verify all reachable Git data in a disposable repository before any working
// file is replaced. Corrupt/incomplete bundles never leave a half-imported tree.
func (s *Service) preflightGit(ctx context.Context, g *Git) error {
	if g == nil {
		return nil
	}
	dir, err := os.MkdirTemp(s.root, "git-preflight-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if _, err = gitCommand(ctx, dir, "", nil, "init", "--bare", "--quiet"); err != nil {
		return err
	}
	if g.Bundle != nil {
		path := filepath.Join(dir, "incoming.bundle")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = s.copy(f, *g.Bundle)
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if _, err = gitCommand(ctx, dir, "", nil, "bundle", "verify", path); err != nil {
			return err
		}
		if _, err = gitCommand(ctx, dir, "", nil, "bundle", "unbundle", path); err != nil {
			return err
		}
	}
	var refs strings.Builder
	for ref, id := range g.Refs {
		refs.WriteString("update " + ref + " " + id + "\n")
	}
	if _, err = gitCommand(ctx, dir, "", []byte(refs.String()), "update-ref", "--stdin"); err != nil {
		return err
	}
	if refOK(g.Head) {
		_, err = gitCommand(ctx, dir, "", nil, "symbolic-ref", "HEAD", g.Head)
	} else {
		_, err = gitCommand(ctx, dir, "", nil, "update-ref", "--no-deref", "HEAD", g.Head)
	}
	if err != nil {
		return err
	}
	_, err = gitCommand(ctx, dir, "", nil, "fsck", "--connectivity-only", "--no-dangling")
	return err
}

func (s *Service) captureGit(ctx context.Context, cwd string, bundle bool) (*Git, error) {
	return s.captureGitLocked(ctx, cwd, bundle, "")
}
func (s *Service) captureGitLocked(ctx context.Context, cwd string, bundle bool, lockToken string) (*Git, error) {
	if _, err := os.Stat(cwd); os.IsNotExist(err) {
		return nil, nil
	}
	if _, err := os.Lstat(filepath.Join(cwd, ".git")); os.IsNotExist(err) {
		if top, err := gitText(ctx, cwd, "rev-parse", "--show-toplevel"); err == nil && top != "" {
			return nil, invalid("synchronize the repository root, not a subfolder")
		}
		return nil, nil
	}
	dir, err := gitText(ctx, cwd, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	common, err := gitText(ctx, cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if dir != common {
		return nil, invalid("linked Git worktrees require a separate standalone copy before synchronization")
	}
	format, err := gitText(ctx, cwd, "rev-parse", "--show-object-format")
	if err != nil {
		return nil, err
	}
	if format != "sha1" {
		return nil, invalid("this Git object format needs a newer project synchronization adapter")
	}
	for _, name := range []string{"MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "index.lock", "shallow"} {
		if _, err = os.Stat(filepath.Join(dir, name)); err == nil {
			if name == "index.lock" && lockToken != "" {
				b, e := os.ReadFile(filepath.Join(dir, name))
				if e == nil && string(b) == lockToken {
					continue
				}
			}
			return nil, invalid("finish the Git merge, rebase or incomplete operation before synchronizing")
		}
	}
	// A submodule, alternates or LFS object store has external data that a normal
	// bundle does not contain. Refuse visibly rather than silently omit its history.
	for _, name := range []string{"objects/info/alternates", "info/grafts", "modules", "lfs/objects"} {
		if _, err = os.Stat(filepath.Join(dir, name)); err == nil {
			return nil, invalid("this repository uses submodules, LFS or an external object store that this sync version cannot export")
		}
	}
	g := &Git{Refs: map[string]string{}, Index: map[string]Entry{}}
	g.Head, err = gitText(ctx, cwd, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		g.Head, err = gitText(ctx, cwd, "rev-parse", "--verify", "HEAD")
		if err != nil {
			return nil, err
		}
	}
	refs, err := gitCommand(ctx, cwd, "", nil, "for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(refs)), "\n") {
		if line == "" {
			continue
		}
		name, id, ok := strings.Cut(line, " ")
		if !ok {
			return nil, invalid("unreadable Git refs")
		}
		g.Refs[name] = id
		if strings.HasPrefix(name, "refs/replace/") {
			return nil, invalid("Git replacement history requires a dedicated synchronization adapter")
		}
	}
	if g.Refs["refs/stash"] != "" {
		return nil, invalid("apply or commit saved Git stashes before synchronizing; stash reflog migration is not supported yet")
	}
	flags, err := gitCommand(ctx, cwd, "", nil, "ls-files", "-v", "-z")
	if err != nil {
		return nil, err
	}
	for _, row := range bytes.Split(flags, []byte{0}) {
		if len(row) > 0 && row[0] != 'H' {
			return nil, invalid("resolve sparse, assume-unchanged or unmerged index entries before synchronizing")
		}
	}
	visible, err := gitCommand(ctx, cwd, "", nil, "diff", "--cached", "--no-ext-diff", "--no-renames", "--name-only", "-z", "--ita-visible-in-index")
	if err != nil {
		return nil, err
	}
	invisible, err := gitCommand(ctx, cwd, "", nil, "diff", "--cached", "--no-ext-diff", "--no-renames", "--name-only", "-z", "--ita-invisible-in-index")
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(visible, invisible) {
		return nil, invalid("stage or unstage intent-to-add entries before synchronizing")
	}
	entries, err := gitCommand(ctx, cwd, "", nil, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	for _, line := range bytes.Split(entries, []byte{0}) {
		if len(line) == 0 {
			continue
		}
		header, path, ok := strings.Cut(string(line), "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || fields[2] != "0" {
			return nil, invalid("resolve unmerged Git index entries before synchronizing")
		}
		if fields[0] == "160000" {
			return nil, invalid("submodule history cannot be synchronized by this version")
		}
		blob, err := s.gitBlob(ctx, cwd, fields[1])
		if err != nil {
			return nil, err
		}
		if fields[0] == "120000" {
			data, err := s.read(blob, 64<<10)
			if err != nil {
				return nil, err
			}
			g.Index[path] = Entry{Kind: "symlink", Mode: 0777, Link: string(data)}
		} else {
			e := blob
			e.Mode = 0644
			if fields[0] == "100755" {
				e.Mode = 0755
			}
			g.Index[path] = e
		}
	}
	if bundle {
		_, headErr := gitText(ctx, cwd, "rev-parse", "--verify", "HEAD")
		if len(g.Refs) > 0 || headErr == nil {
			f, err := os.CreateTemp(s.root, "git-*.bundle")
			if err != nil {
				return nil, err
			}
			path := f.Name()
			f.Close()
			os.Remove(path)
			defer os.Remove(path)
			args := []string{"bundle", "create", path, "--all"}
			if headErr == nil {
				args = append(args, "HEAD")
			}
			if _, err = gitCommand(ctx, cwd, "", nil, args...); err != nil {
				return nil, err
			}
			f, err = os.Open(path)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			e, err := s.blob(ctx, f)
			if err != nil {
				return nil, err
			}
			g.Bundle = &e
		}
	}
	return g, validateGit(g)
}
func gitEqual(a, b *Git) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aa, bb := *a, *b
	aa.Bundle, bb.Bundle = nil, nil
	return equal(aa, bb)
}

func (s *Service) applyGit(ctx context.Context, cwd string, before, after *Git, operationID string) error {
	if after == nil {
		return nil
	} // synchronization never removes an existing repository
	if _, err := os.Lstat(filepath.Join(cwd, ".git")); os.IsNotExist(err) {
		if before != nil {
			return fmt.Errorf("Git repository disappeared during sync")
		}
		if _, err = gitCommand(ctx, cwd, "", nil, "init", "--quiet"); err != nil {
			return err
		}
	}
	dir, err := gitText(ctx, cwd, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	indexLock := filepath.Join(dir, "index.lock")
	lockToken := "mindwire-sync:" + operationID + "\n"
	lock, err := os.OpenFile(indexLock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		b, e := os.ReadFile(indexLock)
		if e != nil || string(b) != lockToken {
			return fmt.Errorf("Git index is in use")
		}
	} else {
		_, err = lock.WriteString(lockToken)
		if err == nil {
			err = lock.Sync()
		}
		lock.Close()
		if err != nil {
			os.Remove(indexLock)
			return err
		}
	}
	defer os.Remove(indexLock)
	current, err := s.captureGitLocked(ctx, cwd, false, lockToken)
	if err != nil {
		return err
	}
	if gitEqual(current, after) {
		return nil
	}
	if before == nil {
		before = &Git{Refs: map[string]string{}, Index: map[string]Entry{}, Head: current.Head}
	}
	if current.Head != before.Head && current.Head != after.Head {
		return fmt.Errorf("Git HEAD changed during sync")
	}
	if !equal(current.Index, before.Index) && !equal(current.Index, after.Index) {
		return fmt.Errorf("Git staging changed during sync")
	}
	for ref, id := range current.Refs {
		if id != before.Refs[ref] && id != after.Refs[ref] {
			return fmt.Errorf("Git reference changed during sync: %s", ref)
		}
	}
	if after.Bundle != nil {
		f, err := os.CreateTemp(s.root, "import-*.bundle")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if err = s.copy(f, *after.Bundle); err != nil {
			f.Close()
			return err
		}
		f.Close()
		if _, err = gitCommand(ctx, cwd, "", nil, "bundle", "verify", f.Name()); err != nil {
			return err
		}
		if _, err = gitCommand(ctx, cwd, "", nil, "bundle", "unbundle", f.Name()); err != nil {
			return err
		}
	}
	// Build an independent index. No checkout, reset, clean, hooks or filters run.
	tmp, err := os.CreateTemp(dir, "mindwire-index-")
	if err != nil {
		return err
	}
	index := tmp.Name()
	tmp.Close()
	os.Remove(index)
	defer os.Remove(index)
	if _, err = gitCommand(ctx, cwd, index, nil, "read-tree", "--empty"); err != nil {
		return err
	}
	paths := make([]string, 0, len(after.Index))
	for path := range after.Index {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var indexInput bytes.Buffer
	for _, path := range paths {
		e := after.Index[path]
		mode := "100644"
		var data io.Reader
		if e.Kind == "symlink" {
			mode = "120000"
			data = strings.NewReader(e.Link)
		} else {
			data = &objectReader{service: s, entry: e}
			if e.Mode&0111 != 0 {
				mode = "100755"
			}
		}
		id, err := gitInput(ctx, cwd, "", data, "hash-object", "-w", "--stdin", "--no-filters")
		if err != nil {
			return err
		}
		indexInput.WriteString(mode + " " + strings.TrimSpace(string(id)) + "\t" + path)
		indexInput.WriteByte(0)
	}
	if _, err = gitCommand(ctx, cwd, index, indexInput.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return err
	}
	refs := map[string]bool{}
	for ref := range before.Refs {
		refs[ref] = true
	}
	for ref := range after.Refs {
		refs[ref] = true
	}
	var updates strings.Builder
	updates.WriteString("start\n")
	for ref := range refs {
		old, next := current.Refs[ref], after.Refs[ref]
		if old == next {
			continue
		}
		if old != before.Refs[ref] {
			return fmt.Errorf("Git reference changed during sync: %s", ref)
		}
		if next == "" {
			updates.WriteString("delete " + ref + " " + old + "\n")
		} else {
			if old == "" {
				old = strings.Repeat("0", len(next))
			}
			updates.WriteString("update " + ref + " " + next + " " + old + "\n")
		}
	}
	updates.WriteString("prepare\ncommit\n")
	if _, err = gitCommand(ctx, cwd, "", []byte(updates.String()), "update-ref", "--stdin"); err != nil {
		return err
	}
	if refOK(after.Head) {
		_, err = gitCommand(ctx, cwd, "", nil, "symbolic-ref", "HEAD", after.Head)
	} else {
		_, err = gitCommand(ctx, cwd, "", nil, "update-ref", "--no-deref", "HEAD", after.Head)
	}
	if err != nil {
		return err
	}
	// index.lock has held off ordinary Git writes throughout the publication.
	f, err := os.Open(index)
	if err != nil {
		return err
	}
	err = f.Sync()
	f.Close()
	if err != nil {
		return err
	}
	if err = os.Rename(index, filepath.Join(dir, "index")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Merge checkpoints must contain the union of reachable objects, not just the
// incoming bundle. Otherwise a later no-change export could omit local commits.
func (s *Service) unionBundle(ctx context.Context, result *Git, inputs ...*Git) (*Entry, error) {
	dir, err := os.MkdirTemp(s.root, "bundle-union-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if _, err = gitCommand(ctx, dir, "", nil, "init", "--bare", "--quiet"); err != nil {
		return nil, err
	}
	for i, g := range inputs {
		if g == nil || g.Bundle == nil {
			continue
		}
		path := filepath.Join(dir, fmt.Sprintf("source-%d.bundle", i))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		err = s.copy(f, *g.Bundle)
		f.Close()
		if err != nil {
			return nil, err
		}
		if _, err = gitCommand(ctx, dir, "", nil, "bundle", "unbundle", path); err != nil {
			return nil, err
		}
	}
	for ref, id := range result.Refs {
		if _, err = gitCommand(ctx, dir, "", nil, "update-ref", ref, id); err != nil {
			return nil, err
		}
	}
	if refOK(result.Head) {
		_, err = gitCommand(ctx, dir, "", nil, "symbolic-ref", "HEAD", result.Head)
	} else {
		_, err = gitCommand(ctx, dir, "", nil, "update-ref", "--no-deref", "HEAD", result.Head)
	}
	if err != nil {
		return nil, err
	}
	_, headErr := gitText(ctx, dir, "rev-parse", "--verify", "HEAD")
	if len(result.Refs) == 0 && headErr != nil {
		return nil, nil
	}
	path := filepath.Join(dir, "complete.bundle")
	args := []string{"bundle", "create", path, "--all"}
	if headErr == nil {
		args = append(args, "HEAD")
	}
	if _, err = gitCommand(ctx, dir, "", nil, args...); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	e, err := s.blob(ctx, f)
	return &e, err
}
