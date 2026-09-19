package toolchain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in: downloads real official packages into a temporary root. An incomplete managed
// selection isolates the test from existing global CLIs. Installation, native verification
// and executable resolution are real. No global CLI or native login is changed.
func TestManagedOfficialPackagesLive(t *testing.T) {
	if os.Getenv("MINDWIRE_TOOLCHAIN_LIVE") != "1" {
		t.Skip("set MINDWIRE_TOOLCHAIN_LIVE=1 to verify official package installation")
	}
	root := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", root)
	m := newManager(root, "0.1.16", "off")
	specs := []Spec{
		{ID: "codex", Name: "Codex", Binary: "codex", Package: "@openai/codex", VersionArgs: []string{"--version"}},
		{ID: "claude-code", Name: "Claude", Binary: "claude", Package: "@anthropic-ai/claude-code", VersionArgs: []string{"--version"}, Environment: map[string]string{"DISABLE_AUTOUPDATER": "1"}},
		{ID: "grok", Name: "Grok", Binary: "grok", Package: "@xai-official/grok", VersionArgs: []string{"--no-auto-update", "version"}},
		{ID: "opencode", Name: "OpenCode", Binary: "opencode", Package: "opencode-ai", VersionArgs: []string{"--version"}, Environment: map[string]string{"OPENCODE_DISABLE_AUTOUPDATE": "true"}},
	}
	for _, spec := range specs {
		t.Run(spec.ID, func(t *testing.T) {
			data, _ := json.Marshal(selection{Version: "0.0.0"})
			if err := atomicWrite(filepath.Join(root, spec.Binary, "selected.json"), data); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			version, err := m.Install(ctx, spec, false)
			if err != nil {
				t.Fatal(err)
			}
			info := m.Software(ctx, spec)
			if !info.Managed || info.Compatibility != "supported" || info.InstalledVersion != version {
				t.Fatalf("%+v", info)
			}
			output, err := CommandContext(ctx, spec.Binary, spec.VersionArgs...).Output()
			if err != nil || ParseVersion(string(output)) != version {
				t.Fatalf("native command: %s %v", output, err)
			}
			t.Logf("official %s %s installed, verified and selected", spec.ID, version)
		})
	}
}
