//go:build !windows

package gitaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrokerIsPrivateLocalSocketAndRemovedOnClose(t *testing.T) {
	s := service(t)
	if strings.Contains(s.endpoint, "://") {
		t.Fatal("credential broker is network reachable")
	}
	info, err := os.Stat(s.endpoint)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("broker must be an owner-only socket: %v", err)
	}
	directory := filepath.Dir(s.endpoint)
	info, err = os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("broker directory must exclude other OS users: %v", err)
	}
	s.Close()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("broker directory survived close")
	}
	if _, _, err := brokerClient("http://127.0.0.1:1234"); err == nil {
		t.Fatal("Unix helper accepted a network credential source")
	}
}
