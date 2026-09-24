// Package gitauthor saves attribution independently of committing. Native Git
// reads the same defaults, whether a command comes from the app, a harness or a terminal.
package gitauthor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

// editConfig takes Git's own config lock and publishes both author fields in a
// single rename. A failed save never leaves a new name paired with an old email.
func editConfig(ctx context.Context, path string, edit func(string) error) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%w: Git configuration is being updated; try saving again", registry.ErrConflict)
		}
		return err
	}
	defer os.Remove(path + ".lock")
	defer lock.Close()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Git configuration is not a regular file")
		}
		if err := lock.Chmod(info.Mode().Perm()); err != nil {
			return err
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".mindwire-author-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := edit(temp.Name()); err != nil {
		return err
	}
	// Validate the final file before publishing it. Includes are deliberately not
	// followed: they may be relative to the original file or not exist yet.
	if _, err := git(ctx, "config", "--file", temp.Name(), "--no-includes", "--list"); err != nil {
		return err
	}
	data, err = os.ReadFile(temp.Name())
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := lock.Write(data); err != nil {
		return err
	}
	if err := lock.Sync(); err != nil {
		return err
	}
	if err := lock.Close(); err != nil {
		return err
	}
	return os.Rename(path+".lock", path)
}

func git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	data, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("could not update Git author configuration: %s", strings.TrimSpace(string(data)))
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

func value(ctx context.Context, args []string, key string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(args, "--get", key)...)
	data, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("could not read Git author configuration")
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

func identity(ctx context.Context, args ...string) (*registry.GitIdentity, error) {
	values, err := configValues(ctx, args...)
	if err != nil {
		return nil, err
	}
	return identityFrom(values), nil
}

// One Git process reads both fields from the same file snapshot. Reading the
// name and email in separate commands could straddle an atomic settings save.
func configValues(ctx context.Context, args ...string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "git", append(args, "--null", "--get-regexp", `^(user\.(name|email)|mindwire\.authorrequest)$`)...)
	data, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("could not read Git author configuration")
		}
	}
	values := map[string]string{}
	for _, field := range strings.Split(string(data), "\x00") {
		if key, value, found := strings.Cut(field, "\n"); found {
			values[key] = value
		}
	}
	return values, nil
}

func identityFrom(values map[string]string) *registry.GitIdentity {
	name, email := values["user.name"], values["user.email"]
	if name == "" && email == "" {
		return nil
	}
	return &registry.GitIdentity{Name: name, Email: email}
}

func writeIdentity(ctx context.Context, path string, author *registry.GitIdentity) error {
	for _, field := range []struct{ key, value string }{
		{"user.name", ""}, {"user.email", ""},
	} {
		if author != nil {
			if field.key == "user.name" {
				field.value = author.Name
			} else {
				field.value = author.Email
			}
			if _, err := git(ctx, "config", "--file", path, "--replace-all", field.key, field.value); err != nil {
				return err
			}
		} else {
			cmd := exec.CommandContext(ctx, "git", "config", "--file", path, "--unset-all", field.key)
			if err := cmd.Run(); err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 5 {
					return fmt.Errorf("could not clear Git author configuration: %w", err)
				}
			}
		}
	}
	return nil
}

func repositoryConfig(ctx context.Context, directory string) (string, error) {
	if directory == "" {
		return "", fmt.Errorf("%w: select a repository", registry.ErrInvalid)
	}
	path, err := git(ctx, "-C", directory, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("%w: this folder is not a Git repository", registry.ErrInvalid)
	}
	return filepath.Join(path, "config"), nil
}

func quote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\b", `\b`).Replace(value) + `"`
}
