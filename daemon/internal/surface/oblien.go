package surface

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Binding is write-only. Its SSH grant is desktop-only, expires at the provider's
// deadline, and must match the workspace for which it was issued.
type Binding struct {
	RegistryID   string        `json:"registryId"`
	WorkspaceID  string        `json:"workspaceId"`
	GatewayToken string        `json:"gatewayToken,omitempty"`
	Connection   SSHConnection `json:"connection"`
}
type SSHConnection struct {
	ExpiresAt time.Time   `json:"expires_at"`
	SSH       SSHSettings `json:"ssh"`
	VNC       VNCSettings `json:"vnc"`
}
type SSHSettings struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	Password           string `json:"password"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
}
type VNCSettings struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Authentication string `json:"authentication"`
}

type Oblien struct {
	mu      sync.Mutex
	binding Binding
	http    *http.Client
	base    string
	client  *rfbClient
	status  ProviderStatus
	target  string
}

func NewOblien(binding Binding) (*Oblien, error) {
	c := binding.Connection
	if !regexp.MustCompile(`^[a-fA-F0-9]{16}$`).MatchString(binding.WorkspaceID) || len(binding.GatewayToken) > 8192 || c.SSH.Host != "ssh.oblien.com" || c.SSH.Port != 22 ||
		c.VNC.Host != "127.0.0.1" || c.VNC.Port != 5900 || c.VNC.Authentication != "none" ||
		!strings.HasPrefix(c.SSH.Username, "desktop-") || c.SSH.Password == "" || len(c.SSH.Password) > 2048 ||
		!strings.HasPrefix(c.SSH.HostKeyFingerprint, "SHA256:") {
		return nil, problem("invalid_binding", "Use the desktop SSH connection issued by Oblien for this workspace.")
	}
	if !strings.Contains(c.SSH.Username, binding.WorkspaceID) {
		return nil, problem("workspace_mismatch", "Desktop credentials belong to another workspace.")
	}
	if c.ExpiresAt.Before(time.Now()) {
		return nil, problem("needs_authorization", "The desktop connection has expired. Reconnect to get a new one.")
	}
	return &Oblien{binding: binding, http: &http.Client{Timeout: 20 * time.Second}, base: "https://workspace.oblien.com"}, nil
}
func (p *Oblien) ExpiresAt() time.Time { return p.binding.Connection.ExpiresAt }
func (p *Oblien) expired() error {
	if time.Now().After(p.ExpiresAt()) {
		_ = p.Close()
		return problem("needs_authorization", "The desktop SSH connection expired. Reconnect to authorize it again.")
	}
	return nil
}
func (p *Oblien) request(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {
	if p.binding.GatewayToken == "" {
		return nil, problem("needs_authorization", "Refresh the workspace desktop connection to use this operation.")
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.binding.GatewayToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := p.http.Do(req)
	if err != nil {
		return nil, problem("unavailable", "The workspace desktop service could not be reached.")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		if response.StatusCode == 401 || response.StatusCode == 403 {
			return nil, problem("needs_authorization", "Refresh the workspace desktop authorization.")
		}
		if response.StatusCode == 404 {
			return nil, problem("unsupported", "This workspace runtime does not expose desktop access.")
		}
		return nil, problem("unavailable", "The workspace display is not ready. Retry when the workspace is running.")
	}
	return response, nil
}
func (p *Oblien) json(ctx context.Context, method, path string, input, out any) error {
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	response, err := p.request(ctx, method, path, body, "application/json")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 2<<20))
		return err
	}
	return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(out)
}
func (p *Oblien) Status(ctx context.Context) (ProviderStatus, error) {
	if err := p.expired(); err != nil {
		return ProviderStatus{}, err
	}
	var status ProviderStatus
	if err := p.json(ctx, "GET", "/desktop/status", nil, &status); err != nil {
		return status, err
	}
	var runtimes struct {
		Default string `json:"default_target"`
		Targets []struct {
			ID      string `json:"id"`
			Runtime struct {
				OS string `json:"os"`
			} `json:"runtime"`
		} `json:"targets"`
	}
	if status.Supported {
		if err := p.json(ctx, "GET", "/runtimes", nil, &runtimes); err != nil {
			return status, err
		}
		target := runtimes.Default
		parts := strings.Split(p.binding.GatewayToken, ".")
		if len(parts) == 3 {
			if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
				var claims struct {
					DesktopRuntime string `json:"desktop_runtime"`
				}
				if json.Unmarshal(payload, &claims) == nil && claims.DesktopRuntime != "" {
					target = claims.DesktopRuntime
				}
			}
		}
		for _, r := range runtimes.Targets {
			if r.ID == target && validTarget.MatchString(target) {
				status.OS = r.Runtime.OS
				p.target = target
				break
			}
		}
		status.Capabilities = Capabilities{View: true, Capture: true, Pointer: true, Keyboard: true, Text: true, KeyboardText: true, ExtendedKeys: true}
		if status.OS == "darwin" && p.target != "" {
			status.Capabilities.ClipboardRead = true
			status.Capabilities.ClipboardWrite = true
		}
	}
	p.mu.Lock()
	p.status = status
	p.mu.Unlock()
	return status, nil
}

var validTarget = regexp.MustCompile("^[a-zA-Z0-9_-]{1,64}$")

func (p *Oblien) Connect(ctx context.Context) (Geometry, error) {
	if err := p.expired(); err != nil {
		return Geometry{}, err
	}
	p.mu.Lock()
	if p.client != nil && p.client.alive() {
		client := p.client
		p.mu.Unlock()
		return client.refreshGeometry(ctx)
	}
	p.mu.Unlock()
	_ = p.Close()
	tunnel, err := p.desktopSocket(ctx)
	if err != nil {
		return Geometry{}, err
	}
	rfb, err := newRFB(ctx, tunnel)
	if err != nil {
		tunnel.Close()
		return Geometry{}, err
	}
	p.mu.Lock()
	p.client = rfb
	p.mu.Unlock()
	return rfb.geometry(), nil
}
func (p *Oblien) Capture(ctx context.Context) (image.Image, Geometry, error) {
	if _, err := p.Connect(ctx); err != nil {
		return nil, Geometry{}, err
	}
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	return client.capture(ctx)
}
func (p *Oblien) Apply(ctx context.Context, a Action) (string, error) {
	// Service.Apply refreshes geometry before spatial validation. Reusing the live
	// connection here avoids a second frame round-trip for every pointer move, and
	// keeps keystrokes independent of screen repaint latency. No retry after input.
	if err := p.expired(); err != nil {
		return "", err
	}
	p.mu.Lock()
	connected := p.client != nil && p.client.alive()
	p.mu.Unlock()
	if !connected {
		if spatial(a.Kind) {
			return "", problem("stale_frame", "The display reconnected. Wait for a fresh frame before moving the pointer.")
		}
		if _, err := p.Connect(ctx); err != nil {
			return "", err
		}
	}
	p.mu.Lock()
	client := p.client
	status := p.status
	p.mu.Unlock()
	switch a.Kind {
	case "clipboard_read":
		if !status.Capabilities.ClipboardRead {
			return "", problem("unsupported", "This desktop provider does not support native clipboard reading.")
		}
		return p.readMacClipboard(ctx)
	case "clipboard_write":
		if !status.Capabilities.ClipboardWrite {
			return "", problem("unsupported", "This desktop provider does not support native clipboard writing.")
		}
		return "", p.writeMacClipboard(ctx, a.Text)
	case "text":
		if a.TextMode == "keyboard" {
			return "", client.typeText(ctx, a.Text, status.OS == "darwin")
		}
		if status.OS == "darwin" {
			err := p.writeMacClipboard(ctx, a.Text)
			if err == nil {
				return "", client.chord(ctx, []string{"Meta", "v"})
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != "login_required" {
				return "", err
			}
			// QEMU login accepts only a bounded US-keyboard sequence.
			return "", client.typeText(ctx, a.Text, true)
		}
		return "", client.typeText(ctx, a.Text, false)
	default:
		return "", client.apply(ctx, a)
	}
}
func (p *Oblien) Release(ctx context.Context) error {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.release(ctx)
}
func (p *Oblien) Close() error {
	p.mu.Lock()
	client := p.client
	p.client = nil
	p.mu.Unlock()
	if client != nil {
		client.close()
	}
	return nil
}

const macClipboardUser = `set -eu
export LC_ALL=en_US.UTF-8
desktop_user=$(/usr/bin/stat -f %Su /dev/console)
case "$desktop_user" in root|loginwindow|_mbsetupuser|'') exit 69;; esac
desktop_uid=$(/usr/bin/id -u "$desktop_user")
`

func (p *Oblien) readMacClipboard(ctx context.Context) (string, error) {
	var result struct {
		ExitCode *int   `json:"exit_code"`
		Stdout   string `json:"stdout"`
	}
	script := macClipboardUser + fmt.Sprintf("/bin/launchctl asuser \"$desktop_uid\" /usr/bin/sudo -u \"$desktop_user\" /usr/bin/pbpaste | /usr/bin/head -c %d | /usr/bin/base64", MaxTextBytes+1)
	err := p.json(ctx, "POST", "/runtimes/"+url.PathEscape(p.target)+"/exec", map[string]any{
		"cmd": []string{"/bin/sh", "-c", script}, "keep_logs": false, "exec_mode": "foreground", "timeout_seconds": 8}, &result)
	if err != nil {
		return "", err
	}
	if result.ExitCode != nil && *result.ExitCode == 69 {
		return "", problem("login_required", "Sign in to the desktop to use its clipboard.")
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		return "", problem("clipboard_failed", "The desktop clipboard could not be read.")
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(result.Stdout), ""))
	if err != nil || len(data) > MaxTextBytes {
		return "", problem("clipboard_limit", "Copy up to one MiB of text at a time.")
	}
	return string(data), nil
}
func (p *Oblien) writeMacClipboard(ctx context.Context, text string) error {
	if p.target == "" {
		return problem("login_required", "Sign in to the desktop to paste text.")
	}
	script := macClipboardUser + "exec /bin/launchctl asuser \"$desktop_uid\" /usr/bin/sudo -u \"$desktop_user\" /usr/bin/pbcopy"
	code, _, err := p.execInput(ctx, []string{"/bin/sh", "-c", script}, text)
	if err != nil {
		return err
	}
	if code == 69 {
		return problem("login_required", "Sign in to the desktop to paste Unicode text.")
	}
	if code != 0 {
		return problem("clipboard_failed", "The desktop clipboard could not be updated.")
	}
	return nil
}
