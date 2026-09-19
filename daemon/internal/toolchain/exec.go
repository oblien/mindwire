package toolchain

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func Root() string {
	if dir := os.Getenv("MINDWIRE_TOOLCHAIN_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mindwire", "toolchains")
}

type selection struct {
	Version     string            `json:"version"`
	Environment map[string]string `json:"environment,omitempty"`
}

func selected(root, binary string) string {
	if binary == "" || filepath.Base(binary) != binary {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, binary, "selected.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return "invalid-selection"
	}
	var value selection
	if json.Unmarshal(data, &value) != nil || !validVersion(value.Version) {
		return "invalid-selection"
	}
	return value.Version
}

func versionBinary(root, binary, version string) string {
	prefix := filepath.Join(root, binary, "versions", version)
	if runtime.GOOS == "windows" {
		return filepath.Join(prefix, binary+".cmd")
	}
	return filepath.Join(prefix, "bin", binary)
}

// Executable resolves the same selection for native exec, help/schema discovery, auth and
// turns. A broken selected install must fail visibly; it never falls through to another CLI.
func Executable(binary string) string {
	if version := selected(Root(), binary); version != "" {
		return versionBinary(Root(), binary, version)
	}
	return binary
}

func CommandContext(ctx context.Context, binary string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, Executable(binary), args...)
	cmd.Env = Environment()
	return cmd
}

func Environment() []string {
	env := os.Environ()
	for _, s := range selections() {
		for k, v := range s.Environment {
			if validEnvKey(k) {
				env = append(env, k+"="+v)
			}
		}
	}
	return env
}

func selections() map[string]selection {
	out := map[string]selection{}
	entries, _ := os.ReadDir(Root())
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Root(), e.Name(), "selected.json"))
		if err != nil {
			continue
		}
		var s selection
		if json.Unmarshal(data, &s) != nil || !validVersion(s.Version) {
			s = selection{Version: "invalid-selection"}
		}
		out[e.Name()] = s
	}
	return out
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, c := range key {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// Shell restores managed paths AFTER the login shell has sourced its startup files. Merely
// setting cmd.Env's PATH lets macOS path_helper choose a different, globally installed CLI.
func Shell(script string) string {
	var bins []string
	var env []string
	var pins []string
	for binary, s := range selections() {
		path := versionBinary(Root(), binary, s.Version)
		bins = append(bins, filepath.Dir(path))
		pins = append(pins, "unset -f "+quote(binary)+"; hash -p "+quote(path)+" "+quote(binary))
		for k, v := range s.Environment {
			if validEnvKey(k) {
				env = append(env, "export "+k+"="+quote(v))
			}
		}
	}
	if len(bins) == 0 {
		return script
	}
	sort.Strings(bins)
	sort.Strings(env)
	sort.Strings(pins)
	prefix := "export PATH=" + quote(strings.Join(bins, string(os.PathListSeparator))) + ":\"$PATH\"; "
	prefix += "shopt -u checkhash; " + strings.Join(pins, "; ") + "; "
	if len(env) > 0 {
		prefix += strings.Join(env, "; ") + "; "
	}
	return prefix + script
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
