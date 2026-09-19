package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureManager(t *testing.T) (*Manager, Spec) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("requires bash")
	}
	root := t.TempDir()
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", root)
	m := newManager(root, "0.1.16", "off")
	m.catalog.catalog = policyFixture()
	spec := Spec{ID: "codex", Name: "Fixture", Binary: "mindwire-fixture-cli", Package: "@fixture/cli", VersionArgs: []string{"--version"}, Environment: map[string]string{"MINDWIRE_FIXTURE_ENV": "selected"}}
	return m, spec
}

func fakeInstall(ctx context.Context, spec Spec, version, prefix string) error {
	bin := filepath.Join(prefix, "bin", spec.Binary)
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		return err
	}
	return os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' "+quote(version)+"\n"), 0700)
}

func TestInstallPinsVerifiedVersionAndAllLaunchPathsUseIt(t *testing.T) {
	m, spec := fixtureManager(t)
	var calls int
	m.install = func(ctx context.Context, s Spec, v, p string) error { calls++; return fakeInstall(ctx, s, v, p) }
	version, err := m.Install(context.Background(), spec, false)
	if err != nil || version != "1.1.0" {
		t.Fatalf("%s %v", version, err)
	}
	if got := m.Software(context.Background(), spec); !got.Managed || got.InstalledVersion != version || got.Compatibility != "supported" {
		t.Fatalf("%+v", got)
	}
	for _, cmd := range []*exec.Cmd{
		CommandContext(context.Background(), spec.Binary, "--version"),
		exec.Command("bash", "-lc", Shell(spec.Binary+" --version")),
	} {
		out, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(out)) != version {
			t.Fatalf("launch mismatch: %s %v", out, err)
		}
	}
	if _, err := m.Install(context.Background(), spec, true); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("reinstalled unchanged version %d times", calls)
	}
	// A restarted daemon uses the saved selection without installing or following latest.
	restarted := newManager(m.root, m.version, "off")
	restarted.catalog.catalog = m.catalog.catalog
	if got := restarted.Software(context.Background(), spec); !got.Managed || got.InstalledVersion != version {
		t.Fatalf("restart: %+v", got)
	}
}

func TestFailedOrWrongVersionInstallPreservesPreviousSelection(t *testing.T) {
	for _, badVersion := range []bool{false, true} {
		t.Run(fmt.Sprint(badVersion), func(t *testing.T) {
			m, spec := fixtureManager(t)
			old := filepath.Join(m.root, spec.Binary, "versions", "1.0.0")
			if err := fakeInstall(context.Background(), spec, "1.0.0", old); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(selection{Version: "1.0.0"})
			if err := atomicWrite(filepath.Join(m.root, spec.Binary, "selected.json"), data); err != nil {
				t.Fatal(err)
			}
			m.install = func(ctx context.Context, s Spec, v, p string) error {
				if badVersion {
					return fakeInstall(ctx, s, "9.0.0", p)
				}
				return fmt.Errorf("download failed")
			}
			if _, err := m.Install(context.Background(), spec, true); err == nil {
				t.Fatal("accepted failed/unverified install")
			}
			if selected(m.root, spec.Binary) != "1.0.0" {
				t.Fatal("failed install changed selected executable")
			}
			if out, err := CommandContext(context.Background(), spec.Binary).Output(); err != nil || strings.TrimSpace(string(out)) != "1.0.0" {
				t.Fatalf("old version lost: %s %v", out, err)
			}
		})
	}
}

func TestExistingExternalVersionsAreNotSilentlyDowngradedOrReplaced(t *testing.T) {
	m, spec := fixtureManager(t)
	// Use an actual external process so explicit refreshes exercise the version probe too.
	spec.Binary = "bash"
	spec.VersionArgs = []string{"-c", "printf '3.0.0\\n'"}
	m.install = func(context.Context, Spec, string, string) error { t.Fatal("touched external CLI"); return nil }
	if _, err := m.Install(context.Background(), spec, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(context.Background(), spec, true); err == nil {
		t.Fatal("silently downgraded untested newer version")
	}
	if selected(m.root, spec.Binary) != "" {
		t.Fatal("created a selection for an external CLI")
	}
}

func TestKnownIncompatibleCLIIsRejectedAndDoesNotAutoInstallOnEnsure(t *testing.T) {
	m, spec := fixtureManager(t)
	spec.Binary = "bash"
	spec.VersionArgs = []string{"-c", "printf '2.0.0\\n'"}
	if _, err := m.Check(context.Background(), spec); err == nil {
		t.Fatal("accepted known incompatible version")
	}
	m.install = func(context.Context, Spec, string, string) error {
		t.Fatal("automatically changed an existing CLI")
		return nil
	}
	if _, err := m.Install(context.Background(), spec, false); err == nil {
		t.Fatal("ensure hid incompatibility")
	}
}
