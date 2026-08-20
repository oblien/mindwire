package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// claudeLike is a layout with BOTH project and user scopes for memory and prompts (Claude's shape).
func claudeLike(base string) MemoryLayout {
	return MemoryLayout{
		MemoryFile:       "CLAUDE.md",
		UserBase:         base,
		ProjectPromptDir: ".claude/commands",
		UserPromptDir:    "commands",
	}
}

// codexLike is a layout whose prompt templates are USER-only (Codex's shape): no ProjectPromptDir.
func codexLike(base string) MemoryLayout {
	return MemoryLayout{MemoryFile: "AGENTS.md", UserBase: base, UserPromptDir: "prompts"}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}

func TestValidatePromptName(t *testing.T) {
	for _, ok := range []string{"greet", "review-pr", "release.notes", "under_score"} {
		if err := ValidatePromptName(ok); err != nil {
			t.Errorf("ValidatePromptName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "   ", ".", "..", "a/b", `a\b`, "sub/dir", "../escape"} {
		if err := ValidatePromptName(bad); err == nil {
			t.Errorf("ValidatePromptName(%q) = nil, want error", bad)
		}
	}
}

func TestResolveDir(t *testing.T) {
	// Explicit value wins and is trimmed.
	if got := ResolveDir("  /explicit  ", "/fallback"); got != "/explicit" {
		t.Errorf("explicit: got %q, want /explicit", got)
	}
	// Empty request falls back to the daemon cwd (trimmed).
	if got := ResolveDir("   ", " /fallback "); got != "/fallback" {
		t.Errorf("fallback: got %q, want /fallback", got)
	}
	// Both empty → the process working directory (non-empty on any normal host).
	wd, _ := os.Getwd()
	if got := ResolveDir("", ""); got != wd {
		t.Errorf("process-cwd: got %q, want %q", got, wd)
	}
}

func TestMemoryLayoutMemoryRoundtrip(t *testing.T) {
	base, proj := t.TempDir(), t.TempDir()
	l := claudeLike(base)

	if scopes := l.MemoryScopes(); len(scopes) != 2 || scopes[0] != MemoryProject || scopes[1] != MemoryUser {
		t.Fatalf("MemoryScopes = %v, want [project user]", scopes)
	}

	// An absent file is not an error: Exists=false, Content empty, but Path is always resolved.
	doc, err := l.ReadMemory(MemoryProject, proj)
	if err != nil {
		t.Fatalf("ReadMemory(absent): %v", err)
	}
	if doc.Exists || doc.Content != "" || doc.Path != filepath.Join(proj, "CLAUDE.md") {
		t.Fatalf("absent read = %+v", doc)
	}

	// Project write → read roundtrip, mode 0644.
	if _, err := l.WriteMemory(MemoryProject, proj, "# project memory"); err != nil {
		t.Fatalf("WriteMemory(project): %v", err)
	}
	doc, err = l.ReadMemory(MemoryProject, proj)
	if err != nil || !doc.Exists || doc.Content != "# project memory" {
		t.Fatalf("project roundtrip = %+v err=%v", doc, err)
	}
	assertMode(t, filepath.Join(proj, "CLAUDE.md"), 0o644)

	// User write auto-creates the config home and roundtrips (dir arg ignored for user scope).
	if _, err := l.WriteMemory(MemoryUser, "", "# user memory"); err != nil {
		t.Fatalf("WriteMemory(user): %v", err)
	}
	udoc, err := l.ReadMemory(MemoryUser, "")
	if err != nil || udoc.Content != "# user memory" || udoc.Path != filepath.Join(base, "CLAUDE.md") {
		t.Fatalf("user roundtrip = %+v err=%v", udoc, err)
	}

	// Project scope with an empty dir is a caller error; a write into a missing dir also errors
	// (a memory file never conjures a project tree).
	if _, err := l.ReadMemory(MemoryProject, ""); err == nil {
		t.Error("ReadMemory(project, \"\") should error")
	}
	if _, err := l.WriteMemory(MemoryProject, filepath.Join(proj, "nope"), "x"); err == nil {
		t.Error("WriteMemory into a missing project dir should error")
	}
}

func TestMemoryLayoutPrompts(t *testing.T) {
	base, proj := t.TempDir(), t.TempDir()
	l := claudeLike(base)

	if scopes := l.PromptScopes(); len(scopes) != 2 {
		t.Fatalf("PromptScopes = %v, want project+user", scopes)
	}

	// A missing prompt directory contributes an empty list, never an error.
	list, err := l.ListPrompts(proj)
	if err != nil || len(list) != 0 {
		t.Fatalf("ListPrompts(empty) = %v err=%v", list, err)
	}

	// Project write appends .md and creates the dir under the (existing) project.
	tpl, err := l.WritePrompt(MemoryProject, proj, "greet", "Say hi")
	if err != nil {
		t.Fatalf("WritePrompt(project): %v", err)
	}
	if want := filepath.Join(proj, ".claude", "commands", "greet.md"); tpl.Path != want {
		t.Fatalf("project prompt path = %q, want %q", tpl.Path, want)
	}
	assertMode(t, tpl.Path, 0o644)
	got, err := l.ReadPrompt(MemoryProject, proj, "greet")
	if err != nil || got.Content != "Say hi" {
		t.Fatalf("read project prompt = %+v err=%v", got, err)
	}

	// User write roundtrips too.
	if _, err := l.WritePrompt(MemoryUser, "", "notes", "hello"); err != nil {
		t.Fatalf("WritePrompt(user): %v", err)
	}

	// List spans both scopes with content omitted.
	list, err = l.ListPrompts(proj)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListPrompts(both) = %v err=%v, want 2", list, err)
	}
	for _, p := range list {
		if p.Content != "" {
			t.Errorf("list entry %q should omit content, got %q", p.Name, p.Content)
		}
	}

	// A missing template surfaces fs.ErrNotExist (→ 404 at the API); a traversal name is rejected.
	if _, err := l.ReadPrompt(MemoryUser, "", "nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadPrompt(missing) err = %v, want ErrNotExist", err)
	}
	if _, err := l.ReadPrompt(MemoryProject, proj, "../escape"); err == nil {
		t.Error("traversal prompt name should be rejected")
	}
	// A project prompt write into a missing project dir errors (the tree is never created).
	if _, err := l.WritePrompt(MemoryProject, filepath.Join(proj, "gone"), "x", "y"); err == nil {
		t.Error("project prompt write into a missing dir should error")
	}
}

// TestMemoryLayoutDelete covers the delete lifecycle for both memory files and prompt templates:
// removal actually removes, and deleting an absent target is idempotent (no error).
func TestMemoryLayoutDelete(t *testing.T) {
	base, proj := t.TempDir(), t.TempDir()
	l := claudeLike(base)

	// Deleting an absent memory file is a no-op success and reports Exists=false at the resolved path.
	doc, err := l.DeleteMemory(MemoryProject, proj)
	if err != nil {
		t.Fatalf("DeleteMemory(absent): %v", err)
	}
	if doc.Exists || doc.Path != filepath.Join(proj, "CLAUDE.md") {
		t.Fatalf("DeleteMemory(absent) doc = %+v", doc)
	}

	// Write then delete: the file is gone and a follow-up read reports Exists=false.
	if _, err := l.WriteMemory(MemoryProject, proj, "# mem"); err != nil {
		t.Fatalf("WriteMemory: %v", err)
	}
	if _, err := l.DeleteMemory(MemoryProject, proj); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("memory file still present after delete: %v", err)
	}
	if got, _ := l.ReadMemory(MemoryProject, proj); got.Exists {
		t.Fatal("ReadMemory after delete reports Exists=true")
	}

	// Prompt delete: absent is idempotent; present is removed (a later read → ErrNotExist).
	if err := l.DeletePrompt(MemoryUser, "", "nope"); err != nil {
		t.Fatalf("DeletePrompt(absent): %v", err)
	}
	if _, err := l.WritePrompt(MemoryUser, "", "notes", "hi"); err != nil {
		t.Fatalf("WritePrompt: %v", err)
	}
	if err := l.DeletePrompt(MemoryUser, "", "notes"); err != nil {
		t.Fatalf("DeletePrompt: %v", err)
	}
	if _, err := l.ReadPrompt(MemoryUser, "", "notes"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadPrompt after delete err = %v, want ErrNotExist", err)
	}

	// A traversal name is rejected before touching the filesystem.
	if err := l.DeletePrompt(MemoryUser, "", "../escape"); err == nil {
		t.Error("DeletePrompt(traversal) should be rejected")
	}
}

// A user-only prompt layout (Codex) surfaces exactly one prompt scope and rejects project-scope ops,
// while memory keeps both scopes.
func TestMemoryLayoutUserOnlyPrompts(t *testing.T) {
	base, proj := t.TempDir(), t.TempDir()
	l := codexLike(base)

	if scopes := l.PromptScopes(); len(scopes) != 1 || scopes[0] != MemoryUser {
		t.Fatalf("PromptScopes = %v, want [user]", scopes)
	}
	if scopes := l.MemoryScopes(); len(scopes) != 2 {
		t.Fatalf("MemoryScopes = %v, want project+user", scopes)
	}
	if _, err := l.WritePrompt(MemoryProject, proj, "x", "y"); err == nil {
		t.Error("project-scope prompt write should be unsupported")
	}
	if _, err := l.ReadPrompt(MemoryProject, proj, "x"); err == nil {
		t.Error("project-scope prompt read should be unsupported")
	}
	if _, err := l.WritePrompt(MemoryUser, "", "p", "body"); err != nil {
		t.Fatalf("WritePrompt(user): %v", err)
	}
	got, err := l.ReadPrompt(MemoryUser, "", "p")
	if err != nil || got.Content != "body" || got.Path != filepath.Join(base, "prompts", "p.md") {
		t.Fatalf("user prompt roundtrip = %+v err=%v", got, err)
	}
}
