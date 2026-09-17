package surface

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const MCPName = "mindwire_desktop"
const runTokenEnv = "MINDWIRE_DESKTOP_RUN_TOKEN"

// A separate loopback listener never accepts the daemon's owner credential. A
// token identifies exactly one live run; it conveys no provider or SSH secrets.
type runIPC struct {
	mu     sync.Mutex
	server *http.Server
	url    string
	runs   map[string]http.Handler
}

func (p *runIPC) close() { _ = p.server.Close() }

// BindRun returns a per-turn, harness-native MCP overlay and private environment.
// Existing MCP servers are preserved; the built-in name cannot be shadowed.
func (s *Service) BindRun(ctx context.Context, actor Actor, harness string, existing json.RawMessage) (json.RawMessage, map[string]string, func(), error) {
	if harness != "codex" && harness != "claude-code" {
		return existing, nil, func() {}, nil
	}
	s.mu.Lock()
	if s.ipc == nil {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.mu.Unlock()
			return nil, nil, nil, err
		}
		p := &runIPC{url: "http://" + listener.Addr().String() + "/mcp", runs: map[string]http.Handler{}}
		p.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/mcp" || r.Header.Get("Origin") != "" {
				http.Error(w, "forbidden", 403)
				return
			}
			authorization := r.Header.Get("Authorization")
			if !strings.HasPrefix(authorization, "Bearer ") {
				http.Error(w, "unauthorized", 401)
				return
			}
			token := strings.TrimPrefix(authorization, "Bearer ")
			p.mu.Lock()
			handler := p.runs[token]
			p.mu.Unlock()
			if handler == nil {
				http.Error(w, "unauthorized", 401)
				return
			}
			handler.ServeHTTP(w, r)
		})}
		s.ipc = p
		go func() { _ = p.server.Serve(listener) }()
	}
	p := s.ipc
	s.mu.Unlock()
	server := s.mcpServer(ctx, actor)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 8 << 20, PropagateRequestCancellation: true})
	token := newID() + newID()
	p.mu.Lock()
	p.runs[token] = handler
	p.mu.Unlock()
	var once sync.Once
	cleanup := func() { once.Do(func() { p.mu.Lock(); delete(p.runs, token); p.mu.Unlock(); s.CloseRun(actor.RunID) }) }
	stop := context.AfterFunc(ctx, cleanup)
	finish := func() { stop(); cleanup() }
	servers := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &servers); err != nil {
			finish()
			return nil, nil, nil, err
		}
		if wrapped, ok := servers["mcpServers"]; ok {
			if err := json.Unmarshal(wrapped, &servers); err != nil {
				finish()
				return nil, nil, nil, err
			}
			delete(servers, "mcpServers")
		}
	}
	// This private server enforces access itself through Service.Open and the
	// shared approval reducer. Avoid a second, harness-owned prompt for each tool.
	// https://developers.openai.com/codex/mcp/#other-configuration-options
	var config any = map[string]any{"url": p.url, "bearerTokenEnvVar": runTokenEnv, "defaultToolsApprovalMode": "approve"}
	if harness == "claude-code" {
		config = map[string]any{"type": "http", "url": p.url, "headers": map[string]string{"Authorization": "Bearer ${" + runTokenEnv + "}"}}
	}
	servers[MCPName], _ = json.Marshal(config)
	overlay, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		finish()
		return nil, nil, nil, err
	}
	return overlay, map[string]string{runTokenEnv: token}, finish, nil
}

// PermissionSettings allows only these service-owned tools to reach the desktop
// permission gate. Existing Claude settings and explicit deny rules are retained.
// No shell, file, or third-party MCP permission is changed.
func PermissionSettings(harness string, existing json.RawMessage) (json.RawMessage, error) {
	if harness != "claude-code" {
		return existing, nil
	}
	settings := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &settings); err != nil {
			return nil, err
		}
		if settings == nil {
			settings = map[string]json.RawMessage{}
		}
	}
	permissions := map[string]json.RawMessage{}
	if raw := settings["permissions"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &permissions); err != nil {
			return nil, err
		}
		if permissions == nil {
			permissions = map[string]json.RawMessage{}
		}
	}
	var allow []string
	if raw := permissions["allow"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &allow); err != nil {
			return nil, err
		}
	}
	for _, tool := range []string{"surface_status", "surface_open", "surface_capture", "surface_action", "surface_release"} {
		rule := "mcp__" + MCPName + "__" + tool
		found := false
		for _, existing := range allow {
			found = found || existing == rule
		}
		if !found {
			allow = append(allow, rule)
		}
	}
	permissions["allow"], _ = json.Marshal(allow)
	settings["permissions"], _ = json.Marshal(permissions)
	return json.Marshal(settings)
}

type sessionInput struct {
	SessionID string `json:"sessionId"`
}
type toolPayload struct {
	SurfaceID string    `json:"surfaceId"`
	Operation string    `json:"operation"`
	Session   *Session  `json:"session,omitempty"`
	Snapshot  *Snapshot `json:"snapshot,omitempty"`
	Capture   *Capture  `json:"capture,omitempty"`
	Receipt   *Receipt  `json:"receipt,omitempty"`
	Error     *Error    `json:"error,omitempty"`
}

func toolResult(payload toolPayload, image []byte, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		payload.Error = asError(err)
	}
	wrapped := map[string]any{"mindwireSurface": payload}
	encoded, _ := json.Marshal(wrapped)
	// Codex prefers structuredContent over content, which drops images when both
	// are present. The canonical envelope stays in text content alongside the PNG
	// so both native harnesses receive the same metadata and actual image.
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, IsError: err != nil}
	if image != nil {
		result.Content = append(result.Content, &mcp.ImageContent{MIMEType: "image/png", Data: image})
	}
	return result, nil, nil
}

func (s *Service) mcpServer(run context.Context, actor Actor) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: MCPName, Version: "1"}, nil)
	// Bind cancellation to both the RPC and the owning run. The HTTP session may
	// outlive a disconnected tool request, but it may never outlive this run.
	operation := func(ctx context.Context) (context.Context, func()) {
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(run, cancel)
		if run.Err() != nil {
			cancel()
		}
		return ctx, func() { stop(); cancel() }
	}
	mcp.AddTool(server, &mcp.Tool{Name: "surface_status", Description: "Check the shared workspace desktop and its current controller. No desktop access is enabled automatically."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		ctx, cancel := operation(ctx)
		defer cancel()
		snapshot, err := s.Refresh(ctx)
		return toolResult(toolPayload{SurfaceID: DesktopID, Operation: "status", Snapshot: &snapshot}, nil, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "surface_open", Description: "Open the shared desktop in view or control mode. Requests user approval once for this session. A controller is exclusive within Mindwire. Use a stable requestId. Never bypass a busy or revoked controller."}, func(ctx context.Context, _ *mcp.CallToolRequest, in OpenRequest) (*mcp.CallToolResult, any, error) {
		ctx, cancel := operation(ctx)
		defer cancel()
		session, err := s.Open(ctx, actor, in)
		return toolResult(toolPayload{SurfaceID: DesktopID, Operation: "open", Session: &session}, nil, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "surface_capture", Description: "Observe the desktop as an image. Returns a frame ID, dimensions and a durable artifact. Capture again after input; pointer actions require a frame younger than 30 seconds."}, func(ctx context.Context, _ *mcp.CallToolRequest, in sessionInput) (*mcp.CallToolResult, any, error) {
		ctx, cancel := operation(ctx)
		defer cancel()
		capture, err := s.Capture(ctx, in.SessionID, actor)
		var data []byte
		if err == nil {
			content, e := s.artifacts.Get(capture.ArtifactID)
			err = e
			if err == nil {
				data = content.Data
			}
		}
		return toolResult(toolPayload{SurfaceID: DesktopID, Operation: "capture", Capture: &capture}, data, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "surface_action", Description: "Send desktop input through the shared controller. Use the controlGeneration from surface_open, frameId from surface_capture, and a unique stable requestId. Coordinates are pixels in that image. Kinds: click, drag, scroll, key (e.g. [Meta,l]), text, clipboard_read/write, pointer, release. A dispatched receipt confirms delivery to VNC, not the application's result; capture to verify. Never repeat an outcome_unknown action blindly."}, func(ctx context.Context, _ *mcp.CallToolRequest, in ActionRequest) (*mcp.CallToolResult, any, error) {
		ctx, cancel := operation(ctx)
		defer cancel()
		receipt, err := s.Apply(ctx, actor, in)
		if err == nil && receipt.Error != nil {
			err = receipt.Error
		}
		return toolResult(toolPayload{SurfaceID: DesktopID, Operation: "action", Receipt: &receipt}, nil, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "surface_release", Description: "Close this run's desktop session and release all held input. Does not disable the workspace display or close another user's viewer."}, func(ctx context.Context, _ *mcp.CallToolRequest, in sessionInput) (*mcp.CallToolResult, any, error) {
		return toolResult(toolPayload{SurfaceID: DesktopID, Operation: "release"}, nil, s.CloseSession(in.SessionID, actor))
	})
	return server
}
