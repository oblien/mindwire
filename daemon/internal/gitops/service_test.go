package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/gitaccess"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

func fixture(t *testing.T) (*Service, *registry.Store, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	store, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	access, err := gitaccess.New(store.Directory())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	s, err := New(store, access)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	git(t, root, "config", "user.name", "Fixture User")
	git(t, root, "config", "user.email", "fixture@example.invalid")
	return s, store, root
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	data, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}
func put(t *testing.T, root, path, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func contents(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func wait(t *testing.T, s *Service, id string) registry.GitOperation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o, err := s.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func run(t *testing.T, s *Service, root, id, action string, paths []string, message string) registry.GitOperation {
	t.Helper()
	_, err := s.Start(registry.GitSpec{ID: id, ProjectID: "project", Path: root, Action: action, Paths: paths, Message: message}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return wait(t, s, id)
}

func TestLiteralPathsAndUnbornUnstagePreserveFiles(t *testing.T) {
	s, _, root := fixture(t)
	put(t, root, "a[1].txt", "one")
	put(t, root, "a1.txt", "neighbor")
	o := run(t, s, root, "stage", "stage", []string{"a[1].txt"}, "")
	if o.Status != "succeeded" || git(t, root, "ls-files") != "a[1].txt" {
		t.Fatalf("literal stage: %+v", o)
	}
	put(t, root, "a[1].txt", "changed after staging")
	o = run(t, s, root, "unstage", "unstage", []string{"a[1].txt"}, "")
	if o.Status != "succeeded" || git(t, root, "ls-files") != "" || contents(t, root, "a[1].txt") != "changed after staging" {
		t.Fatalf("unborn unstage lost working copy: %+v", o)
	}
	run(t, s, root, "stage2", "stage", []string{"a[1].txt"}, "")
	o = run(t, s, root, "commit", "commit", nil, "User's first commit")
	if o.Status != "succeeded" || git(t, root, "log", "-1", "--format=%an <%ae>") != "Fixture User <fixture@example.invalid>" {
		t.Fatalf("commit changed the configured identity: %+v", o)
	}
}

func TestDiscardFailureNeverDeletesTrackedOrOtherSelectedFiles(t *testing.T) {
	s, _, root := fixture(t)
	put(t, root, "tracked", "original")
	put(t, root, ".gitignore", "ignored\n")
	git(t, root, "add", "tracked", ".gitignore")
	git(t, root, "commit", "-qm", "baseline")
	put(t, root, "tracked", "work in progress")
	put(t, root, "untracked", "keep on restore failure")
	put(t, root, "ignored", "private ignored contents")
	put(t, root, ".git/index.lock", "another git process")
	o := run(t, s, root, "blocked-discard", "discard", []string{"tracked", "untracked"}, "")
	if o.Status != "failed" || contents(t, root, "tracked") != "work in progress" || contents(t, root, "untracked") != "keep on restore failure" {
		t.Fatalf("failed restore deleted work: %+v", o)
	}
	if err := os.Remove(filepath.Join(root, ".git/index.lock")); err != nil {
		t.Fatal(err)
	}
	o = run(t, s, root, "discard", "discard", []string{"tracked", "untracked", "ignored"}, "")
	if o.Status != "succeeded" || contents(t, root, "tracked") != "original" || contents(t, root, "ignored") != "private ignored contents" {
		t.Fatalf("discard did not preserve ignored files: %+v", o)
	}
	if _, err := os.Stat(filepath.Join(root, "untracked")); !os.IsNotExist(err) {
		t.Fatal("selected untracked file was not removed")
	}
}

func TestGitRejectsPathsOutsideWorkingTree(t *testing.T) {
	s, _, root := fixture(t)
	for _, path := range []string{"../outside", ".git/config", "a/../../outside", "/tmp/outside", ".", "a/../b", "a/.git/config"} {
		_, err := s.Start(registry.GitSpec{ID: "unsafe", ProjectID: "project", Path: root, Action: "discard", Paths: []string{path}}, nil, nil)
		if !errors.Is(err, registry.ErrInvalid) {
			t.Fatalf("accepted unsafe path %q: %v", path, err)
		}
	}
}

func TestDurableJobSurvivesDisconnectDeduplicatesAndCoordinates(t *testing.T) {
	s, store, root := fixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.run = func(ctx context.Context, spec registry.GitSpec, _ *gitaccess.Connection, _ *gitaccess.Auth) (gitaccess.Result, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return gitaccess.Result{}, ctx.Err()
		case <-release:
			return gitaccess.Result{Output: "finished"}, nil
		}
	}
	spec := registry.GitSpec{ID: "stable-request", ProjectID: "project", Path: root, Action: "commit", Message: "once"}
	if _, err := s.Start(spec, nil, nil); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Wait(ctx, spec.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("observer did not disconnect")
	}
	if _, err := s.Start(spec, nil, nil); err != nil || calls.Load() != 1 {
		t.Fatalf("lost acknowledgement repeated the write: %v", err)
	}
	changed := spec
	changed.Message = "different intent"
	if _, err := s.Start(changed, nil, nil); !errors.Is(err, registry.ErrConflict) {
		t.Fatal("ID reused for a different write")
	}
	changed = spec
	changed.ID = "another-client"
	if _, err := s.Start(changed, nil, nil); !errors.Is(err, registry.ErrConflict) {
		t.Fatal("overlapping write admitted")
	}
	if !s.BusyPath(filepath.Join(root, "subdir")) || !s.BusyPath(filepath.Dir(root)) || s.ActiveCount() != 1 {
		t.Fatal("active Git job does not reserve the repository")
	}
	if err := store.CheckProjectPath(root); !errors.Is(err, registry.ErrConflict) {
		t.Fatal("agent/removal admission bypassed Git reservation")
	}
	close(release)
	if got := wait(t, s, spec.ID); got.Status != "succeeded" || got.Output != "finished" {
		t.Fatalf("detached operation lost result: %+v", got)
	}
	s.Close()
	restarted, err := New(store, s.access)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got, err := restarted.Start(spec, nil, nil)
	if err != nil || got.Status != "succeeded" || calls.Load() != 1 {
		t.Fatalf("completed ID replayed after restart: %+v %v", got, err)
	}
}

func TestInterruptedJobsAreRecoveredWithoutReplayAndCredentialsStayPrivate(t *testing.T) {
	s, store, root := fixture(t)
	s.run = func(_ context.Context, _ registry.GitSpec, _ *gitaccess.Connection, auth *gitaccess.Auth) (gitaccess.Result, error) {
		return gitaccess.Result{Output: "diagnostic " + auth.Token}, nil
	}
	c := gitaccess.Connection{ID: "account", Login: "octocat", Mode: "token", Lifetime: "run"}
	auth := gitaccess.Auth{Kind: "token", Token: "private-run-fixture"}
	spec := registry.GitSpec{ID: "authorized", ProjectID: "project", Path: root, Action: "fetch"}
	if _, err := s.Start(spec, &c, &auth); err != nil {
		t.Fatal(err)
	}
	got := wait(t, s, spec.ID)
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), auth.Token) || !strings.Contains(got.Output, "[redacted]") {
		t.Fatal("operation snapshot exposed a forwarded credential")
	}
	s.Close()
	spec.ID = "interrupted"
	if _, _, err := store.ReserveGitOperation(spec); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(store, s.access)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got, err = restarted.Start(spec, &c, &auth)
	if err != nil || got.Status != "interrupted" || restarted.ActiveCount() != 0 {
		t.Fatalf("recovery replayed an operation with an unknown outcome: %+v %v", got, err)
	}
}

func TestExplicitCancellationAndShutdownReleaseReservations(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "shutdown"}[shutdown], func(t *testing.T) {
			s, _, root := fixture(t)
			started := make(chan struct{})
			s.run = func(ctx context.Context, _ registry.GitSpec, _ *gitaccess.Connection, _ *gitaccess.Auth) (gitaccess.Result, error) {
				close(started)
				<-ctx.Done()
				return gitaccess.Result{}, ctx.Err()
			}
			if _, err := s.Start(registry.GitSpec{ID: "cancel", ProjectID: "project", Path: root, Action: "commit", Message: "test"}, nil, nil); err != nil {
				t.Fatal(err)
			}
			<-started
			want := "cancelled"
			if shutdown {
				s.Close()
				want = "interrupted"
			} else if _, err := s.Cancel("cancel"); err != nil {
				t.Fatal(err)
			}
			if got := wait(t, s, "cancel"); got.Status != want || s.BusyPath(root) {
				t.Fatalf("cancellation left an active reservation: %+v", got)
			}
		})
	}
}

type failingCompletionStore struct{ *registry.Store }

func (s failingCompletionStore) ChangeGitOperation(id string, change func(*registry.GitOperation)) (registry.GitOperation, error) {
	o, err := s.GitOperation(id)
	if err != nil {
		return o, err
	}
	change(&o)
	if !o.Active() {
		return o, errors.New("fixture storage unavailable")
	}
	return s.Store.ChangeGitOperation(id, change)
}

func TestUnpersistedOutcomeIsReportedAndCannotBeReplayed(t *testing.T) {
	s, store, root := fixture(t)
	s.store = failingCompletionStore{store}
	put(t, root, "tracked", "contents")
	spec := registry.GitSpec{ID: "storage-failure", ProjectID: "project", Path: root, Action: "stage", Paths: []string{"tracked"}}
	if _, err := s.Start(spec, nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Wait(ctx, spec.ID); err == nil || !strings.Contains(err.Error(), "persist") {
		t.Fatalf("unrecorded result was reported as success or left observers waiting: %v", err)
	}
	if git(t, root, "ls-files") != "tracked" {
		t.Fatal("fixture did not exercise an already-applied write")
	}
	if !s.BusyPath(root) {
		t.Fatal("unrecorded outcome released the reservation")
	}
	if _, err := s.Start(spec, nil, nil); err == nil {
		t.Fatal("unrecorded operation was replayed")
	}
}
