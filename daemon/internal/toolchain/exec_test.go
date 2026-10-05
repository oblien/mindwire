package toolchain

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellPreservesNativeToolsWhenLoginReplacesPath(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("requires bash")
	}
	t.Setenv("MINDWIRE_TOOLCHAIN_DIR", t.TempDir())
	native := filepath.Join(t.TempDir(), "native tools ' $literal")
	login := filepath.Join(t.TempDir(), "login tools")
	for dir, tools := range map[string]map[string]string{
		native: {"mindwire-path-probe": "native"},
		login:  {"mindwire-path-probe": "wrong-version", "mindwire-login-probe": "login"},
	} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for name, output := range tools {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf '%s\\n' "+quote(output)+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", native+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Simulate /etc/profile replacing PATH before the generated command runs.
	script := "export PATH=" + quote(login) + ":/usr/bin:/bin; " + Shell("mindwire-path-probe && mindwire-login-probe")
	output, err := exec.Command(bash, "-c", script).CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "native\nlogin" {
		t.Fatalf("native/login tools changed or disappeared: %q %v", output, err)
	}
}
