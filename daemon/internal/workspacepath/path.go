// Package workspacepath resolves project identities on the machine that owns the files.
package workspacepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Canonical expands the daemon user's home and resolves existing symlinks, including
// the parent of a directory that has not been created yet. It never creates files.
func Canonical(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || len(path) > 4096 || strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("a project directory is required")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("project directory must be absolute or start with ~/")
	}
	path = filepath.Clean(path)
	parent := path
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		// A dangling symlink is not a new directory and must not be overwritten.
		if info, statErr := os.Lstat(parent); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("project directory contains a dangling symlink")
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", err
		}
		tail = append(tail, filepath.Base(parent))
		parent = next
	}
}

// Contains includes equality and respects path components ("app" is not "app2").
func Contains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func Overlaps(a, b string) bool { return Contains(a, b) || Contains(b, a) }

// WorkingDirectory accepts the legacy turn API's relative/default working directory.
func WorkingDirectory(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !filepath.IsAbs(path) {
		var err error
		path, err = filepath.Abs(path)
		if err != nil {
			return "", err
		}
	}
	return Canonical(path)
}
