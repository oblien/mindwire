package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/oblien/mindwire/daemon/internal/agent"
)

var errNoSharedServer = errors.New("Codex shared app-server is unavailable")

// A private Unix socket keeps native control on the host account. The phone
// reaches only Mindwire's authenticated API through its existing tunnel.
func sharedSocketPath() (string, error) {
	path := os.Getenv("MINDWIRE_CODEX_SOCKET")
	if path == "" {
		if configBase() == "" {
			return "", errNoSharedServer
		}
		path = filepath.Join(configBase(), "app-server-control", "app-server-control.sock")
	}
	if !filepath.IsAbs(path) {
		return "", agent.NativeError("native_socket_invalid", "Codex's control socket must have an absolute path.")
	}
	info, err := os.Stat(filepath.Dir(path))
	if os.IsNotExist(err) {
		return "", errNoSharedServer
	}
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0077 != 0 {
		return "", agent.NativeError("native_socket_permissions", "Codex's control socket directory must be private to your account (mode 700).")
	}
	// Codex publishes its control address as an atomic symlink to a private
	// generation socket. Validate both directories, then dial that generation.
	path, err = filepath.EvalSymlinks(path)
	if os.IsNotExist(err) {
		return "", errNoSharedServer
	}
	if err != nil {
		return "", err
	}
	info, err = os.Stat(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0077 != 0 {
		return "", agent.NativeError("native_socket_permissions", "Codex's control socket target must be private to your account (mode 700).")
	}
	info, err = os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return "", agent.NativeError("native_socket_invalid", "Codex's control endpoint is not a Unix socket.")
	}
	return path, nil
}

type sharedRPC struct {
	conn      *websocket.Conn
	transport *http.Transport
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	writeMu   sync.Mutex
	seq       int
	pending   map[string]chan sharedFrame
	frames    chan sharedFrame
	done      chan struct{}
}

type sharedFrame struct {
	rpcIn
	sequence uint64
}

func dialSharedRPC(ctx context.Context) (*sharedRPC, error) {
	path, err := sharedSocketPath()
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, "ws://localhost", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("%w: %v", errNoSharedServer, err)
	}
	conn.SetReadLimit(16 << 20)
	lifetime, stop := context.WithCancel(ctx)
	c := &sharedRPC{conn: conn, transport: transport, ctx: lifetime, cancel: stop, pending: map[string]chan sharedFrame{}, frames: make(chan sharedFrame, 256), done: make(chan struct{})}
	go c.read()
	initCtx, initCancel := context.WithTimeout(ctx, 5*time.Second)
	defer initCancel()
	if err = c.call(initCtx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "mindwire", "version": agent.Version}, "capabilities": map[string]any{"experimentalApi": true}}, nil); err == nil {
		err = c.write(initCtx, rpcNotification{Method: "initialized"})
	}
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *sharedRPC) read() {
	defer close(c.done)
	defer close(c.frames)
	defer c.cancel()
	var sequence uint64
	for {
		var msg rpcIn
		if err := wsjson.Read(c.ctx, c.conn, &msg); err != nil {
			return
		}
		sequence++
		frame := sharedFrame{rpcIn: msg, sequence: sequence}
		if msg.Method == "" && len(msg.ID) > 0 {
			c.mu.Lock()
			pending := c.pending[string(msg.ID)]
			delete(c.pending, string(msg.ID))
			c.mu.Unlock()
			if pending != nil {
				pending <- frame
			}
			continue
		}
		// Backpressure closes this viewer, never the shared server. Reattachment
		// reads a fresh native snapshot instead of silently dropping deltas.
		select {
		case c.frames <- frame:
		default:
			return
		}
	}
}

func (c *sharedRPC) write(ctx context.Context, value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return wsjson.Write(ctx, c.conn, value)
}

func (c *sharedRPC) call(ctx context.Context, method string, params, out any) error {
	_, err := c.callAt(ctx, method, params, out)
	return err
}

func (c *sharedRPC) callAt(ctx context.Context, method string, params, out any) (uint64, error) {
	c.mu.Lock()
	c.seq++
	id, _ := json.Marshal("mw-native-" + strconv.Itoa(c.seq))
	ch := make(chan sharedFrame, 1)
	c.pending[string(id)] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, string(id)); c.mu.Unlock() }()
	if err := c.write(ctx, rpcRequest{ID: id, Method: method, Params: params}); err != nil {
		return 0, err
	}
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-c.done:
		return 0, agent.NativeError("native_connection_lost", "Codex's shared connection closed. Reconnect to read the current session.")
	case msg := <-ch:
		if msg.Error != nil {
			code := "native_request_rejected"
			lower := strings.ToLower(msg.Error.Message)
			if (method == "turn/steer" || method == "turn/interrupt") && strings.Contains(lower, "no active turn") {
				code = "native_turn_ended"
			} else if strings.Contains(lower, "expected active turn id") {
				code = "native_turn_changed"
			} else if method == "thread/read" && (strings.Contains(lower, "not found") || strings.Contains(lower, "no rollout found")) {
				code = "native_session_not_found"
			}
			return msg.sequence, agent.NativeError(code, msg.Error.text())
		}
		if out != nil {
			return msg.sequence, json.Unmarshal(msg.Result, out)
		}
		return msg.sequence, nil
	}
}

func (c *sharedRPC) close() { c.cancel(); _ = c.conn.CloseNow(); c.transport.CloseIdleConnections() }
