package claude

import (
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestClaudeMemoryModule verifies the adapter wires the shared MemoryLayout to Claude's paths and
// advertises the capability flags. The exhaustive edge-case coverage lives in agent/memfile_test.go;
// this asserts the wiring (file names, both scopes, project slash-command subdir) against a temp home.
func TestClaudeMemoryModule(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	a := adapter{}

	if caps := a.Capabilities(); !caps.Memory || !caps.PromptTemplates {
		t.Fatalf("claude caps: Memory=%v PromptTemplates=%v, want both true", caps.Memory, caps.PromptTemplates)
	}

	// Memory: both scopes, CLAUDE.md at project <dir>/ and user <home>/.
	if scopes := a.MemoryScopes(); len(scopes) != 2 {
		t.Fatalf("MemoryScopes = %v, want project+user", scopes)
	}
	if _, err := a.WriteMemory(agent.MemoryProject, proj, "# project"); err != nil {
		t.Fatalf("WriteMemory(project): %v", err)
	}
	if doc, err := a.ReadMemory(agent.MemoryProject, proj); err != nil ||
		doc.Content != "# project" || doc.Path != filepath.Join(proj, "CLAUDE.md") {
		t.Fatalf("project memory = %+v err=%v", doc, err)
	}
	if _, err := a.WriteMemory(agent.MemoryUser, proj, "# user"); err != nil {
		t.Fatalf("WriteMemory(user): %v", err)
	}
	if doc, err := a.ReadMemory(agent.MemoryUser, proj); err != nil ||
		doc.Content != "# user" || doc.Path != filepath.Join(home, "CLAUDE.md") {
		t.Fatalf("user memory = %+v err=%v", doc, err)
	}

	// Prompts: project slash-command under <dir>/.claude/commands, user under <home>/commands.
	tpl, err := a.WritePrompt(agent.MemoryProject, proj, "greet", "Say hi")
	if err != nil || tpl.Path != filepath.Join(proj, ".claude", "commands", "greet.md") {
		t.Fatalf("project prompt = %+v err=%v", tpl, err)
	}
	if got, err := a.ReadPrompt(agent.MemoryProject, proj, "greet"); err != nil || got.Content != "Say hi" {
		t.Fatalf("read project prompt = %+v err=%v", got, err)
	}
	if _, err := a.WritePrompt(agent.MemoryUser, proj, "notes", "n"); err != nil {
		t.Fatalf("WritePrompt(user): %v", err)
	}
	if list, err := a.ListPrompts(proj); err != nil || len(list) != 2 {
		t.Fatalf("ListPrompts = %v err=%v, want 2 (project+user)", list, err)
	}
}
