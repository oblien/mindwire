package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SubagentLayout describes WHERE one adapter's persistent subagent definition files live, so the
// read/write/list logic is shared (DRY) across adapters — the same philosophy as MemoryLayout. It is a
// dedicated layout (not folded into MemoryLayout) because subagent stores diverge in two ways: they are
// scanned RECURSIVELY (a definition may sit in a nested folder), and a definition's identity comes from
// its frontmatter `name` rather than its filename. An adapter builds a layout PER CALL (resolving its
// config home lazily) and delegates each SubagentsModule method to the matching layout method.
type SubagentLayout struct {
	// ProjectDir is the definitions directory RELATIVE to a project dir (e.g. ".claude/agents"); ""
	// means the agent has no project-scope definitions.
	ProjectDir string
	// UserDir is the ABSOLUTE definitions directory for the user scope (e.g. "<config home>/agents"); ""
	// (unresolved config home, or no user scope) drops the user scope.
	UserDir string
}

// SubagentScopes lists the scopes this layout supports: project when ProjectDir is set, user when
// UserDir is set.
func (l SubagentLayout) SubagentScopes() []MemoryScope {
	var out []MemoryScope
	if l.ProjectDir != "" {
		out = append(out, MemoryProject)
	}
	if l.UserDir != "" {
		out = append(out, MemoryUser)
	}
	return out
}

// scopeDir resolves the directory holding a scope's definitions, or an error for an unsupported scope.
func (l SubagentLayout) scopeDir(scope MemoryScope, dir string) (string, error) {
	switch scope {
	case MemoryProject:
		if l.ProjectDir == "" {
			return "", fmt.Errorf("agent does not support project-scope subagents")
		}
		if strings.TrimSpace(dir) == "" {
			return "", fmt.Errorf("project scope requires a directory")
		}
		return filepath.Join(dir, l.ProjectDir), nil
	case MemoryUser:
		if l.UserDir == "" {
			return "", fmt.Errorf("agent does not support user-scope subagents")
		}
		return l.UserDir, nil
	default:
		return "", fmt.Errorf("unsupported subagent scope %q", scope)
	}
}

// walkDefs recursively visits every "*.md" file under a scope's directory, calling fn with the parsed
// definition. A missing directory contributes nothing (reads are forgiving). fn may return fs.SkipAll
// to stop early.
func (l SubagentLayout) walkDefs(scope MemoryScope, dir string, fn func(Subagent) error) error {
	base, err := l.scopeDir(scope, dir)
	if err != nil {
		return err
	}
	return filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // missing scope dir (or a vanished entry) → skip
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		meta := parseFrontmatter(string(data))
		return fn(Subagent{
			Name:    subagentName(meta, d.Name()),
			Scope:   scope,
			Path:    path,
			Content: string(data),
			Meta:    meta,
		})
	})
}

// ListSubagents enumerates every definition across the supported scopes with Content omitted (Meta
// populated). A missing directory contributes nothing; any other read error is returned.
func (l SubagentLayout) ListSubagents(dir string) ([]Subagent, error) {
	out := []Subagent{}
	for _, scope := range l.SubagentScopes() {
		err := l.walkDefs(scope, dir, func(s Subagent) error {
			s.Content = "" // list stays cheap: raw body omitted, parsed Meta kept
			out = append(out, s)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ReadSubagent returns the first definition in the scope whose identity (frontmatter name, else filename
// stem) matches name, with full raw Content + parsed Meta. A missing definition returns fs.ErrNotExist.
func (l SubagentLayout) ReadSubagent(scope MemoryScope, dir, name string) (Subagent, error) {
	if err := ValidatePromptName(name); err != nil {
		return Subagent{}, err
	}
	var found *Subagent
	err := l.walkDefs(scope, dir, func(s Subagent) error {
		if s.Name == name {
			hit := s
			found = &hit
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return Subagent{}, err
	}
	if found == nil {
		return Subagent{}, fs.ErrNotExist
	}
	return *found, nil
}

// WriteSubagent writes a definition verbatim to "<scopeDir>/<name>.md" (flat top-level, even though
// reads recurse). The user dir is created (0700 tree); a project dir is created (0755) only when the
// project dir already exists — the project tree itself is never created.
func (l SubagentLayout) WriteSubagent(scope MemoryScope, dir, name, content string) (Subagent, error) {
	if err := ValidatePromptName(name); err != nil {
		return Subagent{}, err
	}
	base, err := l.scopeDir(scope, dir)
	if err != nil {
		return Subagent{}, err
	}
	path := filepath.Join(base, name+".md")
	if filepath.Dir(path) != filepath.Clean(base) {
		return Subagent{}, fmt.Errorf("invalid subagent name %q", name)
	}
	switch scope {
	case MemoryUser:
		if err := os.MkdirAll(base, 0o700); err != nil {
			return Subagent{}, err
		}
	case MemoryProject:
		if _, err := os.Stat(dir); err != nil {
			return Subagent{}, err // missing project dir → error (don't create it)
		}
		if err := os.MkdirAll(base, 0o755); err != nil {
			return Subagent{}, err
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Subagent{}, err
	}
	return Subagent{Name: name, Scope: scope, Path: path, Content: content, Meta: parseFrontmatter(content)}, nil
}

// DeleteSubagent removes the definition in the scope whose identity matches name — the SAME match
// ReadSubagent uses (frontmatter `name`, else filename stem), so a nested file is found and removed.
// Removing an absent definition is not an error (idempotent).
func (l SubagentLayout) DeleteSubagent(scope MemoryScope, dir, name string) error {
	if err := ValidatePromptName(name); err != nil {
		return err
	}
	var target string
	err := l.walkDefs(scope, dir, func(s Subagent) error {
		if s.Name == name {
			target = s.Path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return err
	}
	if target == "" {
		return nil // absent → idempotent success
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
