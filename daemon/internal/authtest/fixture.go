// Package authtest supplies isolated native-process fixtures for auth tests.
package authtest

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type Store struct {
	mu      sync.Mutex
	values  map[string]string
	FailKey string
}

func NewStore(values map[string]string) *Store { return &Store{values: maps.Clone(values)} }
func (s *Store) Get(key string) string         { s.mu.Lock(); defer s.mu.Unlock(); return s.values[key] }
func (s *Store) All() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.values)
}
func (s *Store) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == s.FailKey {
		return errors.New("fixture storage failure")
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

// NativeCLI pins the test process as the CLI, including through bash login shells.
func NativeCLI(t *testing.T, binary, testName string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", root)
	t.Setenv("MINDWIRE_AUTH_TEST_PROCESS", binary)
	path := filepath.Join(root, binary, "versions", "1.0.0", "bin", binary)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nexec " + quote(self) + " -test.run=" + quote("^"+testName+"$") + " -- \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, binary, "selected.json"), []byte(`{"version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINDWIRE_AUTH_TEST_STATE", filepath.Join(root, "native-account"))
	t.Setenv("MINDWIRE_AUTH_TEST_CANCEL", filepath.Join(root, "canceled"))
	return root
}

func Eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native auth did not reach the expected state")
}
