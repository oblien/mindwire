package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MemoryLayout describes WHERE one adapter's persistent memory and prompt-template files live, so the
// read/write/list logic is shared (DRY) across adapters — the same philosophy as helpers.go. Its
// methods implement the whole MemoryModule + PromptsModule surface; an adapter builds a layout PER
// CALL (resolving its config home lazily, so a test that sets CLAUDE_CONFIG_DIR / CODEX_HOME after
// init sees the change) and delegates each module method straight to the matching layout method.
type MemoryLayout struct {
	// MemoryFile is the basename of the persistent memory file (e.g. "CLAUDE.md", "AGENTS.md"), placed
	// directly in the project dir (project scope) or the user config home (user scope).
	MemoryFile string
	// UserBase is the agent's user config home (e.g. ~/.claude, ~/.codex); "" when it can't be resolved
	// (no home), which drops the user scope.
	UserBase string
	// ProjectPromptDir is the prompt-template directory RELATIVE to a project dir (e.g.
	// ".claude/commands"); "" means the agent has no project-scope templates.
	ProjectPromptDir string
	// UserPromptDir is the prompt-template directory relative to UserBase (e.g. "commands", "prompts");
	// "" means no user-scope templates.
	UserPromptDir string
}

// ---- MemoryModule -----------------------------------------------------------

// MemoryScopes lists the memory scopes this layout supports: project whenever a memory file is
// declared, user additionally when the config home resolved.
func (l MemoryLayout) MemoryScopes() []MemoryScope {
	if l.MemoryFile == "" {
		return nil
	}
	out := []MemoryScope{MemoryProject}
	if l.UserBase != "" {
		out = append(out, MemoryUser)
	}
	return out
}

// memoryPath resolves the memory file path for a scope, or an error for an unsupported scope / missing
// inputs (empty project dir, unresolved config home).
func (l MemoryLayout) memoryPath(scope MemoryScope, dir string) (string, error) {
	if l.MemoryFile == "" {
		return "", fmt.Errorf("agent has no memory file")
	}
	switch scope {
	case MemoryProject:
		if strings.TrimSpace(dir) == "" {
			return "", fmt.Errorf("project scope requires a directory")
		}
		return filepath.Join(dir, l.MemoryFile), nil
	case MemoryUser:
		if l.UserBase == "" {
			return "", fmt.Errorf("cannot resolve user config directory")
		}
		return filepath.Join(l.UserBase, l.MemoryFile), nil
	default:
		return "", fmt.Errorf("unsupported memory scope %q", scope)
	}
}

// ReadMemory returns the scope's memory doc. A missing file is not an error (Exists=false).
func (l MemoryLayout) ReadMemory(scope MemoryScope, dir string) (MemoryDoc, error) {
	path, err := l.memoryPath(scope, dir)
	if err != nil {
		return MemoryDoc{}, err
	}
	doc := MemoryDoc{Scope: scope, Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return doc, nil // absent: Exists=false, Content=""
		}
		return MemoryDoc{}, err
	}
	doc.Exists = true
	doc.Content = string(data)
	return doc, nil
}

// WriteMemory writes content to the scope's memory file. For user scope the config home is created
// (0700); for project scope the project dir must already exist (a memory file is never a reason to
// create a project tree), so a missing dir surfaces as an error.
func (l MemoryLayout) WriteMemory(scope MemoryScope, dir, content string) (MemoryDoc, error) {
	path, err := l.memoryPath(scope, dir)
	if err != nil {
		return MemoryDoc{}, err
	}
	if scope == MemoryUser {
		if err := os.MkdirAll(l.UserBase, 0o700); err != nil {
			return MemoryDoc{}, err
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return MemoryDoc{}, err
	}
	return MemoryDoc{Scope: scope, Path: path, Exists: true, Content: content}, nil
}

// DeleteMemory removes the scope's memory file and returns the resulting doc (Exists=false). Removing
// an absent file is not an error (idempotent) — the post-state is "no memory" either way.
func (l MemoryLayout) DeleteMemory(scope MemoryScope, dir string) (MemoryDoc, error) {
	path, err := l.memoryPath(scope, dir)
	if err != nil {
		return MemoryDoc{}, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return MemoryDoc{}, err
	}
	return MemoryDoc{Scope: scope, Path: path}, nil
}

// ---- PromptsModule ----------------------------------------------------------

// PromptScopes lists the prompt-template scopes this layout supports: project when ProjectPromptDir is
// set, user when UserPromptDir is set and the config home resolved.
func (l MemoryLayout) PromptScopes() []MemoryScope {
	var out []MemoryScope
	if l.ProjectPromptDir != "" {
		out = append(out, MemoryProject)
	}
	if l.UserPromptDir != "" && l.UserBase != "" {
		out = append(out, MemoryUser)
	}
	return out
}

// promptDir resolves the directory holding a scope's templates, or an error for an unsupported scope.
func (l MemoryLayout) promptDir(scope MemoryScope, dir string) (string, error) {
	switch scope {
	case MemoryProject:
		if l.ProjectPromptDir == "" {
			return "", fmt.Errorf("agent does not support project-scope prompt templates")
		}
		if strings.TrimSpace(dir) == "" {
			return "", fmt.Errorf("project scope requires a directory")
		}
		return filepath.Join(dir, l.ProjectPromptDir), nil
	case MemoryUser:
		if l.UserPromptDir == "" {
			return "", fmt.Errorf("agent does not support user-scope prompt templates")
		}
		if l.UserBase == "" {
			return "", fmt.Errorf("cannot resolve user config directory")
		}
		return filepath.Join(l.UserBase, l.UserPromptDir), nil
	default:
		return "", fmt.Errorf("unsupported prompt scope %q", scope)
	}
}

// ListPrompts enumerates every "*.md" template across the supported scopes with Content omitted. A
// missing directory contributes nothing (reads are forgiving); any other read error is returned.
func (l MemoryLayout) ListPrompts(dir string) ([]PromptTemplate, error) {
	out := []PromptTemplate{}
	for _, scope := range l.PromptScopes() {
		base, err := l.promptDir(scope, dir)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // no directory yet → no templates for this scope
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			out = append(out, PromptTemplate{
				Name:  strings.TrimSuffix(e.Name(), ".md"),
				Scope: scope,
				Path:  filepath.Join(base, e.Name()),
			})
		}
	}
	return out, nil
}

// promptPath validates the name, resolves the scope's directory, and joins "<name>.md" — asserting
// the result stays directly inside that directory (defense-in-depth against traversal).
func (l MemoryLayout) promptPath(scope MemoryScope, dir, name string) (string, string, error) {
	if err := ValidatePromptName(name); err != nil {
		return "", "", err
	}
	base, err := l.promptDir(scope, dir)
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(base, name+".md")
	if filepath.Dir(path) != filepath.Clean(base) {
		return "", "", fmt.Errorf("invalid prompt name %q", name)
	}
	return base, path, nil
}

// ReadPrompt returns one template's full content. A missing file bubbles up an fs.ErrNotExist error.
func (l MemoryLayout) ReadPrompt(scope MemoryScope, dir, name string) (PromptTemplate, error) {
	_, path, err := l.promptPath(scope, dir, name)
	if err != nil {
		return PromptTemplate{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PromptTemplate{}, err // ErrNotExist bubbles → 404 at the API
	}
	return PromptTemplate{Name: name, Scope: scope, Path: path, Content: string(data)}, nil
}

// WritePrompt writes a template. The user prompt dir is created (0700 tree); a project prompt dir is
// created (0755) only when the project dir already exists — the project tree itself is never created.
func (l MemoryLayout) WritePrompt(scope MemoryScope, dir, name, content string) (PromptTemplate, error) {
	base, path, err := l.promptPath(scope, dir, name)
	if err != nil {
		return PromptTemplate{}, err
	}
	switch scope {
	case MemoryUser:
		if err := os.MkdirAll(base, 0o700); err != nil {
			return PromptTemplate{}, err
		}
	case MemoryProject:
		if _, err := os.Stat(dir); err != nil {
			return PromptTemplate{}, err // missing project dir → error (don't create it)
		}
		if err := os.MkdirAll(base, 0o755); err != nil {
			return PromptTemplate{}, err
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return PromptTemplate{}, err
	}
	return PromptTemplate{Name: name, Scope: scope, Path: path, Content: content}, nil
}

// DeletePrompt removes one template. Removing an absent template (or from a missing directory) is not
// an error (idempotent) — the post-state is "no such template" either way.
func (l MemoryLayout) DeletePrompt(scope MemoryScope, dir, name string) error {
	_, path, err := l.promptPath(scope, dir, name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
