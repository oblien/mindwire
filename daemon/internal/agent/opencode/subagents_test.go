package opencode

import (
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// TestOpencodeSubagentsModule verifies the adapter wires agent.SubagentLayout to opencode's "agent"
// definition dirs (<dir>/.opencode/agent project, <configDir>/agent user), advertises the capability,
// and round-trips a definition with its parsed Meta. Config home is a temp XDG_CONFIG_HOME so
// configDir() resolves to <tmp>/opencode. Edge-case IO lives in agent/subagentfile_test.go.
func TestOpencodeSubagentsModule(t *testing.T) {
	xdg, proj := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	cfg := filepath.Join(xdg, "opencode")
	a := adapter{}

	if !a.Capabilities().SubagentDefs {
		t.Fatal("opencode caps: SubagentDefs=false, want true")
	}
	if scopes := a.SubagentScopes(); len(scopes) != 2 {
		t.Fatalf("SubagentScopes = %v, want project+user", scopes)
	}

	// Project scope lands under <dir>/.opencode/agent/<name>.md with parsed frontmatter.
	sub, err := a.WriteSubagent(agent.MemoryProject, proj, "reviewer",
		"---\nname: reviewer\ndescription: Reviews code\ntools: read, grep\n---\nBe thorough.")
	if err != nil || sub.Path != filepath.Join(proj, ".opencode", "agent", "reviewer.md") {
		t.Fatalf("WriteSubagent(project) = %+v err=%v", sub, err)
	}
	if sub.Meta == nil || sub.Meta.Description != "Reviews code" {
		t.Fatalf("parsed Meta = %+v, want description", sub.Meta)
	}
	if got, err := a.ReadSubagent(agent.MemoryProject, proj, "reviewer"); err != nil || got.Content != sub.Content {
		t.Fatalf("ReadSubagent(project) = %+v err=%v", got, err)
	}

	// User scope lands under <configDir>/agent/<name>.md.
	us, err := a.WriteSubagent(agent.MemoryUser, proj, "notes", "---\nname: notes\n---\nx")
	if err != nil || us.Path != filepath.Join(cfg, "agent", "notes.md") {
		t.Fatalf("WriteSubagent(user) = %+v err=%v", us, err)
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
