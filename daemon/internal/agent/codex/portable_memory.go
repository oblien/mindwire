package codex

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"github.com/oblien/mindwire/daemon/internal/agent"
	_ "modernc.org/sqlite"
)

var _ agent.SessionDataPorter = adapter{}

type memoryMigration struct {
	Version     int64  `json:"version"`
	Description string `json:"description"`
	Checksum    []byte `json:"checksum"`
}
type portableMemory struct {
	Version         int               `json:"version"`
	ThreadID        string            `json:"threadId"`
	SourceUpdatedAt int64             `json:"sourceUpdatedAt"`
	RawMemory       string            `json:"rawMemory"`
	RolloutSummary  string            `json:"rolloutSummary"`
	RolloutSlug     *string           `json:"rolloutSlug"`
	GeneratedAt     int64             `json:"generatedAt"`
	Migrations      []memoryMigration `json:"migrations"`
}

// Native schema is explicit. A future incompatible memory layout fails export;
// arbitrary incoming DDL is never executed. Worker leases/usage counters and the
// global consolidated memory cache remain owned by the destination harness.
const memorySchema = `
CREATE TABLE _sqlx_migrations (version BIGINT PRIMARY KEY, description TEXT NOT NULL, installed_on TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, success BOOLEAN NOT NULL, checksum BLOB NOT NULL, execution_time BIGINT NOT NULL);
CREATE TABLE stage1_outputs (thread_id TEXT PRIMARY KEY, source_updated_at INTEGER NOT NULL, raw_memory TEXT NOT NULL, rollout_summary TEXT NOT NULL, rollout_slug TEXT, generated_at INTEGER NOT NULL, usage_count INTEGER, last_usage INTEGER, selected_for_phase2 INTEGER NOT NULL DEFAULT 0, selected_for_phase2_source_updated_at INTEGER);
CREATE TABLE jobs (kind TEXT NOT NULL, job_key TEXT NOT NULL, status TEXT NOT NULL, worker_id TEXT, ownership_token TEXT, started_at INTEGER, finished_at INTEGER, lease_until INTEGER, retry_at INTEGER, retry_remaining INTEGER NOT NULL, last_error TEXT, input_watermark INTEGER, last_success_watermark INTEGER, PRIMARY KEY(kind,job_key));
CREATE TABLE consolidation_progress (singleton INTEGER PRIMARY KEY CHECK(singleton=1), max_thread_count INTEGER NOT NULL DEFAULT 0);
CREATE INDEX idx_jobs_kind_status_retry_lease ON jobs(kind, status, retry_at, lease_until);
CREATE INDEX idx_stage1_outputs_source_updated_at ON stage1_outputs(source_updated_at DESC, thread_id DESC);`

func supportedMemoryMigrations() []memoryMigration {
	a, _ := hex.DecodeString("a1af50da50775a70f98da680006b7df4501956753ae98511ab727aa295915d9ae6f4190b4eaa5abc4fd7891642f7d21d")
	b, _ := hex.DecodeString("18f0a8dd7fe9a847b30d719029066d7a78e0bc64310dc66e4f7708bad6f1a0a594c0dbd14fec1ccb410cd7ae75dd7b10")
	return []memoryMigration{{1, "memories", a}, {2, "consolidation progress", b}}
}

func compactMemorySQL(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSuffix(strings.TrimSpace(s), ";"))
}

type memoryQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func memoryDB(path string, readOnly bool) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	if readOnly {
		q.Set("mode", "ro")
	}
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func memoryLayout(ctx context.Context, q memoryQuery) ([]memoryMigration, error) {
	// Validate every native table and index, not just the exported columns. Never
	// claim unknown migrations were installed in a synthesized destination DB.
	expected := map[string]bool{}
	for _, ddl := range strings.Split(memorySchema, ";") {
		if normalized := compactMemorySQL(ddl); normalized != "" {
			expected[normalized] = true
		}
	}
	rows, err := q.QueryContext(ctx, "SELECT sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND sql IS NOT NULL")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ddl string
		if err = rows.Scan(&ddl); err != nil {
			rows.Close()
			return nil, err
		}
		key := compactMemorySQL(ddl)
		if !expected[key] {
			rows.Close()
			return nil, fmt.Errorf("this Codex memory database needs a newer Mindwire portability adapter")
		}
		delete(expected, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(expected) != 0 {
		return nil, fmt.Errorf("this Codex memory database needs a newer Mindwire portability adapter")
	}
	rows, err = q.QueryContext(ctx, "SELECT version,description,checksum,success FROM _sqlx_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memoryMigration{}
	for rows.Next() {
		var m memoryMigration
		var success bool
		if err = rows.Scan(&m.Version, &m.Description, &m.Checksum, &success); err != nil {
			return nil, err
		}
		if !success {
			return nil, fmt.Errorf("Codex memory migration is incomplete")
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(out, supportedMemoryMigrations()) {
		return nil, fmt.Errorf("this Codex memory migration version needs a newer Mindwire portability adapter")
	}
	return out, nil
}
func memoryRow(ctx context.Context, q memoryQuery, sid string) ([]byte, error) {
	var m portableMemory
	m.Version = 1
	m.ThreadID = sid
	err := q.QueryRowContext(ctx, "SELECT source_updated_at,raw_memory,rollout_summary,rollout_slug,generated_at FROM stage1_outputs WHERE thread_id=?", sid).Scan(&m.SourceUpdatedAt, &m.RawMemory, &m.RolloutSummary, &m.RolloutSlug, &m.GeneratedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Migrations, err = memoryLayout(ctx, q)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}
func (adapter) ValidSessionDataName(name string) bool {
	return strings.HasPrefix(name, "memory/") && strings.HasSuffix(name, ".json") && agent.ValidNativeSessionID(strings.TrimSuffix(strings.TrimPrefix(name, "memory/"), ".json"))
}
func (a adapter) ReadSessionData(ctx context.Context, cwd, sid, name string) ([]byte, error) {
	if !a.ValidSessionDataName(name) {
		return nil, fmt.Errorf("invalid Codex memory artifact")
	}
	sid = strings.TrimSuffix(strings.TrimPrefix(name, "memory/"), ".json")
	if err := checkOtherMemoryStores(ctx, sid); err != nil {
		return nil, err
	}
	path := filepath.Join(configBase(), "memories_1.sqlite")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	db, err := memoryDB(path, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Empty/new databases may not have had native migrations run yet.
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='stage1_outputs'").Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		var tables int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&tables); err != nil {
			return nil, err
		}
		if tables == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("unsupported Codex memory database; no context was omitted")
	}
	if _, err = memoryLayout(ctx, tx); err != nil {
		return nil, err
	}
	return memoryRow(ctx, tx, sid)
}
func (a adapter) ValidateSessionData(ctx context.Context, cwd, sid, name string, data []byte) error {
	if !a.ValidSessionDataName(name) {
		return fmt.Errorf("invalid Codex memory artifact")
	}
	var desired portableMemory
	if err := json.Unmarshal(data, &desired); err != nil {
		return err
	}
	if desired.Version != 1 || desired.ThreadID != strings.TrimSuffix(strings.TrimPrefix(name, "memory/"), ".json") || !reflect.DeepEqual(desired.Migrations, supportedMemoryMigrations()) {
		return fmt.Errorf("invalid Codex memory version or identity")
	}
	path := filepath.Join(configBase(), "memories_1.sqlite")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	db, err := memoryDB(path, true)
	if err != nil {
		return err
	}
	defer db.Close()
	var exists int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='stage1_outputs'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		var tables int
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&tables); err != nil {
			return err
		}
		if tables == 0 {
			return nil
		}
		return fmt.Errorf("incompatible destination Codex memory schema")
	}
	migrations, err := memoryLayout(ctx, db)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(migrations, desired.Migrations) {
		return fmt.Errorf("Codex memory schema versions differ; update to compatible harness versions before synchronizing")
	}
	return nil
}
func (a adapter) ApplySessionData(ctx context.Context, cwd, sid, name string, before, after []byte) error {
	if !a.ValidSessionDataName(name) || len(after) == 0 {
		return fmt.Errorf("Codex memory records cannot be removed by synchronization")
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, "memory/"), ".json")
	var desired portableMemory
	if err := json.Unmarshal(after, &desired); err != nil {
		return err
	}
	if desired.Version != 1 || desired.ThreadID != id || !reflect.DeepEqual(desired.Migrations, supportedMemoryMigrations()) {
		return fmt.Errorf("invalid Codex memory record")
	}
	canonical, err := json.Marshal(desired)
	if err != nil {
		return err
	}
	var prior portableMemory
	var expected []byte
	if len(before) > 0 {
		if err = json.Unmarshal(before, &prior); err != nil {
			return err
		}
		expected, _ = json.Marshal(prior)
	}
	path := filepath.Join(configBase(), "memories_1.sqlite")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	f.Close()
	db, err := memoryDB(path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='stage1_outputs'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		var tables int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&tables); err != nil {
			return err
		}
		if tables != 0 {
			return fmt.Errorf("incompatible destination Codex memory schema")
		}
		if _, err = tx.ExecContext(ctx, memorySchema); err != nil {
			return err
		}
		for _, m := range desired.Migrations {
			if _, err = tx.ExecContext(ctx, "INSERT INTO _sqlx_migrations(version,description,success,checksum,execution_time) VALUES(?,?,1,?,0)", m.Version, m.Description, m.Checksum); err != nil {
				return err
			}
		}
	}
	migrations, err := memoryLayout(ctx, tx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(migrations, desired.Migrations) {
		return fmt.Errorf("Codex memory schema versions differ; use compatible harness versions before synchronizing")
	}
	current, err := memoryRow(ctx, tx, id)
	if err != nil {
		return err
	}
	if bytes.Equal(current, canonical) {
		return nil
	}
	if !bytes.Equal(current, expected) {
		return fmt.Errorf("Codex memory changed independently; both checkpoints are retained")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO stage1_outputs(thread_id,source_updated_at,raw_memory,rollout_summary,rollout_slug,generated_at) VALUES(?,?,?,?,?,?)
ON CONFLICT(thread_id) DO UPDATE SET source_updated_at=excluded.source_updated_at,raw_memory=excluded.raw_memory,rollout_summary=excluded.rollout_summary,rollout_slug=excluded.rollout_slug,generated_at=excluded.generated_at,selected_for_phase2=0,selected_for_phase2_source_updated_at=NULL`, id, desired.SourceUpdatedAt, desired.RawMemory, desired.RolloutSummary, desired.RolloutSlug, desired.GeneratedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func checkOtherMemoryStores(ctx context.Context, sid string) error {
	entries, err := os.ReadDir(configBase())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sqlite") || name == "memories_1.sqlite" {
			continue
		}
		if strings.HasPrefix(name, "memor") {
			return fmt.Errorf("unsupported Codex memory store %s; update Mindwire before synchronizing", name)
		}
		if !strings.HasPrefix(name, "state_") {
			continue
		}
		db, err := memoryDB(filepath.Join(configBase(), name), true)
		if err != nil {
			return err
		}
		var exists int
		err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='stage1_outputs'").Scan(&exists)
		if err == nil && exists != 0 {
			err = db.QueryRowContext(ctx, "SELECT count(*) FROM stage1_outputs WHERE thread_id=?", sid).Scan(&exists)
			if err == nil && exists != 0 {
				err = fmt.Errorf("selected conversation uses legacy Codex memory in %s; update Mindwire before synchronizing", name)
			}
		}
		db.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
