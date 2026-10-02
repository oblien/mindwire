package registry

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestFinishSyncOperationRollsBackStatusAndReservationTogether(t *testing.T) {
	for _, fault := range []struct {
		name    string
		trigger string
	}{
		{"result", `CREATE TRIGGER fail_finish BEFORE UPDATE ON project_sync_records
WHEN OLD.kind = 'operation' AND OLD.id = 'sync'
BEGIN SELECT RAISE(ABORT, 'injected result failure'); END`},
		{"reservation", `CREATE TRIGGER fail_finish BEFORE DELETE ON project_sync_locks
WHEN OLD.id = 'sync'
BEGIN SELECT RAISE(ABORT, 'injected reservation failure'); END`},
	} {
		t.Run(fault.name, func(t *testing.T) {
			st := openTest(t)
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path, other := filepath.Join(root, "project"), filepath.Join(root, "other")
			for id, path := range map[string]string{"sync": path, "other": other} {
				if err := st.ReserveSync(id, path); err != nil {
					t.Fatal(err)
				}
			}
			before := map[string]string{"status": "preparing"}
			after := map[string]string{"status": "conflicts"}
			if err := st.SyncPut("operation", "sync", before); err != nil {
				t.Fatal(err)
			}
			if _, err := st.db.Exec(fault.trigger); err != nil {
				t.Fatal(err)
			}
			if err := st.FinishSyncOperation("sync", after); err == nil {
				t.Fatal("completion ignored the injected database failure")
			}
			var got map[string]string
			if err := st.SyncGet("operation", "sync", &got); err != nil || got["status"] != before["status"] {
				t.Fatalf("failed completion published a result: %v, %v", got, err)
			}
			if err := st.CheckSyncPath(path); !errors.Is(err, ErrConflict) {
				t.Fatalf("failed completion lost its reservation: %v", err)
			}
			if _, err := st.db.Exec("DROP TRIGGER fail_finish"); err != nil {
				t.Fatal(err)
			}
			if err := st.FinishSyncOperation("sync", after); err != nil {
				t.Fatal(err)
			}
			if err := st.SyncGet("operation", "sync", &got); err != nil || got["status"] != after["status"] {
				t.Fatalf("retry did not publish its result: %v, %v", got, err)
			}
			if err := st.CheckSyncPath(path); err != nil {
				t.Fatalf("retry retained its reservation: %v", err)
			}
			if err := st.CheckSyncPath(other); !errors.Is(err, ErrConflict) {
				t.Fatalf("completion released another operation's reservation: %v", err)
			}
		})
	}
}

func TestCompleteSyncProtectsCommittedDataUntilResultIsPublished(t *testing.T) {
	st := openTest(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	batch := fixture()
	batch.Projects[0].Path = root
	if err := st.ReserveSync("sync", root); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteSync(batch, "project-sync-id", "checkpoint", "sync", nil, nil); err != nil {
		t.Fatal(err)
	}
	var committed bool
	if err := st.SyncGet("committed", "sync", &committed); err != nil || !committed {
		t.Fatalf("missing recovery acknowledgement: %v, %v", committed, err)
	}
	if err := st.CheckSyncPath(root); !errors.Is(err, ErrConflict) {
		t.Fatalf("committed data was unlocked before the result was published: %v", err)
	}
	if err := st.FinishSyncOperation("sync", map[string]string{"status": "succeeded"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckSyncPath(root); err != nil {
		t.Fatalf("completed operation retained its reservation: %v", err)
	}
}
