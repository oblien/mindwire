package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func fixture(t *testing.T) (*Service, *registry.Store, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := registry.Open(filepath.Join(dir, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); st.Close() })
	return s, st, dir
}
func git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}
func repository(t *testing.T, parent string, empty bool) string {
	t.Helper()
	path := filepath.Join(parent, "source")
	git(t, "init", path)
	if !empty {
		if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		git(t, "-C", path, "add", ".")
		git(t, "-C", path, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
	}
	return path
}
func terminal(t *testing.T, s *Service, id string) registry.ProjectOperation {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		o, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if !o.Active() {
			return o
		}
		time.Sleep(5 * time.Millisecond)
	}
	o, _ := s.Get(id)
	t.Fatalf("operation stuck: %+v", o)
	return o
}
func TestFolderCreateCloneAndEmptyRepository(t *testing.T) {
	for _, source := range []string{"folder", "create", "clone", "empty-clone"} {
		t.Run(source, func(t *testing.T) {
			s, st, dir := fixture(t)
			req := Request{ProjectSpec: registry.ProjectSpec{ID: "project", Source: source, Name: "App", Path: filepath.Join(dir, "app")}}
			if source == "folder" {
				if err := os.Mkdir(req.Path, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if strings.Contains(source, "clone") {
				req.Source = "clone"
				req.RepoURL = repository(t, dir, source == "empty-clone")
			}
			o, err := s.Start(req)
			if err != nil {
				t.Fatal(err)
			}
			o = terminal(t, s, o.ID)
			if o.Status != "succeeded" || o.ProjectID != "project" {
				t.Fatalf("%+v", o)
			}
			p, err := st.Project(o.ProjectID)
			if err != nil || p.Path != req.Path {
				t.Fatalf("project=%+v err=%v", p, err)
			}
			if markerMatches(req.Path, o) {
				t.Fatal("temporary ownership marker left in project")
			}
			if _, err := os.Stat(stagePath(o)); !os.IsNotExist(err) {
				t.Fatalf("staging remains: %v", err)
			}
			if source == "clone" && git(t, "-C", req.Path, "remote", "get-url", "origin") != req.RepoURL {
				t.Fatal("origin was rewritten")
			}
			before, _ := st.Snapshot(nil)
			again, err := s.Start(req)
			after, _ := st.Snapshot(nil)
			if err != nil || again.ID != o.ID || before.Revision != after.Revision {
				t.Fatalf("retry duplicated work: %+v %v", again, err)
			}
		})
	}
}

func TestConcurrentRequestsAndCancellation(t *testing.T) {
	s, st, dir := fixture(t)
	started := make(chan struct{})
	var calls atomic.Int32
	s.clone = func(ctx context.Context, o registry.ProjectOperation, a Auth, dst string, out func(string)) error {
		if calls.Add(1) == 1 {
			close(started)
		}
		_ = os.Mkdir(dst, 0755)
		out("Receiving objects: 42% (42/100)")
		<-ctx.Done()
		return ctx.Err()
	}
	req := Request{ProjectSpec: registry.ProjectSpec{ID: "clone", Source: "clone", Name: "App", Path: filepath.Join(dir, "app"), RepoURL: "https://example.invalid/repo.git"}}
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := req
			r.ID = fmt.Sprint("clone-", i)
			o, err := s.Start(r)
			ids <- o.ID
			errs <- err
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	<-started
	var id string
	for value := range ids {
		if id != "" && id != value {
			t.Fatal("clients got different active operations")
		}
		id = value
	}
	if calls.Load() != 1 {
		t.Fatalf("cloned %d times", calls.Load())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		o, _ := s.Get(id)
		if o.Progress != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	o, _ := s.Get(id)
	if o.Progress == nil || *o.Progress != .42 {
		t.Fatalf("progress missing: %+v", o)
	}
	if _, err := s.Cancel(id); err != nil {
		t.Fatal(err)
	}
	o = terminal(t, s, id)
	if o.Status != "cancelled" {
		t.Fatalf("%+v", o)
	}
	if _, err := st.Project(id); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("cancelled operation registered a project")
	}
	s.Close() // also waits for owned staging cleanup
	if _, err := os.Stat(stagePath(o)); !os.IsNotExist(err) {
		t.Fatal("cancelled staging left behind")
	}
}

func TestFailedCloneRetriesWithoutOverwritingExistingFiles(t *testing.T) {
	s, _, dir := fixture(t)
	repo := repository(t, dir, false)
	realClone := s.clone
	var attempts atomic.Int32
	s.clone = func(ctx context.Context, o registry.ProjectOperation, a Auth, dst string, out func(string)) error {
		if attempts.Add(1) == 1 {
			return errors.New("temporary clone failure")
		}
		return realClone(ctx, o, a, dst, out)
	}
	req := Request{ProjectSpec: registry.ProjectSpec{ID: "retry", Source: "clone", Name: "App", Path: filepath.Join(dir, "app"), RepoURL: repo}}
	o, err := s.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	if terminal(t, s, o.ID).Status != "failed" {
		t.Fatal("failure missing")
	}
	if _, err = s.Retry(o.ID, Auth{}); err != nil {
		t.Fatal(err)
	}
	o = terminal(t, s, o.ID)
	if o.Status != "succeeded" || o.Attempt != 2 || attempts.Load() != 2 {
		t.Fatalf("retry %+v", o)
	}
	// A different operation cannot replace a directory it did not create.
	other := filepath.Join(dir, "existing")
	_ = os.Mkdir(other, 0755)
	_ = os.WriteFile(filepath.Join(other, "keep"), []byte("user file"), 0600)
	req.ID = "existing"
	req.Path = other
	o, err = s.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	if terminal(t, s, o.ID).Status != "failed" {
		t.Fatal("existing destination accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(other, "keep")); string(b) != "user file" {
		t.Fatal("existing files changed")
	}
}

func TestRestartRecoversPublishedAndUnpublishedCommit(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(fmt.Sprint(published), func(t *testing.T) {
			s, st, dir := fixture(t)
			s.Close()
			spec := registry.ProjectSpec{ID: "recover", Source: "create", Name: "App", Path: filepath.Join(dir, "app")}
			o, _, err := st.ReserveProject(spec)
			if err != nil {
				t.Fatal(err)
			}
			stage, err := prepareStage(o)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(stage, "project")
			_ = os.Mkdir(path, 0755)
			if err = writeMarker(path, o); err != nil {
				t.Fatal(err)
			}
			_, err = st.ChangeProjectOperation(o.ID, func(v *registry.ProjectOperation) error { v.Status = "running"; v.Phase = "registering"; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if published {
				if err = publish(path, o); err != nil {
					t.Fatal(err)
				}
			}
			restarted, err := New(st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			o, err = restarted.Get(o.ID)
			if err != nil || o.Status != "succeeded" {
				t.Fatalf("recovery %+v %v", o, err)
			}
			if _, err = st.Project(o.ProjectID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRestartMarksUnfinishedCloneAndPreservesForeignStaging(t *testing.T) {
	s, st, dir := fixture(t)
	s.Close()
	o, _, err := st.ReserveProject(registry.ProjectSpec{ID: "interrupted", Source: "clone", Name: "App", Path: filepath.Join(dir, "app"), RepoURL: "https://example.invalid/repo.git"})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Mkdir(stagePath(o), 0755)
	_ = os.WriteFile(filepath.Join(stagePath(o), "foreign"), []byte("keep"), 0600)
	restarted, err := New(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	o, _ = restarted.Get(o.ID)
	if o.Status != "interrupted" {
		t.Fatalf("%+v", o)
	}
	if b, _ := os.ReadFile(filepath.Join(stagePath(o), "foreign")); string(b) != "keep" {
		t.Fatal("unowned staging deleted")
	}
}

func TestCredentialsAreWriteOnlyAndCanonicalFoldersDeduplicate(t *testing.T) {
	s, st, dir := fixture(t)
	s.clone = func(ctx context.Context, o registry.ProjectOperation, a Auth, dst string, out func(string)) error {
		out("remote: " + a.Token)
		return errors.New("failed with " + a.Token)
	}
	secret := "fixture-secret-never-store"
	o, err := s.Start(Request{ProjectSpec: registry.ProjectSpec{ID: "secret", Source: "clone", Name: "App", Path: filepath.Join(dir, "app"), RepoURL: "https://example.invalid/repo.git"}, Auth: Auth{Kind: "token", Token: secret}})
	if err != nil {
		t.Fatal(err)
	}
	o = terminal(t, s, o.ID)
	data, _ := json.Marshal(o)
	if strings.Contains(string(data), secret) {
		t.Fatal("credential exposed in persisted operation")
	}
	if _, err := s.Retry(o.ID, Auth{}); !errors.Is(err, registry.ErrInvalid) {
		t.Fatal("retry accepted missing credential")
	}
	folder := filepath.Join(dir, "folder")
	_ = os.Mkdir(folder, 0755)
	alias := filepath.Join(dir, "alias")
	_ = os.Symlink(folder, alias)
	req := Request{ProjectSpec: registry.ProjectSpec{ID: "folder", Source: "folder", Name: "Folder", Path: folder}}
	o, err = s.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	o = terminal(t, s, o.ID)
	req.ID = "alias"
	req.Path = alias
	second, err := s.Start(req)
	if err != nil || second.ProjectID != o.ProjectID {
		t.Fatalf("alias duplicated project %+v %v", second, err)
	}
	snap, _ := st.Snapshot(nil)
	if len(snap.Projects) != 1 {
		t.Fatalf("projects=%d", len(snap.Projects))
	}
}

func TestInvalidRequestsCannotStartAFileOperation(t *testing.T) {
	s, _, dir := fixture(t)
	for _, url := range []string{"ext::/tmp/unwanted-command", "https://user:secret@example.invalid/repo", "custom://repo", "--upload-pack=anything"} {
		_, err := s.Start(Request{ProjectSpec: registry.ProjectSpec{ID: "invalid", Source: "clone", Name: "App", Path: filepath.Join(dir, "app"), RepoURL: url}})
		if !errors.Is(err, registry.ErrInvalid) {
			t.Fatalf("accepted repository %q: %v", url, err)
		}
	}
	for _, id := range []string{".", "..", "bad\tid"} {
		_, err := s.Start(Request{ProjectSpec: registry.ProjectSpec{ID: id, Source: "create", Name: "App", Path: filepath.Join(dir, "app")}})
		if !errors.Is(err, registry.ErrInvalid) {
			t.Fatalf("accepted id %q: %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "app")); !os.IsNotExist(err) {
		t.Fatal("invalid request created files")
	}
}
