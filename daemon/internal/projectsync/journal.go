package projectsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

func safeParent(path string) error {
	parent := filepath.Dir(path)
	actual, err := workspacepath.Canonical(parent)
	if err != nil {
		return err
	}
	if actual != parent {
		return fmt.Errorf("a destination directory was replaced by a symbolic link; no write was allowed")
	}
	return nil
}

func (s *Service) matches(ctx context.Context, w Write) (bool, error) {
	if w.Resource != nil {
		actual, err := s.readResource(ctx, *w.Resource, w.CWD)
		if err != nil {
			return false, err
		}
		if equal(actual, w.After) {
			return true, nil
		}
		if !equal(actual, w.Before) {
			return false, fmt.Errorf("native memory changed independently; recovery copies are retained")
		}
		return false, nil
	}
	if err := safeParent(w.Path); err != nil {
		return false, err
	}
	lines := w.After != nil && w.After.JSONLines || w.Before != nil && w.Before.JSONLines
	actual, err := s.capture(ctx, w.Path, w.CWD, lines)
	if err != nil {
		return false, err
	}
	// Native file modes are always tightened to private on installation.
	if actual != nil && w.Private {
		actual.Mode = 0600
	}
	if equal(actual, w.After) {
		return true, nil
	}
	if !equal(actual, w.Before) {
		return false, fmt.Errorf("%s changed outside the synchronization; recovery copies are retained", filepath.Base(w.Path))
	}
	return false, nil
}

func (s *Service) install(ctx context.Context, w Write) error {
	done, err := s.matches(ctx, w)
	if err != nil || done {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if w.Resource != nil {
		return s.installResource(ctx, w)
	}
	if w.After == nil {
		if err = os.Remove(w.Path); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(w.Path))
	} // rmdir only; never RemoveAll on user data
	e := *w.After
	if w.Before != nil && w.Before.Kind != e.Kind && (w.Before.Kind == "directory" || e.Kind == "directory") {
		return fmt.Errorf("folder/file type changes require resolution before synchronization")
	}
	if err = durableMkdirAll(filepath.Dir(w.Path), 0700); err != nil {
		return err
	}
	if err = safeParent(w.Path); err != nil {
		return err
	}
	if e.Kind == "directory" {
		if err = durableMkdirAll(w.Path, os.FileMode(e.Mode)); err != nil {
			return err
		}
		if err = os.Chmod(w.Path, os.FileMode(e.Mode)); err != nil {
			return err
		}
		return syncDirectory(w.Path)
	}
	f, err := os.CreateTemp(filepath.Dir(w.Path), ".mindwire-sync-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e.Kind == "symlink" {
		f.Close()
		os.Remove(tmp)
		link := e.Link
		if link == projectMarker || strings.HasPrefix(link, projectMarker+"/") {
			link = w.CWD + strings.TrimPrefix(link, projectMarker)
		}
		if err = os.Symlink(link, tmp); err != nil {
			return err
		}
	} else {
		if err = f.Chmod(os.FileMode(e.Mode)); err != nil {
			f.Close()
			return err
		}
		if e.JSONLines {
			// Stream normalized records from disk, expanding the destination cwd.
			raw, err := os.CreateTemp(s.root, "expand-")
			if err != nil {
				f.Close()
				return err
			}
			defer os.Remove(raw.Name())
			defer raw.Close()
			if err = s.copy(raw, e); err == nil {
				_, err = raw.Seek(0, 0)
			}
			if err == nil {
				err = remapJSON(raw, f, projectMarker, w.CWD)
			}
			if err != nil {
				f.Close()
				return err
			}
		} else if err = s.copy(f, e); err != nil {
			f.Close()
			return err
		}
		if err = f.Sync(); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
	}
	if err = os.Rename(tmp, w.Path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(w.Path))
}

func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func durableMkdirAll(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("destination parent is not a directory")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return err
	}
	if err = durableMkdirAll(parent, mode); err != nil {
		return err
	}
	if err = os.Mkdir(path, mode); err != nil && !os.IsExist(err) {
		return err
	}
	return syncDirectory(parent)
}

func (s *Service) applyJournal(ctx context.Context, o *Operation) error {
	var j journal
	if err := s.reg.SyncGet("journal", o.ID, &j); err != nil {
		return err
	}
	var committed bool
	if s.reg.SyncGet("committed", o.ID, &committed) == nil && committed {
		o.Status, o.Phase, o.Progress, o.ResultCheckpointID, o.ResultProjectID = "succeeded", "complete", 1, j.Result, j.Batch.Projects[0].ID
		_ = s.reg.ReleaseSync(o.ID)
		return s.save(*o)
	}
	m, err := s.manifest(j.Result)
	if err != nil {
		return err
	}
	if err = nativeIdle(ctx, j.Path); err != nil {
		return err
	}
	o.Status, o.Phase = "applying", "apply"
	if err = s.save(*o); err != nil {
		return err
	}
	// All expectations are checked before the first replacement, including
	// recovery. A concurrent external edit is never used as rollback fodder.
	for _, w := range j.Writes {
		if _, err = s.matches(ctx, w); err != nil {
			return err
		}
	}
	if err = durableMkdirAll(j.Path, 0755); err != nil {
		return err
	}
	for i, w := range j.Writes {
		if err = s.install(ctx, w); err != nil {
			return err
		}
		if s.afterWrite != nil {
			if err = s.afterWrite(i); err != nil {
				return err
			}
		}
		if i%20 == 0 {
			o.Progress = float64(i+1) / float64(max(len(j.Writes), 1)) * .8
			if err = s.save(*o); err != nil {
				return err
			}
		}
	}
	if !j.GitDone {
		if err = s.applyGit(ctx, j.Path, j.GitBefore, j.GitAfter, o.ID); err != nil {
			return err
		}
		j.GitDone = true
		if err = s.reg.SyncPut("journal", o.ID, j); err != nil {
			return err
		}
	}
	imports, err := s.chatImports(j, m)
	if err != nil {
		return err
	}
	if err = s.store.ImportChats(imports); err != nil {
		return err
	}
	forkPending := map[string]bool{}
	for _, chat := range imports {
		if chat.Data.ForkPending {
			forkPending[chat.ChatID] = true
		}
	}
	if err = s.reg.CompleteSync(j.Batch, m.ProjectID, j.Result, o.ID, j.Artifacts, forkPending); err != nil {
		return err
	}
	o.Status, o.Phase, o.Progress, o.ResultCheckpointID, o.ResultProjectID = "succeeded", "complete", 1, j.Result, j.Batch.Projects[0].ID
	if err = s.save(*o); err != nil {
		return err
	}
	return s.reg.ReleaseSync(o.ID)
}
