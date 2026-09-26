package computer

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"
)

// Start exposes only the SSH protocol. The daemon HTTP API stays on loopback.
// WebSocket is an alternative byte carrier for the same pinned SSH handshake.
func (s *Server) Start(sshAddress, websocketAddress, directory string) error {
	var err error
	s.sshListener, err = net.Listen("tcp", sshAddress)
	if err != nil {
		return err
	}
	s.wsListener, err = net.Listen("tcp", websocketAddress)
	if err != nil {
		s.sshListener.Close()
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ssh", s.WebSocket)
	s.wsServer = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() { _ = s.wsServer.Serve(s.wsListener) }()
	go func() {
		for {
			conn, err := s.sshListener.Accept()
			if err != nil {
				return
			}
			go s.Handle(conn)
		}
	}()
	if err = s.bootstrap(directory); err != nil {
		s.Close()
		return err
	}
	return nil
}

func (s *Server) WebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	s.Handle(websocket.NetConn(r.Context(), conn, websocket.MessageBinary))
}

func (s *Server) Handle(raw net.Conn) {
	defer raw.Close()
	select {
	case s.limit <- struct{}{}:
		defer func() { <-s.limit }()
	default:
		return
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	config := &ssh.ServerConfig{MaxAuthTries: 3, ServerVersion: "SSH-2.0-Mindwire"}
	config.AddHostKey(s.signer)
	config.PasswordCallback = func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		id := strings.TrimPrefix(meta.User(), "pair:")
		if !strings.HasPrefix(meta.User(), "pair:") || !s.authenticatePair(id, password) {
			return nil, errors.New("pairing denied")
		}
		return &ssh.Permissions{Extensions: map[string]string{"mode": "pair", "offer": id}}, nil
	}
	config.PublicKeyCallback = func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		id := keyID(key)
		s.mu.Lock()
		device, exists := s.state.Devices[id]
		s.mu.Unlock()
		if !exists || device.Revoked {
			return nil, errors.New("device denied")
		}
		return &ssh.Permissions{Extensions: map[string]string{"mode": "device", "device": id}}, nil
	}
	_ = raw.SetDeadline(time.Now().Add(20 * time.Second))
	conn, channels, requests, err := ssh.NewServerConn(raw, config)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = raw.SetDeadline(time.Time{})
	deviceID := conn.Permissions.Extensions["device"]
	s.mu.Lock()
	if conn.Permissions.Extensions["mode"] == "pair" {
		offer, err := s.offer(conn.Permissions.Extensions["offer"])
		if err != nil || offer.Status == "rejected" {
			s.mu.Unlock()
			return
		}
		// Expiry bounds already-authenticated pairing sessions as well as future
		// handshakes. They must not occupy connection slots indefinitely.
		_ = raw.SetDeadline(offer.ExpiresAt)
	}
	device := s.state.Devices[deviceID]
	if s.closed || deviceID != "" && (device.ID == "" || device.Revoked) {
		s.mu.Unlock()
		return
	}
	s.connections[conn] = deviceID
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
	go func() {
		for req := range requests {
			if req.WantReply {
				_ = req.Reply(req.Type == "keepalive@openssh.com", nil)
			}
		}
	}()
	channelLimit := make(chan struct{}, 32)
	for incoming := range channels {
		select {
		case channelLimit <- struct{}{}:
			go func() { defer func() { <-channelLimit }(); s.forward(conn, incoming) }()
		default:
			_ = incoming.Reject(ssh.ResourceShortage, "too many channels")
		}
	}
}

func (s *Server) forward(conn *ssh.ServerConn, incoming ssh.NewChannel) {
	if incoming.ChannelType() != "direct-tcpip" {
		_ = incoming.Reject(ssh.UnknownChannelType, "use the workspace API")
		return
	}
	var target struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	if ssh.Unmarshal(incoming.ExtraData(), &target) != nil || target.Host != "127.0.0.1" {
		_ = incoming.Reject(ssh.Prohibited, "target not allowed")
		return
	}
	pairing := conn.Permissions.Extensions["mode"] == "pair"
	var revoked <-chan struct{}
	address := s.apiAddress
	if !pairing && target.Port != APIPort {
		var allowed bool
		revoked, allowed = s.forwardedPort(conn.Permissions.Extensions["device"], target.Port)
		if !allowed {
			_ = incoming.Reject(ssh.Prohibited, "authorize this port through the workspace API first")
			return
		}
		address = net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(target.Port), 10))
	}
	if pairing && target.Port != PairingPort {
		_ = incoming.Reject(ssh.Prohibited, "target not allowed")
		return
	}
	if pairing {
		s.mu.Lock()
		_, err := s.offer(conn.Permissions.Extensions["offer"])
		s.mu.Unlock()
		if err != nil {
			_ = incoming.Reject(ssh.Prohibited, "pairing expired")
			return
		}
	}
	var local net.Conn
	var err error
	if !pairing {
		local, err = net.DialTimeout("tcp", address, 5*time.Second)
		if err != nil {
			_ = incoming.Reject(ssh.ConnectionFailed, "workspace unavailable")
			return
		}
	}
	channel, requests, err := incoming.Accept()
	if err != nil {
		if local != nil {
			local.Close()
		}
		return
	}
	defer channel.Close()
	go ssh.DiscardRequests(requests)
	if pairing {
		client, server := net.Pipe()
		local = client
		listener := &singleListener{conn: server, done: make(chan struct{})}
		httpServer := &http.Server{Handler: s.PairHandler(conn.Permissions.Extensions["offer"]), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
		go func() { _ = httpServer.Serve(listener) }()
		defer httpServer.Close()
		defer listener.Close()
	}
	defer local.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(channel, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, channel); done <- struct{}{} }()
	select {
	case <-done:
	case <-revoked:
		_ = channel.Close()
		_ = local.Close()
		<-done
	}
	_ = channel.Close()
	_ = local.Close()
	<-done
}

func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	for id := range s.forwards {
		s.closeForwardLocked(id)
	}
	connections := []*ssh.ServerConn{}
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.mu.Unlock()
	if s.sshListener != nil {
		_ = s.sshListener.Close()
	}
	if s.wsServer != nil {
		_ = s.wsServer.Close()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
}

type singleListener struct {
	mu   sync.Mutex
	conn net.Conn
	done chan struct{}
	once sync.Once
}

func (l *singleListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	c := l.conn
	l.conn = nil
	l.mu.Unlock()
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *singleListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *singleListener) Addr() net.Addr { return localAddress("mindwire-pair") }

type localAddress string

func (a localAddress) Network() string { return "ssh" }
func (a localAddress) String() string  { return string(a) }
