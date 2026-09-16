package projects

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func removalProject(t *testing.T, s *Service, st *registry.Store, dir string) *registry.Project {
	t.Helper()
	o, err := s.Start(Request{ProjectSpec: registry.ProjectSpec{ID: "project", Source: "create", Name: "App", Path: filepath.Join(dir, "app")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := terminal(t, s, o.ID); got.Status != "succeeded" {
		t.Fatalf("%+v", got)
	}
	if err := os.WriteFile(filepath.Join(o.Path, "keep.txt"), []byte("original files"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := st.Project(o.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFileRemovalCommitsMembershipAndReplayCannotDeleteNewFiles(t *testing.T) {
	s, st, dir := fixture(t)
	p := removalProject(t, s, st, dir)
	if err := st.Import(registry.Import{
		Agents: []registry.Agent{{Record: registry.Record{ID: "agent"}, Name: "Codex", AgentType: "codex"}},
		Chats:  []registry.Chat{{Record: registry.Record{ID: "chat"}, AgentID: "agent", ProjectID: p.ID, Title: "Build"}},
	}); err != nil {
		t.Fatal(err)
	}
	request := RemoveRequest{OperationID: "remove", ExpectedRevision: p.Revision}
	stale := request
	stale.ExpectedRevision++
	if _, err := s.RemoveFiles(p.ID, stale); !errors.Is(err, registry.ErrConflict) {
		t.Fatalf("stale removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Path, "keep.txt")); err != nil {
		t.Fatal("stale request touched files", err)
	}
	o, err := s.RemoveFiles(p.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	o = terminal(t, s, o.ID)
	if o.Status != "succeeded" || o.Source != "delete" {
		t.Fatalf("%+v", o)
	}
	snapshot, err := st.Snapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Projects) != 0 || len(snapshot.Chats) != 0 || len(snapshot.Agents) != 1 || len(snapshot.Deleted) != 2 {
		t.Fatalf("non-atomic membership removal: %+v", snapshot)
	}
	if _, err := os.Stat(p.Path); !os.IsNotExist(err) {
		t.Fatalf("folder remains: %v", err)
	}
	if err := os.Mkdir(p.Path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Path, "new.txt"), []byte("new files"), 0600); err != nil {
		t.Fatal(err)
	}
	again, err := s.RemoveFiles(p.ID, request)
	if err != nil || again.Sequence != o.Sequence {
		t.Fatalf("replay=%+v err=%v", again, err)
	}
	if _, err := os.Stat(filepath.Join(p.Path, "new.txt")); err != nil {
		t.Fatal("replayed deletion touched a replacement folder", err)
	}
}

type busyFixture struct{ chat, path bool }

func (b busyFixture) Busy(string) bool     { return b.chat }
func (b busyFixture) BusyPath(string) bool { return b.path }

func TestFileRemovalChecksRunningChatsAndOverlappingProjectsBeforeTouchingFiles(t *testing.T) {
	s, st, dir := fixture(t)
	p := removalProject(t, s, st, dir)
	if err := st.Import(registry.Import{
		Agents: []registry.Agent{{Record: registry.Record{ID: "agent"}, Name: "Codex", AgentType: "codex"}},
		Chats:  []registry.Chat{{Record: registry.Record{ID: "chat"}, AgentID: "agent", ProjectID: p.ID, Title: "Build"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, busy := range []busyFixture{{chat: true}, {path: true}} {
		s.runs = busy
		if _, err := s.RemoveFiles(p.ID, RemoveRequest{OperationID: "remove", ExpectedRevision: p.Revision}); !errors.Is(err, registry.ErrConflict) {
			t.Fatalf("busy removal: %v", err)
		}
	}
	s.runs = nil
	if err := st.Import(registry.Import{Projects: []registry.Project{{Record: registry.Record{ID: "child"}, Name: "Child", Path: filepath.Join(p.Path, "child")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveFiles(p.ID, RemoveRequest{OperationID: "remove", ExpectedRevision: p.Revision}); !errors.Is(err, registry.ErrConflict) {
		t.Fatalf("nested removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Path, "keep.txt")); err != nil {
		t.Fatal("rejected removal touched files", err)
	}
	if operations, err := s.List(true); err != nil || len(operations) != 0 {
		t.Fatalf("rejected request reserved a job: %+v %v", operations, err)
	}
	// These checks are pure validation; the test never submits a real home/root deletion.
	home, _ := os.UserHomeDir()
	for _, path := range []string{string(filepath.Separator), home, st.Directory()} {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ensureRemovable(registry.ProjectOperation{ProjectSpec: registry.ProjectSpec{Path: canonical}}); !errors.Is(err, registry.ErrInvalid) {
			t.Fatalf("protected directory %s: %v", canonical, err)
		}
	}
}

func quarantinedRemoval(t *testing.T, st *registry.Store, p *registry.Project) registry.ProjectOperation {
	t.Helper()
	o, _, err := st.ReserveRemoval("remove", p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	o, err = st.ChangeProjectOperation(o.ID, func(v *registry.ProjectOperation) error { v.Status, v.Phase = "running", "removing"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareStage(o); err != nil {
		t.Fatal(err)
	}
	if err := renameExclusive(p.Path, filepath.Join(stagePath(o), "project")); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestFileRemovalRecoversEitherSideOfCommit(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-commit", true: "after-commit"}[committed], func(t *testing.T) {
			s, st, dir := fixture(t)
			p := removalProject(t, s, st, dir)
			s.Close()
			o := quarantinedRemoval(t, st, p)
			if committed {
				var err error
				o, err = st.CommitRemoval(o.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(p.Path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p.Path, "new.txt"), []byte("replacement"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			restarted, err := New(st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			if got := terminal(t, restarted, o.ID); got.Status != "succeeded" {
				t.Fatalf("recovery: %+v", got)
			}
			if _, err := st.Project(p.ID); !errors.Is(err, registry.ErrNotFound) {
				t.Fatalf("membership remains: %v", err)
			}
			if _, err := os.Stat(stagePath(o)); !os.IsNotExist(err) {
				t.Fatalf("quarantine remains: %v", err)
			}
			if committed {
				if _, err := os.Stat(filepath.Join(p.Path, "new.txt")); err != nil {
					t.Fatal("recovery deleted replacement files", err)
				}
			} else if _, err := os.Stat(p.Path); !os.IsNotExist(err) {
				t.Fatalf("original directory remains: %v", err)
			}
		})
	}
}

func TestUncommittedRemovalRestoresOnCancellationOrPreservesQuarantineOnConflict(t *testing.T) {
	for _, blockedRestore := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "restore-conflict"}[blockedRestore], func(t *testing.T) {
			s, st, dir := fixture(t)
			p := removalProject(t, s, st, dir)
			s.Close()
			o := quarantinedRemoval(t, st, p)
			_, err := st.ChangeProjectOperation(o.ID, func(v *registry.ProjectOperation) error { v.Status = "cancelling"; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if blockedRestore {
				if err := os.Mkdir(p.Path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p.Path, "new.txt"), []byte("replacement"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			restarted, err := New(st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			got := terminal(t, restarted, o.ID)
			wantStatus := "cancelled"
			if blockedRestore {
				wantStatus = "failed"
			}
			if got.Status != wantStatus {
				t.Fatalf("%+v", got)
			}
			if _, err := st.Project(p.ID); err != nil {
				t.Fatal("uncommitted removal dropped membership", err)
			}
			file := filepath.Join(p.Path, "keep.txt")
			if blockedRestore {
				file = filepath.Join(stagePath(o), "project", "keep.txt")
				if !strings.Contains(got.Error, stagePath(o)) {
					t.Fatalf("recovery location was hidden: %+v", got)
				}
				if _, err := os.Stat(filepath.Join(p.Path, "new.txt")); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Stat(stagePath(o)); !os.IsNotExist(err) {
				t.Fatalf("restored staging remains: %v", err)
			}
			if body, err := os.ReadFile(file); err != nil || string(body) != "original files" {
				t.Fatalf("original data lost: %q %v", body, err)
			}
		})
	}
}

func TestCommittedCleanupRetryDoesNotTouchReplacementProject(t *testing.T) {
	s, st, dir := fixture(t)
	p := removalProject(t, s, st, dir)
	s.Close()
	o := quarantinedRemoval(t, st, p)
	if _, err := st.CommitRemoval(o.ID); err != nil {
		t.Fatal(err)
	}
	unexpected := filepath.Join(stagePath(o), "unexpected.txt")
	if err := os.WriteFile(unexpected, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cancel(o.ID); !errors.Is(err, registry.ErrConflict) {
		t.Fatalf("committed deletion accepted cancel: %v", err)
	}
	restarted, err := New(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if got := terminal(t, restarted, o.ID); got.Status != "failed" || got.Phase != "deleting" {
		t.Fatalf("cleanup boundary lost: %+v", got)
	}
	if err := os.Remove(unexpected); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p.Path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Path, "new.txt"), []byte("new project"), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(registry.Project{Name: "Replacement", Path: p.Path})
	if err := restarted.Save("replacement", data, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Retry(o.ID, Auth{}); err != nil {
		t.Fatal(err)
	}
	if got := terminal(t, restarted, o.ID); got.Status != "succeeded" || got.Attempt != 2 {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(filepath.Join(p.Path, "new.txt")); err != nil {
		t.Fatal("cleanup retry touched new project", err)
	}
}
