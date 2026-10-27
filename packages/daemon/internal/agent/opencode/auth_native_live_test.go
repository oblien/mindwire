package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Discovery invokes no login or model call and uses isolated native homes.
func TestInstalledOpenCodeAuthDiscovery(t *testing.T) {
	if os.Getenv("MINDWIRE_OPENCODE_NATIVE") != "1" {
		t.Skip("set MINDWIRE_OPENCODE_NATIVE=1 for installed native auth discovery")
	}
	root := t.TempDir()
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, filepath.Join(root, name))
	}
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{}`)
	m := newAuth(mapStore{})
	methods, err := m.DiscoverMethods(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	native := 0
	for _, method := range methods {
		if strings.HasPrefix(method.ID, "oauth:") {
			native++
		}
	}
	if native == 0 {
		t.Fatal("no native OAuth methods discovered")
	}
	t.Logf("discovered %d native OpenCode provider sign-in methods", native)
}
