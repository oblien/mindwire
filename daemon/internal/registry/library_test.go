package registry

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func librarySnapshot(t *testing.T, st *Store) ProjectLibrary {
	t.Helper()
	library, err := st.ProjectLibrary()
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func TestProjectLibrarySurvivesRestartAndOnlyChangesOrganization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	if err := st.Import(fixture()); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, st, nil)
	if before.ProjectLibrary == nil || before.ProjectLibrary.Version != 1 || before.ProjectLibrary.Revision != 0 {
		t.Fatal("full snapshots must expose the empty library for feature discovery")
	}
	if snapshot(t, st, &before.Revision).ProjectLibrary != nil {
		t.Fatal("unchanged library must not be repeated in every delta")
	}
	folder := "work"
	edit := ProjectLibraryEdit{Folders: []ProjectFolder{{ID: folder, Name: "Work"}},
		Placements: []ProjectPlacement{{ProjectID: "project", FolderID: &folder}}, Order: []string{"project"}}
	if err := st.EditProjectLibrary(edit); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, st, nil)
	if !reflect.DeepEqual(before.Import, after.Import) || len(after.Deleted) != 0 {
		t.Fatal("organizing projects changed their records or conversations")
	}
	delta := snapshot(t, st, &before.Revision)
	if delta.ProjectLibrary == nil || len(delta.Projects)+len(delta.Agents)+len(delta.Chats) != 0 {
		t.Fatalf("library-only delta: %+v", delta)
	}
	if err := st.EditProjectLibrary(edit); err != nil || snapshot(t, st, nil).Revision != after.Revision {
		t.Fatalf("retry after a lost acknowledgement was not idempotent: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := librarySnapshot(t, st); !reflect.DeepEqual(got, *after.ProjectLibrary) {
		t.Fatalf("restart lost organization: %+v", got)
	}
}

func TestProjectLibraryConcurrentClientsAndConditionalRetry(t *testing.T) {
	st := openTest(t)
	var winners, conflicts atomic.Int32
	var wg sync.WaitGroup
	for _, name := range []string{"First", "Second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			err := st.EditProjectLibrary(ProjectLibraryEdit{Folders: []ProjectFolder{{ID: name, Name: name}}})
			if err == nil {
				winners.Add(1)
			} else if errors.Is(err, ErrConflict) {
				conflicts.Add(1)
			} else {
				t.Errorf("edit: %v", err)
			}
		}(name)
	}
	wg.Wait()
	if winners.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners.Load(), conflicts.Load())
	}
	before := librarySnapshot(t, st)
	folder := before.Folders[0]
	folder.Name = "Renamed"
	if err := st.EditProjectLibrary(ProjectLibraryEdit{Folders: []ProjectFolder{folder}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit overwrote another device: %v", err)
	}
	if err := st.EditProjectLibrary(ProjectLibraryEdit{ExpectedRevision: before.Revision, Folders: []ProjectFolder{folder}}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectLibraryInvalidBatchRollsBackEveryPart(t *testing.T) {
	st := openTest(t)
	if err := st.Import(fixture()); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, st, nil)
	id := "folder"
	for _, edit := range []ProjectLibraryEdit{
		{Folders: []ProjectFolder{{ID: id, Name: "Work"}}, Placements: []ProjectPlacement{{ProjectID: "missing", FolderID: &id}}},
		{Folders: []ProjectFolder{{ID: id, Name: "Work"}}, Order: []string{"missing"}},
		{DeleteFolders: []string{id}, Folders: []ProjectFolder{{ID: id, Name: "Conflicting intent"}}},
		{Folders: []ProjectFolder{{ID: id, Name: "Work"}, {ID: "duplicate", Name: "work"}}},
		{Folders: []ProjectFolder{{ID: id, Name: "Work"}}, FolderOrder: []string{"missing"}},
	} {
		if err := st.EditProjectLibrary(edit); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid batch accepted: %+v %v", edit, err)
		}
		if after := snapshot(t, st, nil); !reflect.DeepEqual(before, after) {
			t.Fatal("an invalid batch partially committed")
		}
	}
	// A rolled-back deletion must not leave an invisible tombstone.
	if err := st.EditProjectLibrary(ProjectLibraryEdit{Folders: []ProjectFolder{{ID: id, Name: "Work"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectLibraryScopedOrderingPreservesOtherSlots(t *testing.T) {
	st := openTest(t)
	batch := Import{}
	for _, id := range []string{"a", "b", "c", "d"} {
		batch.Projects = append(batch.Projects, Project{Record: Record{ID: id}, Name: id, Path: "/work/" + id})
	}
	if err := st.Import(batch); err != nil {
		t.Fatal(err)
	}
	if err := st.EditProjectLibrary(ProjectLibraryEdit{Order: []string{"a", "b", "c", "d"},
		Folders: []ProjectFolder{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}, {ID: "three", Name: "Three"}}}); err != nil {
		t.Fatal(err)
	}
	before := librarySnapshot(t, st)
	if err := st.EditProjectLibrary(ProjectLibraryEdit{ExpectedRevision: before.Revision,
		Order: []string{"c", "a"}, FolderOrder: []string{"three", "one"}}); err != nil {
		t.Fatal(err)
	}
	after := librarySnapshot(t, st)
	if !reflect.DeepEqual(after.Order, []string{"c", "b", "a", "d"}) || after.Folders[1].ID != "two" || after.Folders[0].ID != "three" {
		t.Fatalf("scoped reorder moved unrelated projects/folders: %+v", after)
	}
}

func TestProjectFolderDeletionKeepsFilesChatsAndBlocksResurrection(t *testing.T) {
	st := openTest(t)
	batch := fixture()
	batch.Projects[0].Path = t.TempDir()
	file := filepath.Join(batch.Projects[0].Path, "keep.txt")
	if err := os.WriteFile(file, []byte("keep my work"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := st.Import(batch); err != nil {
		t.Fatal(err)
	}
	folder := "work"
	create := ProjectLibraryEdit{Folders: []ProjectFolder{{ID: folder, Name: "Work"}},
		Placements: []ProjectPlacement{{ProjectID: "project", FolderID: &folder}}, Order: []string{"project"}}
	if err := st.EditProjectLibrary(create); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, st, nil)
	remove := ProjectLibraryEdit{ExpectedRevision: before.ProjectLibrary.Revision, DeleteFolders: []string{folder}}
	if err := st.EditProjectLibrary(remove); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, st, nil)
	if len(after.ProjectLibrary.Folders)+len(after.ProjectLibrary.Membership) != 0 || len(after.ProjectLibrary.Order) != 1 || !reflect.DeepEqual(after.Import, before.Import) {
		t.Fatalf("folder deletion touched its projects: %+v", after)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep my work" {
		t.Fatal("folder deletion changed files")
	}
	if err := st.EditProjectLibrary(remove); err != nil || snapshot(t, st, nil).Revision != after.Revision {
		t.Fatal("repeated deletion must be a no-op", err)
	}
	create.ExpectedRevision = after.Revision
	if err := st.EditProjectLibrary(create); !errors.Is(err, ErrDeleted) {
		t.Fatalf("offline client resurrected deleted folder: %v", err)
	}
	if err := st.Delete("projects", "project", &after.Projects[0].Revision, false); err != nil {
		t.Fatal(err)
	}
	delta := snapshot(t, st, &after.Revision)
	if delta.ProjectLibrary == nil || len(delta.ProjectLibrary.Order) != 0 || len(delta.Deleted) != 2 {
		t.Fatalf("project deletion left library references: %+v", delta)
	}
}

func TestLibraryMigrationCannotReplaceExistingOrganizationOrDeletedFolder(t *testing.T) {
	st := openTest(t)
	if err := st.EditProjectLibrary(ProjectLibraryEdit{DeleteFolders: []string{"offline"}}); err != nil {
		t.Fatal(err)
	}
	before := librarySnapshot(t, st)
	if err := st.EditProjectLibrary(ProjectLibraryEdit{ExpectedRevision: before.Revision,
		Folders: []ProjectFolder{{ID: "offline", Name: "Never uploaded"}}}); !errors.Is(err, ErrDeleted) {
		t.Fatalf("deleting an unacknowledged folder did not suppress its delayed upload: %v", err)
	}
	if err := st.EditProjectLibrary(ProjectLibraryEdit{ImportIfEmpty: true,
		Folders: []ProjectFolder{{ID: "stale", Name: "Local copy"}}}); err != nil {
		t.Fatal(err)
	}
	if got := librarySnapshot(t, st); !reflect.DeepEqual(got, before) {
		t.Fatal("local-only migration overwrote another client's organization")
	}
}
