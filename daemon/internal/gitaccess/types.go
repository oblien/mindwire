// Package gitaccess supplies repository authentication to Git without putting
// credentials in project metadata, command arguments, or Git configuration.
package gitaccess

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const Version = 1

var ErrCredential = errors.New("GitHub authentication is unavailable")

// Connection is a non-secret reference. A nil project connection inherits the
// workspace default; native explicitly uses the server's existing Git setup.
type Connection struct {
	ID       string `json:"id"`
	Login    string `json:"login"`
	Mode     string `json:"mode"`     // token, oblienApp, sshKey, ghCli, native
	Lifetime string `json:"lifetime"` // run or workspace
}

// Auth is write-only. Run credentials are retained only for the operation's
// lifetime. Only an explicit workspace connection can persist credentials.
type Auth struct {
	ConnectionID string `json:"connectionId,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Username     string `json:"username,omitempty"`
	Token        string `json:"token,omitempty"`
	PrivateKey   string `json:"privateKey,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	ReadOnly     bool   `json:"readOnly,omitempty"`
}

func (c Connection) Validate() error {
	if c.ID == "" || len(c.ID) > 128 || len(c.Login) > 256 || strings.ContainsAny(c.ID+c.Login, "\x00\r\n") {
		return fmt.Errorf("%w: invalid connection", ErrCredential)
	}
	if c.Lifetime != "run" && c.Lifetime != "workspace" {
		return fmt.Errorf("%w: choose an operation or workspace lifetime", ErrCredential)
	}
	switch c.Mode {
	case "token", "sshKey", "ghCli", "native":
	case "oblienApp":
		if c.Lifetime != "run" {
			return fmt.Errorf("%w: Oblien clone tokens must be renewed for each operation", ErrCredential)
		}
	default:
		return fmt.Errorf("%w: unknown connection method", ErrCredential)
	}
	return nil
}

func (a Auth) Validate(c Connection) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if a.ConnectionID != "" && a.ConnectionID != c.ID {
		return fmt.Errorf("%w: the selected GitHub connection changed; refresh and try again", ErrCredential)
	}
	if len(a.Token) > 8192 || len(a.PrivateKey) > 32768 || len(a.Username) > 256 ||
		strings.ContainsAny(a.Token+a.Username, "\x00\r\n") || strings.ContainsRune(a.PrivateKey, 0) {
		return fmt.Errorf("%w: invalid credential", ErrCredential)
	}
	if a.ExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339, a.ExpiresAt)
		if err != nil || !expires.After(time.Now()) {
			return fmt.Errorf("%w: the token expired; reconnect GitHub", ErrCredential)
		}
	}
	switch c.Mode {
	case "token", "oblienApp":
		if a.Kind != "token" || a.Token == "" || a.PrivateKey != "" {
			return fmt.Errorf("%w: reconnect %s on this device or save access on the workspace", ErrCredential, c.Login)
		}
	case "sshKey":
		if a.Kind != "ssh" || !strings.Contains(a.PrivateKey, "PRIVATE KEY-----") || a.Token != "" {
			return fmt.Errorf("%w: reconnect the SSH key", ErrCredential)
		}
	case "ghCli":
		if a.Kind != "gh" || a.Token != "" || a.PrivateKey != "" {
			return fmt.Errorf("%w: use the workspace's GitHub CLI login", ErrCredential)
		}
	case "native":
		if a != (Auth{}) {
			return fmt.Errorf("%w: server authentication does not accept a forwarded credential", ErrCredential)
		}
	}
	return nil
}

// CheckRepository validates both the host and the transport before configuring
// a helper. Selecting an SSH key never silently rewrites an existing remote.
func (c Connection) CheckRepository(repo string) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	repository, err := Repository(repo)
	if err != nil {
		return "", err
	}
	if c.Mode == "sshKey" && !strings.HasPrefix(repo, "ssh://") && !strings.HasPrefix(repo, "git@") {
		return "", fmt.Errorf("%w: use an SSH remote with this connection", ErrCredential)
	}
	if c.Mode != "sshKey" && c.Mode != "native" && !strings.HasPrefix(repo, "https://") {
		return "", fmt.Errorf("%w: use an HTTPS remote with this connection", ErrCredential)
	}
	return repository, nil
}

// Repository accepts only an exact GitHub host and a single owner/repository.
// The returned identity is independent of HTTPS versus SSH and a .git suffix.
func Repository(raw string) (string, error) {
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = "ssh://git@github.com/" + strings.TrimPrefix(raw, "git@github.com:")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "ssh") || !strings.EqualFold(u.Host, "github.com") ||
		u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("%w: the selected connection can only authenticate a GitHub repository", ErrCredential)
	}
	if u.User != nil && (u.Scheme != "ssh" || u.User.String() != "git") {
		return "", fmt.Errorf("%w: repository URLs must not contain credentials", ErrCredential)
	}
	path := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: invalid GitHub repository", ErrCredential)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 100 {
			return "", fmt.Errorf("%w: invalid GitHub repository", ErrCredential)
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				return "", fmt.Errorf("%w: invalid GitHub repository", ErrCredential)
			}
		}
	}
	return strings.ToLower(path), nil
}
