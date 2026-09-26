package agent

import "context"

// SessionPorter describes selected native files; the shared sync engine owns
// hashing, transport, merging and recoverable installation for every harness.
// It never exports an entire home or replaces a harness database/configuration.
type SessionPorter interface {
	ExportSession(context.Context, string, string) (NativeArchive, error) // cwd, native ID
	ImportSessionPath(cwd, sessionID, name string) (string, error)
}

type NativeArchive struct {
	Version   int          `json:"version"`
	Harness   string       `json:"harness"`
	SessionID string       `json:"sessionId"`
	Files     []NativeFile `json:"-"`
}

type NativeFile struct {
	Name      string // adapter-relative, validated again by ImportSessionPath
	Path      string
	JSONLines bool // normalize only structured cwd fields; preserve unknown records
	Resource  bool // selected native database record, applied through SessionDataPorter
	Data      []byte
}

// SessionDataPorter transports selected database-backed context without copying
// an entire database over the destination's unrelated sessions or credentials.
type SessionDataPorter interface {
	ValidSessionDataName(string) bool
	ReadSessionData(context.Context, string, string, string) ([]byte, error)
	ValidateSessionData(context.Context, string, string, string, []byte) error
	ApplySessionData(context.Context, string, string, string, []byte, []byte) error
}
