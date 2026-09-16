package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func fixture() Import {
	return Import{
		Agents:   []Agent{{Record: Record{ID: "agent", CreatedAt: "2026-09-16T00:00:00Z"}, Name: "My Codex", AgentType: "codex"}},
		Projects: []Project{{Record: Record{ID: "project", CreatedAt: "2026-09-16T00:00:00Z"}, Name: "App", Path: "/work/app"}},
		Chats:    []Chat{{Record: Record{ID: "chat", CreatedAt: "2026-09-16T00:00:00Z"}, AgentID: "agent", ProjectID: "project", Title: "Build it", SessionID: "native-session"}},
	}
}

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func snapshot(t *testing.T, st *Store, since *int64) Snapshot {
	t.Helper()
	s, err := st.Snapshot(since)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestImportIsDurableIdempotentAndPreservesIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Import(fixture()); err != nil {
		t.Fatal(err)
	}
	first := snapshot(t, st, nil)
	if first.Revision != 1 || first.Agents[0].ID != "agent" || first.Projects[0].ID != "project" || first.Chats[0].SessionID != "native-session" {
		t.Fatalf("bad import: %+v", first)
	}
	if err = st.Import(fixture()); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, st, nil).Revision != first.Revision {
		t.Fatal("duplicate import changed revision")
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	second := snapshot(t, reopened, nil)
	if second.WorkspaceID != first.WorkspaceID || second.Revision != first.Revision || len(second.Chats) != 1 {
		t.Fatalf("lost metadata after restart: %+v", second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database mode: %v", info.Mode())
	}
}

func TestImportRollsBackMissingParentsAndInvalidRecords(t *testing.T) {
	st := openTest(t)
	batch := fixture()
	batch.Chats[0].AgentID = "missing"
	if err := st.Import(batch); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing parent: %v", err)
	}
	s := snapshot(t, st, nil)
	if s.Revision != 0 || len(s.Agents)+len(s.Projects)+len(s.Chats) != 0 {
		t.Fatalf("partial import committed: %+v", s)
	}
	batch = fixture()
	batch.Projects[0].Path = ""
	if err := st.Import(batch); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid project: %v", err)
	}
	if snapshot(t, st, nil).Revision != 0 {
		t.Fatal("invalid import advanced checkpoint")
	}
}

func TestDeleteCascadesLinksAndOldImportsCannotResurrectThem(t *testing.T) {
	st := openTest(t)
	if err := st.Import(fixture()); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, st, nil)
	if err := st.Delete("projects", "project", &before.Projects[0].Revision, false); err != nil {
		t.Fatal(err)
	}
	delta := snapshot(t, st, &before.Revision)
	if delta.Full || len(delta.Deleted) != 2 || delta.Revision != 2 {
		t.Fatalf("missing cascade delta: %+v", delta)
	}
	if err := st.Import(fixture()); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, st, nil)
	if len(after.Projects) != 0 || len(after.Chats) != 0 || len(after.Agents) != 1 {
		t.Fatalf("stale import resurrected membership: %+v", after)
	}
	data, _ := json.Marshal(fixture().Projects[0])
	if err := st.Put("projects", "project", data, nil); !errors.Is(err, ErrDeleted) {
		t.Fatalf("deleted id recreated: %v", err)
	}
	if _, _, _, err := st.ChatContext("chat"); !errors.Is(err, ErrDeleted) {
		t.Fatalf("deleted chat can run: %v", err)
	}
	if err := st.Delete("projects", "project", nil, false); err != nil {
		t.Fatal("delete retry failed:", err)
	}
}

func TestConcurrentEditsHaveOneWinnerAndDoNotOverwrite(t *testing.T) {
	st := openTest(t)
	if err := st.Import(fixture()); err != nil {
		t.Fatal(err)
	}
	revision := snapshot(t, st, nil).Agents[0].Revision
	var winners, conflicts atomic.Int32
	var wg sync.WaitGroup
	for _, name := range []string{"First phone", "Second phone"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			row := fixture().Agents[0]
			row.Name = name
			data, _ := json.Marshal(row)
			err := st.Put("agents", row.ID, data, &revision)
			if err == nil {
				winners.Add(1)
			} else if errors.Is(err, ErrConflict) {
				conflicts.Add(1)
			} else {
				t.Errorf("put: %v", err)
			}
		}(name)
	}
	wg.Wait()
	if winners.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners.Load(), conflicts.Load())
	}
	current := snapshot(t, st, nil).Agents[0]
	data, _ := json.Marshal(current)
	if err := st.Put("agents", current.ID, data, &revision); err != nil {
		t.Fatal("lost-ack retry should be idempotent:", err)
	}
	if snapshot(t, st, nil).Revision != 2 {
		t.Fatal("unchanged retry advanced revision")
	}
}

func TestSnapshotRevisionAndParentInvariants(t *testing.T) {
	st := openTest(t)
	if err := st.Import(fixture()); err != nil {
		t.Fatal(err)
	}
	s := snapshot(t, st, nil)
	empty := snapshot(t, st, &s.Revision)
	if empty.Full || len(empty.Agents)+len(empty.Projects)+len(empty.Chats)+len(empty.Deleted) != 0 {
		t.Fatalf("unchanged delta: %+v", empty)
	}
	future := s.Revision + 1
	if _, err := st.Snapshot(&future); !errors.Is(err, ErrConflict) {
		t.Fatalf("future cursor: %v", err)
	}
	chat := s.Chats[0]
	chat.AgentID = "another"
	data, _ := json.Marshal(chat)
	if err := st.Put("chats", chat.ID, data, &chat.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reparented chat: %v", err)
	}
	profile := s.Agents[0]
	profile.AgentType = "claude-code"
	data, _ = json.Marshal(profile)
	if err := st.Put("agents", profile.ID, data, &profile.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed harness: %v", err)
	}
	if err := st.RenameChat("chat", "Pinned title"); err != nil {
		t.Fatal(err)
	}
	chatNow, profileNow, projectNow, err := st.ChatContext("chat")
	if err != nil || chatNow.Title != "Pinned title" || !chatNow.TitleIsUserSet || profileNow.ID != "agent" || projectNow.Path != "/work/app" {
		t.Fatalf("chat context: %+v %+v %+v %v", chatNow, profileNow, projectNow, err)
	}
}
