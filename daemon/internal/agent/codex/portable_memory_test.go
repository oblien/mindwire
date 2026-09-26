package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPortableMemorySelectedRowsRoundTripAndSchemaGuards(t *testing.T) {
	ctx := context.Background()
	x, y := t.TempDir(), t.TempDir()
	a := adapter{}
	memory := portableMemory{Version: 1, ThreadID: "thread-one", SourceUpdatedAt: 10,
		RawMemory: "Keep the original decision", RolloutSummary: "Created the project", GeneratedAt: 11,
		Migrations: supportedMemoryMigrations()}
	first, _ := json.Marshal(memory)
	t.Setenv("CODEX_HOME", x)
	if err := a.ApplySessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json", nil, first); err != nil {
		t.Fatal(err)
	}
	got, err := a.ReadSessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json")
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("source memory: %s %v", got, err)
	}
	t.Setenv("CODEX_HOME", y)
	unrelated := memory
	unrelated.ThreadID, unrelated.RawMemory = "unrelated", "Y private memory"
	other, _ := json.Marshal(unrelated)
	if err = a.ApplySessionData(ctx, "/other", "unrelated", "memory/unrelated.json", nil, other); err != nil {
		t.Fatal(err)
	}
	if err = a.ValidateSessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json", first); err != nil {
		t.Fatal(err)
	}
	if err = a.ApplySessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json", nil, first); err != nil {
		t.Fatal(err)
	}
	db, err := memoryDB(filepath.Join(y, "memories_1.sqlite"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE stage1_outputs SET usage_count=7,selected_for_phase2=1,selected_for_phase2_source_updated_at=10 WHERE thread_id='thread-one'"); err != nil {
		t.Fatal(err)
	}
	memory.RawMemory += "; Y continuation"
	memory.SourceUpdatedAt = 12
	second, _ := json.Marshal(memory)
	if err = a.ApplySessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json", first, second); err != nil {
		t.Fatal(err)
	}
	var used, selected int
	if err = db.QueryRow("SELECT usage_count,selected_for_phase2 FROM stage1_outputs WHERE thread_id='thread-one'").Scan(&used, &selected); err != nil {
		t.Fatal(err)
	}
	if used != 7 || selected != 0 {
		t.Fatal("destination counters were lost or stale consolidation retained")
	}
	// A stale retry is idempotent. A genuinely changed record cannot be replaced.
	if err = a.ApplySessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json", first, second); err != nil {
		t.Fatal(err)
	}
	if err = a.ApplySessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json", nil, first); err == nil {
		t.Fatal("overwrote independent memory")
	}
	got, err = a.ReadSessionData(ctx, "/other", "unrelated", "memory/unrelated.json")
	if err != nil || !bytes.Equal(got, other) {
		t.Fatal("unrelated database row was changed", err)
	}
	db.Close()
	t.Setenv("CODEX_HOME", x)
	if err = a.ApplySessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json", first, second); err != nil {
		t.Fatal(err)
	}
	got, err = a.ReadSessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json")
	if err != nil || !bytes.Equal(got, second) {
		t.Fatal("return synchronization lost memory", err)
	}
	db, err = memoryDB(filepath.Join(x, "memories_1.sqlite"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("ALTER TABLE jobs ADD COLUMN future_required_context TEXT"); err != nil {
		t.Fatal(err)
	}
	if err = a.ValidateSessionData(ctx, "/project", memory.ThreadID, "memory/thread-one.json", second); err == nil {
		t.Fatal("unknown native table layout accepted")
	}
}

func TestPortableMemoryRejectsUnknownMigrationsAndOtherStores(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	m := portableMemory{Version: 1, ThreadID: "session", Migrations: []memoryMigration{{999, "unknown", []byte{1}}}}
	b, _ := json.Marshal(m)
	a := adapter{}
	if err := a.ValidateSessionData(ctx, "/project", "session", "memory/session.json", b); err == nil {
		t.Fatal("untrusted migrations accepted")
	}
	if _, err := os.Stat(filepath.Join(home, "memories_1.sqlite")); !os.IsNotExist(err) {
		t.Fatal("preflight created a database")
	}
	if err := os.WriteFile(filepath.Join(home, "memories_2.sqlite"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReadSessionData(ctx, "/project", "session", "memory/session.json"); err == nil {
		t.Fatal("silently omitted a newer memory store")
	}
}
