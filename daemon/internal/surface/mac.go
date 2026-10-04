package surface

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	vnc "github.com/kward/go-vnc"
)

const localDesktopKey = "#surface:macos"

// Write-only configuration. It lives in the existing private credential store,
// never in workspace metadata, SDK status responses, or pairing invitations.
type LocalDesktopSettings struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type LocalDesktopInfo struct {
	Supported     bool   `json:"supported"`
	Enabled       bool   `json:"enabled"`
	ScreenSharing bool   `json:"screenSharing"`
	Username      string `json:"username"`
}

type savedLocalDesktop struct {
	RegistryID string `json:"registryId"`
	LocalDesktopSettings
}

type MacDesktop struct {
	mu           sync.Mutex
	settings     LocalDesktopSettings
	username     string
	client       *rfbClient
	inputDelay   time.Duration
	inputReadyAt time.Time
	dial         func(context.Context) (net.Conn, error)
	console      func(context.Context) bool
	clipboard    func(context.Context, *string) (string, error)
}

func newMacDesktop(settings LocalDesktopSettings) (*MacDesktop, error) {
	if runtime.GOOS != "darwin" || os.Geteuid() == 0 {
		return nil, problem("unsupported", "Desktop control requires Mindwire running as your signed-in Mac account.")
	}
	account, err := user.Current()
	if err != nil || account.Username == "" {
		return nil, problem("unavailable", "Mindwire could not identify this Mac account.")
	}
	if settings.Enabled && (settings.Username != account.Username || settings.Password == "" ||
		len(settings.Username) > 63 || len(settings.Password) > 63 || strings.ContainsRune(settings.Password, 0)) {
		return nil, problem("desktop_credentials", "Use the password for the Mac account running Mindwire (up to 63 UTF-8 bytes).")
	}
	p := &MacDesktop{settings: settings, username: account.Username, inputDelay: 3 * time.Second}
	p.dial = func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", "127.0.0.1:5900")
	}
	p.console = func(ctx context.Context) bool {
		output, err := exec.CommandContext(ctx, "/usr/bin/stat", "-f", "%u", "/dev/console").Output()
		return err == nil && strings.TrimSpace(string(output)) == strconv.Itoa(os.Geteuid())
	}
	p.clipboard = macClipboard
	return p, nil
}

func (p *MacDesktop) ExpiresAt() time.Time { return time.Time{} }

func (p *MacDesktop) Info(ctx context.Context) LocalDesktopInfo {
	info := LocalDesktopInfo{Supported: true, Enabled: p.settings.Enabled, Username: p.username}
	conn, err := p.dial(ctx)
	if err == nil {
		info.ScreenSharing = true
		_ = conn.Close()
	}
	return info
}

func (p *MacDesktop) Status(ctx context.Context) (ProviderStatus, error) {
	status := ProviderStatus{Supported: true, Enabled: p.settings.Enabled, OS: "darwin"}
	status.Capabilities = Capabilities{View: true, Capture: true, Pointer: true, Keyboard: true,
		Text: true, KeyboardText: true, ExtendedKeys: true}
	if !p.settings.Enabled {
		status.Setup = &DesktopSetup{Reason: "disabled", Command: "mindwire desktop enable"}
		return status, nil
	}
	if !p.Info(ctx).ScreenSharing {
		status.Setup = &DesktopSetup{Reason: "screen_sharing_off", Command: "mindwire desktop enable"}
		return status, nil
	}
	status.Available = true
	if p.console(ctx) {
		status.Capabilities.ClipboardRead = true
		status.Capabilities.ClipboardWrite = true
	}
	return status, nil
}

func (p *MacDesktop) authentication(conn net.Conn) []vnc.ClientAuth {
	return []vnc.ClientAuth{&appleDesktopAuth{conn: conn, username: p.settings.Username, password: p.settings.Password}}
}

type macRFBConnection struct {
	net.Conn
	prefix *bytes.Reader
}

func (c *macRFBConnection) Read(data []byte) (int, error) {
	if c.prefix.Len() > 0 {
		return c.prefix.Read(data)
	}
	return c.Conn.Read(data)
}

func (p *MacDesktop) dialAuthenticated(ctx context.Context) (net.Conn, error) {
	conn, err := p.dial(ctx)
	if err != nil {
		return nil, problem("screen_sharing_off", "Open mindwire desktop on your Mac to check Screen Sharing.")
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	var banner [12]byte
	_, err = io.ReadFull(conn, banner[:])
	_ = conn.SetReadDeadline(time.Time{})
	var major, minor int
	if err == nil {
		_, err = fmt.Sscanf(string(banner[:]), "RFB %d.%d\n", &major, &minor)
	}
	// go-vnc's old RFB 3.3 path chooses its own authentication. Never allow that
	// downgrade (or a password-only/None fallback) for the Mac provider.
	if err != nil || major != 3 || minor < 8 {
		_ = conn.Close()
		return nil, problem("desktop_auth_protocol", "Use macOS Screen Sharing with your Mac account, rather than password-only VNC access.")
	}
	if ctx.Err() != nil {
		_ = conn.Close()
		return nil, ctx.Err()
	}
	return &macRFBConnection{Conn: conn, prefix: bytes.NewReader(banner[:])}, nil
}

func (p *MacDesktop) Connect(ctx context.Context) (Geometry, error) {
	if !p.settings.Enabled {
		return Geometry{}, problem("desktop_setup_required", "Run mindwire desktop enable on your Mac to allow desktop access.")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil && p.client.alive() {
		return p.client.refreshGeometry(ctx)
	}
	if p.client != nil {
		p.client.close()
		p.client = nil
	}
	conn, err := p.dialAuthenticated(ctx)
	if err != nil {
		return Geometry{}, err
	}
	client, err := newRFBWithAuth(ctx, conn, p.authentication(conn))
	if err != nil {
		_ = conn.Close()
		return Geometry{}, macConnectionError(ctx, err)
	}
	p.client = client
	// Screen Sharing can deliver frames before its input session is ready. macOS
	// silently discards early events, so delay only initial input, not the video.
	p.inputReadyAt = time.Now().Add(p.inputDelay)
	return client.geometry(), nil
}

func macConnectionError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return problem("timeout", "The Mac's screen-sharing service did not respond. Try again.")
	}
	// Never propagate an authentication library's raw response into logs or chats.
	return problem("desktop_authentication", "Mac screen sharing could not authenticate. Run mindwire desktop enable on your Mac to check access and update its login.")
}

func (p *MacDesktop) Capture(ctx context.Context) (image.Image, Geometry, error) {
	if _, err := p.Connect(ctx); err != nil {
		return nil, Geometry{}, err
	}
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	return client.capture(ctx)
}

func (p *MacDesktop) PrepareControl(ctx context.Context) error {
	p.mu.Lock()
	client, readyAt := p.client, p.inputReadyAt
	p.mu.Unlock()
	if client == nil || !client.alive() {
		return problem("disconnected", "Reconnect the Mac display before taking control.")
	}
	return waitMacInput(ctx, client, readyAt)
}

func (p *MacDesktop) Apply(ctx context.Context, action Action) (string, error) {
	p.mu.Lock()
	client := p.client
	readyAt := p.inputReadyAt
	p.mu.Unlock()
	if client == nil || !client.alive() {
		return "", problem("disconnected", "The Mac display disconnected. Reconnect before sending more input.")
	}
	if action.Kind != "release" && action.Kind != "clipboard_read" && action.Kind != "clipboard_write" {
		if err := waitMacInput(ctx, client, readyAt); err != nil {
			return "", err
		}
	}
	switch action.Kind {
	case "clipboard_read", "clipboard_write":
		if !p.console(ctx) {
			return "", problem("login_required", "Sign in to your Mac desktop to use the clipboard.")
		}
		if action.Kind == "clipboard_read" {
			return p.clipboard(ctx, nil)
		}
		return p.clipboard(ctx, &action.Text)
	case "text":
		if action.TextMode == "keyboard" {
			// Apple Screen Sharing derives Shift from the character keysym. QEMU's
			// physical-US-key conversion would turn capitals into lowercase here.
			return "", client.typeText(ctx, action.Text, false)
		}
		if !p.console(ctx) {
			for _, c := range action.Text {
				if c < 32 || c > 126 {
					return "", problem("login_required", "Sign in to your Mac desktop before entering Unicode or multiline text.")
				}
			}
			return "", client.typeText(ctx, action.Text, false)
		}
		if _, err := p.clipboard(ctx, &action.Text); err != nil {
			return "", err
		}
		return "", client.chord(ctx, []string{"Meta", "v"})
	case "key":
		return "", client.chord(ctx, macKeyChord(action.Keys))
	default:
		return "", client.apply(ctx, action)
	}
}

func waitMacInput(ctx context.Context, client *rfbClient, readyAt time.Time) error {
	delay := time.Until(readyAt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-client.done:
		return problem("disconnected", "The Mac display disconnected before input was ready.")
	case <-timer.C:
		return nil
	}
}

func macKeyChord(names []string) []string {
	shift := false
	for _, name := range names {
		shift = shift || strings.EqualFold(name, "Shift")
	}
	if !shift {
		return names
	}
	out := append([]string(nil), names...)
	const lower = "`1234567890-=[]\\;',./"
	const upper = "~!@#$%^&*()_+{}|:\"<>?"
	for i, name := range out {
		if len(name) != 1 {
			continue
		}
		if at := strings.IndexByte(lower, name[0]); at >= 0 {
			out[i] = string(upper[at])
		} else {
			out[i] = strings.ToUpper(name)
		}
	}
	return out
}

func macClipboard(ctx context.Context, value *string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if value != nil {
		command := exec.CommandContext(ctx, "/usr/bin/pbcopy")
		command.Stdin = strings.NewReader(*value)
		if err := command.Run(); err != nil {
			return "", problem("clipboard_failed", "The Mac clipboard is unavailable in this login session.")
		}
		return "", nil
	}
	command := exec.CommandContext(ctx, "/usr/bin/pbpaste")
	reader, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = command.Start(); err != nil {
		return "", problem("clipboard_failed", "The Mac clipboard is unavailable in this login session.")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxTextBytes+1))
	if len(data) > MaxTextBytes {
		_ = command.Process.Kill()
	}
	wait := command.Wait()
	if len(data) > MaxTextBytes {
		return "", problem("text_limit", "The Mac clipboard is too large to copy.")
	}
	if err != nil || wait != nil {
		return "", problem("clipboard_failed", "The Mac clipboard could not be read.")
	}
	return string(bytes.ToValidUTF8(data, []byte("�"))), nil
}

func (p *MacDesktop) Release(ctx context.Context) error {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client != nil {
		return client.release(ctx)
	}
	return nil
}
func (p *MacDesktop) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		p.client.close()
		p.client = nil
	}
	return nil
}

// EnableLocalDesktop exposes only capability/setup status. It never turns on
// Screen Sharing or opts an owner into desktop access.
func (s *Service) EnableLocalDesktop(credentials Credentials) error {
	s.operation.Lock()
	defer s.operation.Unlock()
	if s.macFactory == nil {
		s.macFactory = newMacDesktop
	}
	settings := LocalDesktopSettings{}
	var setupError error
	if raw := credentials.Get(localDesktopKey); raw != "" {
		var saved savedLocalDesktop
		if json.Unmarshal([]byte(raw), &saved) != nil || saved.RegistryID != s.db.Identity() {
			setupError = problem("desktop_setup_required", "Run mindwire desktop enable to repair this Mac's desktop setup.")
		} else {
			settings = saved.LocalDesktopSettings
		}
	}
	p, err := s.macFactory(settings)
	if err != nil && settings.Enabled {
		setupError = err
		p, err = s.macFactory(LocalDesktopSettings{})
	}
	if err != nil {
		return err
	}
	s.configureProvider(p)
	if setupError != nil {
		s.mu.Lock()
		s.snapshot.Error = asError(setupError)
		s.mu.Unlock()
	}
	return nil
}

func (s *Service) LocalDesktopInfo(ctx context.Context) (LocalDesktopInfo, error) {
	s.mu.Lock()
	p, ok := s.provider.(*MacDesktop)
	s.mu.Unlock()
	if !ok {
		return LocalDesktopInfo{}, problem("unsupported", "Local desktop setup is available on paired Macs.")
	}
	return p.Info(ctx), nil
}

func (s *Service) ConfigureLocalDesktop(ctx context.Context, settings LocalDesktopSettings, credentials Credentials) (Snapshot, error) {
	s.operation.Lock()
	defer s.operation.Unlock()
	if s.macFactory == nil {
		return Snapshot{}, problem("unsupported", "Local desktop setup is available on paired Macs.")
	}
	if !settings.Enabled {
		settings.Username = ""
		settings.Password = ""
	}
	// Joining/retrying an identical setup must not drop active viewers or a
	// controller. Reconfiguration remains serialized with normal desktop input.
	s.mu.Lock()
	existing, sameProvider := s.provider.(*MacDesktop)
	unchanged := sameProvider && existing.settings == settings && s.snapshot.Error == nil
	s.mu.Unlock()
	if unchanged {
		return s.Snapshot(), nil
	}
	p, err := s.macFactory(settings)
	if err != nil {
		return Snapshot{}, err
	}
	if settings.Enabled {
		if _, err = p.Connect(ctx); err != nil {
			_ = p.Close()
			return Snapshot{}, err
		}
		_ = p.Close() // Setup validates credentials without leaving a viewer/capture running.
	}
	data, err := json.Marshal(savedLocalDesktop{RegistryID: s.db.Identity(), LocalDesktopSettings: settings})
	if err != nil {
		return Snapshot{}, err
	}
	if atomic, ok := credentials.(interface{ SetMany(map[string]string) error }); ok {
		err = atomic.SetMany(map[string]string{localDesktopKey: string(data)})
	} else {
		err = credentials.Set(localDesktopKey, string(data))
	}
	if err != nil {
		return Snapshot{}, err
	}
	// Disabling or changing the login invalidates all old display grants at once.
	s.mu.Lock()
	s.sessions = map[string]*sessionState{}
	s.openIDs = map[string]string{}
	s.mu.Unlock()
	s.configureProvider(p)
	status, _ := p.Status(ctx)
	s.mu.Lock()
	s.snapshot.ProviderStatus = status
	s.snapshot.State = "ready"
	if status.Setup != nil {
		s.snapshot.State = "needs_setup"
	}
	s.signalLocked()
	s.mu.Unlock()
	return s.Snapshot(), nil
}
