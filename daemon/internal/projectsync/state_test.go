package projectsync

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func TestPersistedOperationStatusAndReservationAgree(t *testing.T) {
	for _, tt := range []struct {
		status string
		locked bool
	}{
		{"queued", true},
		{"exporting", true},
		{"preparing", true},
		{"applying", true},
		{"recovery_required", true},
		{"succeeded", false},
		{"conflicts", false},
		{"failed", false},
	} {
		t.Run(tt.status, func(t *testing.T) {
			f := testService(t)
			o := Operation{Request: Request{ID: "operation"}, Kind: "import", Status: tt.status}
			if err := f.reg.ReserveSync(o.ID, f.project); err != nil {
				t.Fatal(err)
			}
			if err := f.s.save(o); err != nil {
				t.Fatal(err)
			}
			// A client that has observed the durable result must be able to act
			// on it immediately, without waiting for worker/defer cleanup.
			observed, err := f.s.Get(o.ID)
			if err != nil || observed.Status != tt.status {
				t.Fatalf("operation: %+v, %v", observed, err)
			}
			err = f.reg.CheckProjectPath(f.project)
			if tt.locked {
				if !errors.Is(err, registry.ErrConflict) {
					t.Fatalf("%s must retain its reservation: %v", tt.status, err)
				}
			} else if err != nil {
				t.Fatalf("%s was visible before its reservation was released: %v", tt.status, err)
			}
		})
	}
}

func TestCommittedJournalRecoveryOnlyFinishesOperation(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, "file.txt"), "original")
	c := mustRun(t, x.s, "export", Request{ID: "export", ProjectID: "x"})
	relay(t, x.s, y.s, c.ResultCheckpointID)
	o := mustRun(t, y.s, "import", Request{ID: "import", CheckpointID: c.ResultCheckpointID, Path: y.project})
	y.s.Close()
	// Simulate a restart after metadata was committed but before the final
	// operation status and reservation release were committed.
	o.Status, o.Phase = "applying", "apply"
	if err := y.reg.ReserveSync(o.ID, y.project); err != nil {
		t.Fatal(err)
	}
	if err := y.s.save(o); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(y.project, "file.txt"), "external edit after commit")
	s, err := New(y.reg, y.store, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	y.s = s
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	finished, err := s.Wait(ctx, o.ID)
	if err != nil || finished.Status != "succeeded" || finished.ResultCheckpointID != o.ResultCheckpointID {
		t.Fatalf("committed recovery: %+v, %v", finished, err)
	}
	if got := readFile(t, filepath.Join(y.project, "file.txt")); got != "external edit after commit" {
		t.Fatal("recovery reapplied an already committed file")
	}
	if err := y.reg.CheckProjectPath(y.project); err != nil {
		t.Fatalf("completed recovery retained its reservation: %v", err)
	}
}

func TestUnreadableJournalKeepsProjectProtected(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, "file.txt"), "original")
	c := mustRun(t, x.s, "export", Request{ID: "export", ProjectID: "x"})
	relay(t, x.s, y.s, c.ResultCheckpointID)
	y.s.afterWrite = func(int) error {
		if err := y.reg.SyncPut("journal", "import", "unreadable journal"); err != nil {
			return err
		}
		return errors.New("simulated interruption")
	}
	o := run(t, y.s, "import", Request{ID: "import", CheckpointID: c.ResultCheckpointID, Path: y.project})
	if o.Status != "recovery_required" {
		t.Fatalf("unreadable recovery data was treated as an absent journal: %+v", o)
	}
	if err := y.reg.CheckProjectPath(y.project); !errors.Is(err, registry.ErrConflict) {
		t.Fatalf("project with unreadable recovery data was unlocked: %v", err)
	}
}
