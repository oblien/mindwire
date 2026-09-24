package gitauthor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Git's conditional includes give workspace defaults their proper native
// precedence, without changing anyone's global name/email or copying values
// into each repository. Conditions also cover repositories initialized later.
func (s *Service) bind(ctx context.Context, additional, excluded string) error {
	snapshot, err := s.store.Snapshot(nil)
	if err != nil {
		return err
	}
	directories := map[string]bool{}
	for _, p := range snapshot.Projects {
		if p.Path != excluded {
			directories[p.Path] = true
		}
	}
	if additional != "" {
		directories[additional] = true
	}
	conditions := map[string]bool{}
	for directory := range directories {
		path, err := filepath.EvalSymlinks(directory)
		if err != nil {
			continue
		} // A removed folder doesn't grant a broader match.
		conditions["gitdir:"+pattern(filepath.ToSlash(path))+"/"] = true
		// A linked worktree's Git directory can live outside its working folder.
		if gitDir, err := git(ctx, "-C", directory, "rev-parse", "--absolute-git-dir"); err == nil {
			conditions["gitdir:"+pattern(filepath.ToSlash(gitDir))] = true
		}
	}
	ordered := make([]string, 0, len(conditions))
	for condition := range conditions {
		ordered = append(ordered, condition)
	}
	sort.Strings(ordered)
	var body strings.Builder
	for _, condition := range ordered {
		body.WriteString("[includeIf " + quote(condition) + "]\n\tpath = " + quote(s.config(AllWorkspaces)) +
			"\n\tpath = " + quote(s.config(Workspace)) + "\n")
	}
	bindings := filepath.Join(s.store.Directory(), "git-author-bindings.config")
	if data, err := os.ReadFile(bindings); err != nil || string(data) != body.String() {
		if err := editConfig(ctx, bindings, func(temp string) error {
			return os.WriteFile(temp, []byte(body.String()), 0600)
		}); err != nil {
			return err
		}
	}
	global, err := globalConfig()
	if err != nil {
		return err
	}
	block := "# Mindwire author defaults: " + s.store.Identity() + "\n[include]\n\tpath = " + quote(bindings) +
		"\n# End Mindwire author defaults\n"
	if data, err := os.ReadFile(global); err == nil && strings.Contains(string(data), block) {
		return nil
	}
	return editConfig(ctx, global, func(temp string) error {
		data, err := os.ReadFile(temp)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), block) {
			return nil
		}
		// Append a conditional-only include. Existing global settings and every
		// unrelated repository retain their values; local Git config wins normally.
		return os.WriteFile(temp, []byte(strings.TrimRight(string(data), "\n")+"\n"+block), 0600)
	})
}

func pattern(path string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`, "]", `\]`).Replace(path)
}

// Match Git's documented --global write target, including XDG and overrides.
func globalConfig() (string, error) {
	if path := os.Getenv("GIT_CONFIG_GLOBAL"); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find the Git user's home directory")
	}
	primary := filepath.Join(home, ".gitconfig")
	if _, err := os.Stat(primary); err == nil {
		return primary, nil
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	secondary := filepath.Join(xdg, "git", "config")
	if _, err := os.Stat(secondary); err == nil {
		return secondary, nil
	}
	return primary, nil
}

func (s *Service) PrepareRepository(ctx context.Context, directory string) error {
	if !s.configured() {
		return nil
	}
	return s.bind(ctx, directory, "")
}

func (s *Service) PrepareAll(ctx context.Context) error {
	if !s.configured() {
		return nil
	}
	return s.bind(ctx, "", "")
}

func (s *Service) Detach(ctx context.Context, directory string) error {
	if !s.configured() {
		return nil
	}
	return s.bind(ctx, "", directory)
}
