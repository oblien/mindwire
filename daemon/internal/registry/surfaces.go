package registry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Surface records share the workspace database and its lifetime. Secrets remain in
// the credential store; live control leases are deliberately never persisted.
func (st *Store) SurfaceGet(kind, id string, out any) error {
	var data []byte
	if err := st.db.QueryRow("SELECT data FROM surface_records WHERE kind=? AND id=?", kind, id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return json.Unmarshal(data, out)
}

func (st *Store) SurfacePut(kind, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = st.db.Exec("INSERT INTO surface_records(kind,id,data,updated_at) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at", kind, id, data, operationNow())
	return err
}

func (st *Store) SurfaceDelete(kind, id string) error {
	_, err := st.db.Exec("DELETE FROM surface_records WHERE kind=? AND id=?", kind, id)
	return err
}

func (st *Store) SurfaceList(kind string) ([]json.RawMessage, error) {
	rows, err := st.db.Query("SELECT data FROM surface_records WHERE kind=? ORDER BY updated_at", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var data json.RawMessage
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, rows.Err()
}

func (st *Store) Identity() string { return st.identity }

// Prune only transient control metadata. Chat image retention is independent.
func (st *Store) SurfacePruneBefore(kind string, before time.Time) error {
	_, err := st.db.Exec("DELETE FROM surface_records WHERE kind=? AND updated_at < ?", kind, before.UTC().Format(time.RFC3339Nano))
	return err
}
func (st *Store) SurfaceCount(kind string) (int, error) {
	var count int
	err := st.db.QueryRow("SELECT count(*) FROM surface_records WHERE kind=?", kind).Scan(&count)
	return count, err
}
