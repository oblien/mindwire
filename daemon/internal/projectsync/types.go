// Package projectsync synchronizes preserved project replicas. Immutable,
// content-addressed checkpoints are the common base; no source is ever moved,
// reset, or deleted. Imports use a durable journal and the registry's path gates.
package projectsync

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"time"

	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
)

const Version = 1
const ChunkSize = 256 << 10
const maxManifest = 64 << 20
const maxEntries = 250000

// Directory fsync and native file replacement currently have end-to-end support
// on macOS and Linux. Other hosts must not advertise a durability guarantee.
func ProtocolVersion() int {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		return Version
	}
	return 0
}

type Entry struct {
	Kind      string   `json:"kind"` // file, directory, symlink
	Mode      uint32   `json:"mode"`
	Size      int64    `json:"size"`
	Chunks    []string `json:"chunks,omitempty"`
	Link      string   `json:"link,omitempty"`
	JSONLines bool     `json:"jsonLines,omitempty"`
}

type Chat struct {
	ID             string   `json:"id"` // shared logical chat ID, never a workspace-local cache ID
	Harness        string   `json:"harness"`
	ProfileName    string   `json:"profileName"`
	Title          string   `json:"title"`
	NativeTitle    string   `json:"nativeTitle,omitempty"`
	TitleIsUserSet bool     `json:"titleIsUserSet,omitempty"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt,omitempty"`
	SessionID      string   `json:"sessionId,omitempty"`
	ArchiveVersion int      `json:"archiveVersion"`
	NativeFiles    []string `json:"nativeFiles,omitempty"` // keys in Manifest.Entries
}

type Git struct {
	Head   string            `json:"head"` // symbolic ref or detached object ID
	Refs   map[string]string `json:"refs"`
	Index  map[string]Entry  `json:"index"`            // staged blobs, independently of working files
	Bundle *Entry            `json:"bundle,omitempty"` // reachable local commits, including unpushed work
}

type Manifest struct {
	Version   int                        `json:"version"`
	ProjectID string                     `json:"projectId"` // logical identity
	Name      string                     `json:"name"`
	RepoURL   string                     `json:"repoUrl,omitempty"`
	IconPath  *string                    `json:"iconPath,omitempty"`
	CreatedAt string                     `json:"createdAt"`
	Parents   []string                   `json:"parents"`
	Entries   map[string]Entry           `json:"entries"` // files/, native/<harness>/, records/<logical chat>
	Chats     map[string]Chat            `json:"chats"`
	Artifacts map[string]artifact.Record `json:"artifacts,omitempty"`
	Git       *Git                       `json:"git,omitempty"`
}

type Checkpoint struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"projectId"`
	Parents     []string `json:"parents"`
	Manifest    Entry    `json:"manifest"`
	ObjectCount int      `json:"objectCount"`
	Bytes       int64    `json:"bytes"`
}

type Request struct {
	ID           string `json:"id"`                     // idempotency key, retained across retries/disconnects
	ProjectID    string `json:"projectId,omitempty"`    // local project, for export
	CheckpointID string `json:"checkpointId,omitempty"` // incoming checkpoint, for import
	Path         string `json:"path,omitempty"`         // needed only when this workspace has no replica
}

type Conflict struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"` // file, conversation, git, recovery
	Message string `json:"message"`
}

type Operation struct {
	Request
	Kind                 string     `json:"kind"`
	Status               string     `json:"status"`
	Phase                string     `json:"phase"`
	Progress             float64    `json:"progress"`
	ResultProjectID      string     `json:"resultProjectId,omitempty"`
	ResultCheckpointID   string     `json:"resultCheckpointId,omitempty"`
	RecoveryCheckpointID string     `json:"recoveryCheckpointId,omitempty"`
	Conflicts            []Conflict `json:"conflicts,omitempty"`
	Error                string     `json:"error,omitempty"`
	CreatedAt            string     `json:"createdAt"`
	UpdatedAt            string     `json:"updatedAt"`
}

func (o Operation) Active() bool {
	switch o.Status {
	case "queued", "exporting", "preparing", "applying":
		return true
	}
	return false
}
func now() string                  { return time.Now().UTC().Format(time.RFC3339Nano) }
func invalid(message string) error { return fmt.Errorf("%w: %s", registry.ErrInvalid, message) }
func equal(a, b any) bool          { return reflect.DeepEqual(a, b) }
func clone[T any](v T) T           { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }

type ObjectPage struct {
	Objects []string `json:"objects"`
	Next    int      `json:"next,omitempty"`
}

// Recovery journal owns only changed paths. Expected and desired content are
// immutable objects, so interrupted writes are replayable without a hard reset.
type Write struct {
	Path     string    `json:"path"`
	Before   *Entry    `json:"before,omitempty"`
	After    *Entry    `json:"after,omitempty"`
	CWD      string    `json:"cwd,omitempty"`
	Private  bool      `json:"private,omitempty"`
	Resource *Resource `json:"resource,omitempty"`
}

type Resource struct {
	Harness   string `json:"harness"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
}
type journal struct {
	Writes        []Write                         `json:"writes"`
	Batch         registry.Import                 `json:"batch"`
	Incoming      string                          `json:"incoming"`
	Result        string                          `json:"result"`
	Path          string                          `json:"path"`
	GitBefore     *Git                            `json:"gitBefore,omitempty"`
	GitAfter      *Git                            `json:"gitAfter,omitempty"`
	GitDone       bool                            `json:"gitDone"`
	Artifacts     map[string]json.RawMessage      `json:"artifacts,omitempty"`
	ExpectedChats map[string]session.PortableChat `json:"expectedChats,omitempty"`
}
