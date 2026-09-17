// Package surface is the workspace's shared desktop control service. HTTP clients
// and harness MCP tools use the same sessions, controller and input executor.
package surface

import (
	"context"
	"image"
	"time"
)

const Version = 1
const DesktopID = "desktop"
const MaxTextBytes = 1 << 20

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string           { return e.Message }
func problem(code, message string) error { return &Error{Code: code, Message: message} }

type Capabilities struct {
	View           bool `json:"view"`
	Capture        bool `json:"capture"`
	Pointer        bool `json:"pointer"`
	Keyboard       bool `json:"keyboard"`
	Text           bool `json:"text"`
	ClipboardRead  bool `json:"clipboardRead"`
	ClipboardWrite bool `json:"clipboardWrite"`
}

type ProviderStatus struct {
	Supported    bool         `json:"supported"`
	Enabled      bool         `json:"enabled"`
	Available    bool         `json:"available"`
	Credentials  bool         `json:"credentials"`
	OS           string       `json:"os,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
}

type Geometry struct {
	Width    int   `json:"width"`
	Height   int   `json:"height"`
	Revision int64 `json:"revision"`
}

type Controller struct {
	SessionID  string    `json:"sessionId"`
	Actor      string    `json:"actor"`
	Name       string    `json:"name"`
	RunID      string    `json:"runId,omitempty"`
	ChatID     string    `json:"chatId,omitempty"`
	Generation int64     `json:"generation"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type Snapshot struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	Kind        string     `json:"kind"`
	Provider    string     `json:"provider"`
	Version     int        `json:"version"`
	Revision    int64      `json:"revision"`
	InstanceID  string     `json:"instanceId"`
	State       string     `json:"state"`
	ObservedAt  *time.Time `json:"observedAt,omitempty"`
	ProviderStatus
	Geometry               *Geometry   `json:"geometry,omitempty"`
	Controller             *Controller `json:"controller,omitempty"`
	AuthorizationExpiresAt *time.Time  `json:"authorizationExpiresAt,omitempty"`
	Error                  *Error      `json:"error,omitempty"`
}

type Session struct {
	ID         string      `json:"id"`
	SurfaceID  string      `json:"surfaceId"`
	Actor      string      `json:"actor"`
	Name       string      `json:"name"`
	ChatID     string      `json:"chatId,omitempty"`
	RunID      string      `json:"runId,omitempty"`
	Mode       string      `json:"mode"`
	CreatedAt  time.Time   `json:"createdAt"`
	Controller *Controller `json:"controller,omitempty"`
}

type OpenRequest struct {
	RequestID string `json:"requestId"`
	Name      string `json:"name,omitempty"`
	Mode      string `json:"mode"`
}

type ControlRequest struct {
	Action     string `json:"action"`
	Generation int64  `json:"generation,omitempty"`
}

type Action struct {
	Kind    string   `json:"kind" jsonschema:"pointer, click, drag, scroll, key, text, clipboard_read, clipboard_write, release"`
	X       *int     `json:"x,omitempty"`
	Y       *int     `json:"y,omitempty"`
	ToX     *int     `json:"toX,omitempty"`
	ToY     *int     `json:"toY,omitempty"`
	Buttons int      `json:"buttons,omitempty"`
	Button  string   `json:"button,omitempty"`
	Count   int      `json:"count,omitempty"`
	DeltaX  int      `json:"deltaX,omitempty"`
	DeltaY  int      `json:"deltaY,omitempty"`
	Keys    []string `json:"keys,omitempty"`
	Text    string   `json:"text,omitempty"`
}

type ActionRequest struct {
	RequestID         string `json:"requestId"`
	SessionID         string `json:"sessionId"`
	ControlGeneration int64  `json:"controlGeneration"`
	FrameID           string `json:"frameId,omitempty"`
	GeometryRevision  int64  `json:"geometryRevision,omitempty"`
	Action            Action `json:"action"`
}

type Receipt struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	SurfaceID string    `json:"surfaceId"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	Error     *Error    `json:"error,omitempty"`
	// Text is transient clipboard output; it is omitted from persisted receipts.
	Text string `json:"text,omitempty"`
}

type Capture struct {
	ID         string    `json:"id"`
	SurfaceID  string    `json:"surfaceId"`
	ArtifactID string    `json:"artifactId"`
	Mime       string    `json:"mime"`
	Geometry   Geometry  `json:"geometry"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Actor comes from authentication, not tool arguments.
type Actor struct {
	Kind   string
	Name   string
	RunID  string
	ChatID string
}

type Provider interface {
	Status(context.Context) (ProviderStatus, error)
	Connect(context.Context) (Geometry, error)
	Capture(context.Context) (image.Image, Geometry, error)
	Apply(context.Context, Action) (string, error)
	Release(context.Context) error
	Close() error
	ExpiresAt() time.Time
}

type Approval func(context.Context, Actor, string) error
