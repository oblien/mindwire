package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/authtest"
)

func TestExistingClaudeAccountUsesNativeNamespaceAndAllowlist(t *testing.T) {
	host := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	m := newAuth(authtest.NewStore(nil))
	var reads []bool
	raw := []byte(`{"claudeAiOauth":{"accessToken":"fixture-access","refreshToken":"fixture-refresh","scopes":["user:inference"],"unrelated":"must-not-copy"},"hooks":"must-not-copy"}`)
	m.SetAuthSource(&agent.AuthSource{Home: host, Keychain: func(_ context.Context, service, account string, reveal bool) ([]byte, bool, error) {
		if service != "Claude Code-credentials" || account != "" {
			t.Fatal("wrong Claude keychain namespace")
		}
		reads = append(reads, reveal)
		return raw, true, nil
	}})
	if m.ExistingMethod() == nil || len(reads) != 1 || reads[0] {
		t.Fatal("discovery revealed a keychain credential")
	}
	data, found, err := m.sourceAccount(context.Background(), true)
	if err != nil || !found || strings.Contains(string(data), "must-not-copy") || !strings.Contains(string(data), "fixture-refresh") {
		t.Fatal("native account allowlist failed")
	}
	if len(reads) != 2 || !reads[1] {
		t.Fatal("explicit reuse did not reveal native account")
	}
	m.source.Keychain = func(context.Context, string, string, bool) ([]byte, bool, error) {
		return nil, false, errors.New("keychain unavailable fixture-private")
	}
	state, err := m.UseExisting(context.Background())
	if err != nil || state.Status != "error" || strings.Contains(state.Message, "fixture-private") {
		t.Fatal("keychain failure exposed native error contents")
	}
	if _, err := os.Stat(filepath.Join(host, ".claude", ".credentials.json")); !os.IsNotExist(err) {
		t.Fatal("source account modified")
	}
}
