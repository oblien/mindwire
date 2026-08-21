package opencode

import (
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestOpencodeMemoryModule verifies the adapter wires the shared MemoryLayout to opencode's paths:
// AGENTS.md at both scopes, and saved commands (prompt templates) at BOTH project and user scope — the
// latter is what distinguishes opencode from codex. Config home is a temp XDG_CONFIG_HOME so
// configDir() resolves to <tmp>/opencode. Edge-case IO lives in agent/memfile_test.go.
func TestOpencodeMemoryModule(t *testing.T) {
	xdg, proj := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	cfg := filepath.Join(xdg, "opencode") // configDir()
	a := adapter{}

	if caps := a.Capabilities(); !caps.Memory || !caps.PromptTemplates {
		t.Fatalf("opencode caps: Memory=%v PromptTemplates=%v, want both true", caps.Memory, caps.PromptTemplates)
	}

	// Memory: both scopes; AGENTS.md at project <dir>/ and user <configDir>/.
	if scopes := a.MemoryScopes(); len(scopes) != 2 {
		t.Fatalf("MemoryScopes = %v, want project+user", scopes)
	}
	if _, err := a.WriteMemory(agent.MemoryProject, proj, "# agents"); err != nil {
		t.Fatalf("WriteMemory(project): %v", err)
	}
	if doc, err := a.ReadMemory(agent.MemoryProject, proj); err != nil ||
		doc.Content != "# agents" || doc.Path != filepath.Join(proj, "AGENTS.md") {
		t.Fatalf("project memory = %+v err=%v", doc, err)
	}
	if doc, err := a.WriteMemory(agent.MemoryUser, proj, "# user"); err != nil ||
		doc.Path != filepath.Join(cfg, "AGENTS.md") {
		t.Fatalf("user memory = %+v err=%v", doc, err)
	}

	// Prompts (commands): BOTH scopes, unlike codex. Project lands under <dir>/.opencode/command/.
	scopes := a.PromptScopes()
	if len(scopes) != 2 {
		t.Fatalf("PromptScopes = %v, want project+user", scopes)
	}
	pt, err := a.WritePrompt(agent.MemoryProject, proj, "deploy", "run the deploy")
	if err != nil || pt.Path != filepath.Join(proj, ".opencode", "command", "deploy.md") {
		t.Fatalf("WritePrompt(project) = %+v err=%v", pt, err)
	}
	if got, err := a.ReadPrompt(agent.MemoryProject, proj, "deploy"); err != nil || got.Content != "run the deploy" {
		t.Fatalf("ReadPrompt(project) = %+v err=%v", got, err)
	}
	// User lands under <configDir>/command/.
	up, err := a.WritePrompt(agent.MemoryUser, proj, "note", "body")
	if err != nil || up.Path != filepath.Join(cfg, "command", "note.md") {
		t.Fatalf("WritePrompt(user) = %+v err=%v", up, err)
	}
	list, err := a.ListPrompts(proj)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListPrompts = %v err=%v, want 2 (project+user)", list, err)
	}
}
