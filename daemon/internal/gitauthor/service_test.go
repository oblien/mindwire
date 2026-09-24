package gitauthor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func fixture(t *testing.T) (*Service, string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	global := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if err := os.WriteFile(global, []byte("[user]\nname = Native User\nemail = native@example.invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s := New(store)
	root := newRepository(t)
	register(t, s, "project", root)
	return s, root, global
}

func newRepository(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "-q")
	return root
}

func register(t *testing.T, s *Service, id, path string) {
	t.Helper()
	data, _ := json.Marshal(map[string]string{"name": id, "path": path})
	if err := s.store.Put("projects", id, data, nil); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, directory string, args ...string) string {
	t.Helper()
	data, err := exec.Command("git", append([]string{"-C", directory}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func save(t *testing.T, s *Service, directory string, scope Scope, author *registry.GitIdentity, id string) Settings {
	t.Helper()
	current, err := s.Read(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.Save(context.Background(), directory, Update{Scope: scope, Identity: author,
		RequestID: id, ExpectedRevision: &current.DefaultRevision})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAuthorScopesInheritInNativeGitAndSavingNeverCommits(t *testing.T) {
	s, root, _ := fixture(t)
	beforeGlobal := gitRun(t, root, "config", "--global", "--no-includes", "--get", "user.name")
	second, outside := newRepository(t), newRepository(t)
	register(t, s, "second", second)
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("staged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "file")
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("unstaged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeIndex := gitRun(t, root, "write-tree")
	beforeStatus := gitRun(t, root, "status", "--porcelain")
	all := &registry.GitIdentity{Name: "Shared User", Email: "shared@example.invalid"}
	workspace := &registry.GitIdentity{Name: "Workspace User", Email: "workspace@example.invalid"}
	repository := &registry.GitIdentity{Name: "Repository User", Email: "repository@example.invalid"}
	save(t, s, root, AllWorkspaces, all, "default-1")
	if got := gitRun(t, second, "config", "user.name"); got != all.Name {
		t.Fatal(got)
	}
	if got := gitRun(t, outside, "config", "user.name"); got != "Native User" {
		t.Fatalf("changed an unrelated repository: %s", got)
	}
	save(t, s, root, Workspace, workspace, "")
	save(t, s, root, Repository, repository, "")
	if got := gitRun(t, root, "config", "user.name"); got != repository.Name {
		t.Fatal(got)
	}
	if got := gitRun(t, second, "config", "user.name"); got != workspace.Name {
		t.Fatal(got)
	}
	save(t, s, root, AllWorkspaces, &registry.GitIdentity{Name: "New Default", Email: "new@example.invalid"}, "default-2")
	if got := gitRun(t, root, "config", "user.name"); got != repository.Name {
		t.Fatal("default overwrote repository author")
	}
	state := save(t, s, root, Repository, nil, "")
	if state.Effective != *workspace {
		t.Fatalf("clearing repo override: %+v", state)
	}
	state = save(t, s, root, Workspace, nil, "")
	if state.Effective.Name != "New Default" {
		t.Fatalf("clearing workspace override: %+v", state)
	}
	if gitRun(t, root, "write-tree") != beforeIndex || gitRun(t, root, "status", "--porcelain") != beforeStatus {
		t.Fatal("author setup changed staged or working files")
	}
	if exec.Command("git", "-C", root, "rev-parse", "--verify", "HEAD").Run() == nil {
		t.Fatal("author setup created a commit")
	}
	afterGlobal := gitRun(t, root, "config", "--global", "--no-includes", "--get", "user.name")
	if afterGlobal != beforeGlobal {
		t.Fatal("changed the machine's global author")
	}
	// A future project uses the persisted default through the normal project hook.
	future := newRepository(t)
	register(t, s, "future", future)
	if err := New(s.store).PrepareRepository(context.Background(), future); err != nil {
		t.Fatal(err)
	}
	if got := gitRun(t, future, "config", "user.name"); got != "New Default" {
		t.Fatal(got)
	}
	if err := s.Detach(context.Background(), future); err != nil {
		t.Fatal(err)
	}
	if got := gitRun(t, future, "config", "user.name"); got != "Native User" {
		t.Fatal("removal kept workspace attribution")
	}
}

func TestDefaultAcknowledgementIsIdempotentAndStaleClientsCannotOverwrite(t *testing.T) {
	s, root, _ := fixture(t)
	ctx := context.Background()
	empty := ""
	author := &registry.GitIdentity{Name: "First", Email: "first@example.invalid"}
	req := Update{Scope: AllWorkspaces, Identity: author, RequestID: "first", ExpectedRevision: &empty}
	if _, err := s.Save(ctx, root, req); err != nil {
		t.Fatal(err)
	}
	if state, err := s.Save(ctx, root, req); err != nil || state.DefaultRevision != req.RequestID {
		t.Fatalf("lost acknowledgement retry: %+v %v", state, err)
	}
	newer := &registry.GitIdentity{Name: "Second", Email: "second@example.invalid"}
	save(t, s, root, AllWorkspaces, newer, "second")
	if _, err := s.Save(ctx, root, req); !errors.Is(err, registry.ErrConflict) {
		t.Fatal("stale request overwrote newer settings", err)
	}
	state, _ := s.Read(ctx, root)
	if *state.AllWorkspaces != *newer {
		t.Fatal(state)
	}
}

func TestLaterGitInitializationAndSeparateWorkspacesUseTheirOwnDefaults(t *testing.T) {
	s, root, _ := fixture(t)
	ctx := context.Background()
	shared := &registry.GitIdentity{Name: "First Workspace", Email: "first@example.invalid"}
	save(t, s, root, AllWorkspaces, shared, "first")
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	future := filepath.Join(parent, "project[one]")
	neighbor := filepath.Join(parent, "projecto")
	if err := os.Mkdir(future, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(neighbor, 0700); err != nil {
		t.Fatal(err)
	}
	register(t, s, "future", future)
	if err := s.PrepareRepository(ctx, future); err != nil {
		t.Fatal(err)
	}
	gitRun(t, future, "init", "-q")
	gitRun(t, neighbor, "init", "-q")
	if got := gitRun(t, future, "config", "user.name"); got != shared.Name {
		t.Fatal("later Git init lost author:", got)
	}
	if got := gitRun(t, neighbor, "config", "user.name"); got != "Native User" {
		t.Fatal("path pattern matched another project:", got)
	}
	store, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	second := New(store)
	secondRoot := newRepository(t)
	register(t, second, "second", secondRoot)
	save(t, second, secondRoot, Workspace, &registry.GitIdentity{Name: "Second Workspace", Email: "second@example.invalid"}, "")
	if got := gitRun(t, secondRoot, "config", "user.name"); got != "Second Workspace" {
		t.Fatal(got)
	}
	if got := gitRun(t, root, "config", "user.name"); got != shared.Name {
		t.Fatal("workspaces leaked defaults:", got)
	}
}

func TestConfigLockAndInvalidInputPreserveBothAuthorFields(t *testing.T) {
	s, root, _ := fixture(t)
	good := &registry.GitIdentity{Name: "Name with quote \" and \\ slash مرحبا", Email: "user@example.invalid"}
	save(t, s, root, Repository, good, "")
	config := filepath.Join(root, ".git", "config")
	original, _ := os.ReadFile(config)
	if err := os.WriteFile(config+".lock", []byte("someone else's lock"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Save(context.Background(), root, Update{Scope: Repository, Identity: &registry.GitIdentity{Name: "Other", Email: "other@example.invalid"}})
	if !errors.Is(err, registry.ErrConflict) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(config)
	if string(data) != string(original) {
		t.Fatal("partial config write")
	}
	lock, _ := os.ReadFile(config + ".lock")
	if string(lock) != "someone else's lock" {
		t.Fatal("removed another writer's lock")
	}
	if _, err := s.Save(context.Background(), root, Update{Scope: Workspace, Identity: &registry.GitIdentity{Name: "bad\nname", Email: "bad@example.invalid"}}); !errors.Is(err, registry.ErrInvalid) {
		t.Fatal(err)
	}
}
