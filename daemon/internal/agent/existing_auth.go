package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const ExistingAuthMethod = "existing"
const AuthSourceEnvironment = "MINDWIRE_AUTH_SOURCE"
const nativeAuthLimit = 128 * 1024

// ExistingAuthConnection contains display metadata only. A saved connection is
// offered for reuse; its existence is not a claim that a credential is valid.
type ExistingAuthConnection struct {
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
}

// AuthSource is supplied by a trusted native host at boot, never by an HTTP
// request. Its environment is used only to resolve selected credential references.
// It is not stored, returned by the API, or inherited by agent processes.
type AuthSource struct {
	Home        string            `json:"home"`
	Environment map[string]string `json:"environment,omitempty"`
	// A trusted host connection is reused on first setup unless the host requests
	// manual selection. Explicit sign-out and existing connection choices always win.
	Reuse    string                                                            `json:"reuse,omitempty"`
	Keychain func(context.Context, string, string, bool) ([]byte, bool, error) `json:"-"`
}

// ConsumeAuthSource removes the bootstrap value before the daemon starts any
// native child. Ordinary daemons need no source: they already use their host login.
func ConsumeAuthSource() (*AuthSource, error) {
	raw := os.Getenv(AuthSourceEnvironment)
	_ = os.Unsetenv(AuthSourceEnvironment)
	if raw == "" {
		return nil, nil
	}
	var source AuthSource
	if len(raw) > 2*1024*1024 || json.Unmarshal([]byte(raw), &source) != nil || !filepath.IsAbs(source.Home) {
		return nil, errors.New("invalid native authentication source")
	}
	if source.Reuse != "" && source.Reuse != "automatic" && source.Reuse != "manual" {
		return nil, errors.New("invalid native authentication reuse mode")
	}
	return &source, nil
}

func (s *AuthSource) ConfigDir(variable, fallback string) string {
	if s == nil {
		return ""
	}
	if value := strings.TrimSpace(s.Environment[variable]); value != "" {
		return filepath.Clean(value)
	}
	return filepath.Join(s.Home, fallback)
}

// ReadNativeAuthFile bounds credential reads and never includes file contents in errors.
func ReadNativeAuthFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("could not read saved connection")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > nativeAuthLimit {
		return nil, errors.New("could not read saved connection")
	}
	data, err := io.ReadAll(io.LimitReader(file, nativeAuthLimit+1))
	if err != nil || len(data) > nativeAuthLimit {
		return nil, errors.New("could not read saved connection")
	}
	return data, nil
}

func (s *AuthSource) ReadKeychain(ctx context.Context, service, account string, reveal bool) ([]byte, bool, error) {
	if s.Keychain != nil {
		return s.Keychain(ctx, service, account, reveal)
	}
	if runtime.GOOS != "darwin" {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"find-generic-password", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	if reveal {
		args = append(args, "-w")
	}
	data, err := exec.CommandContext(ctx, "/usr/bin/security", args...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return nil, false, nil
		}
		return nil, false, errors.New("could not read saved connection; unlock the keychain and try again")
	}
	if !reveal {
		return nil, true, nil
	}
	if len(data) > nativeAuthLimit {
		return nil, false, errors.New("could not read saved connection")
	}
	return data, true, nil
}

// ExistingAuthModule is optional; adapters own native formats and verification.
// ManagedAuth uses it on first setup and exposes explicit reconnect through the
// ordinary methods -> begin -> status flow.
type ExistingAuthModule interface {
	SetAuthSource(*AuthSource)
	ExistingMethod() *AuthMethod
	UseExisting(context.Context) (AuthState, error)
}

// NativeAuthSettings clears only this agent's connection fields when switching.
// Models, other agents and shared provider credentials are preserved unless the
// importing adapter explicitly supplies a new value.
func NativeAuthSettings(module AuthModule, method string) map[string]string {
	settings := map[string]string{"authMethod": method, "oauthToken": ""}
	for _, m := range module.Methods() {
		for _, field := range m.Fields {
			settings[field.Key] = ""
		}
	}
	return settings
}

// ImportNativeAuth restores both the private files and connection fields if native
// verification fails. It never writes to the source account's files or keychain.
func ImportNativeAuth(ctx context.Context, store CredStore, files map[string][]byte, settings map[string]string, verify func(context.Context) bool) error {
	type original struct {
		path string
		data []byte
	}
	previous := []original{}
	previousSettings := map[string]string{}
	restore := func() {
		for i := len(previous) - 1; i >= 0; i-- {
			item := previous[i]
			if item.data == nil {
				_ = os.Remove(item.path)
			} else {
				_ = writeNativeAuth(item.path, item.data)
			}
		}
		for key, value := range previousSettings {
			_ = store.Set(key, value)
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		data, err := ReadNativeAuthFile(path)
		if err != nil {
			restore()
			return err
		}
		previous = append(previous, original{path, data})
		if err := writeNativeAuth(path, files[path]); err != nil {
			restore()
			return errors.New("could not prepare the private connection")
		}
	}
	for key, value := range settings {
		previousSettings[key] = store.Get(key)
		if err := store.Set(key, value); err != nil {
			restore()
			return errors.New("could not save the connection")
		}
	}
	if ctx.Err() != nil || !verify(ctx) {
		restore()
		return errors.New("the saved connection could not be verified; sign in again or choose another method")
	}
	return nil
}

func writeNativeAuth(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".native-auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
