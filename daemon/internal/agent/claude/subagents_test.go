package claude

import (
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestClaudeSubagentsModule verifies the adapter wires agent.SubagentLayout to Claude's paths
// (<dir>/.claude/agents project, <base>/agents user), advertises the capability, and round-trips a
// definition with its parsed Meta. Edge-case IO lives in agent/subagentfile_test.go; this asserts the
// Claude-specific wiring against a temp home + project.
func TestClaudeSubagentsModule(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	a := adapter{}

	if !a.Capabilities().SubagentDefs {
		t.Fatal("claude caps: SubagentDefs=false, want true")
	}
	if scopes := a.SubagentScopes(); len(scopes) != 2 {
		t.Fatalf("SubagentScopes = %v, want project+user", scopes)
	}

	// Project scope lands under <dir>/.claude/agents/<name>.md.
	sub, err := a.WriteSubagent(agent.MemoryProject, proj, "reviewer",
		"---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\nBe thorough.")
	if err != nil || sub.Path != filepath.Join(proj, ".claude", "agents", "reviewer.md") {
		t.Fatalf("WriteSubagent(project) = %+v err=%v", sub, err)
	}
	if sub.Meta == nil || sub.Meta.Description != "Reviews code" || len(sub.Meta.Tools) != 2 {
		t.Fatalf("parsed Meta = %+v, want description + 2 tools", sub.Meta)
	}
	got, err := a.ReadSubagent(agent.MemoryProject, proj, "reviewer")
	if err != nil || got.Content != sub.Content {
		t.Fatalf("ReadSubagent(project) = %+v err=%v", got, err)
	}

	// User scope lands under <home>/agents/<name>.md.
	if _, err := a.WriteSubagent(agent.MemoryUser, proj, "notes", "---\nname: notes\n---\nx"); err != nil {
		t.Fatalf("WriteSubagent(user): %v", err)
	}
	list, err := a.ListSubagents(proj)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListSubagents = %v err=%v, want 2 (project+user)", list, err)
	}
	for _, s := range list {
		if s.Content != "" {
			t.Errorf("list entry %q leaked Content", s.Name)
		}
	}
}
