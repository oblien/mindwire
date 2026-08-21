package grok

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestMemoryUsesNativeGrokAgentsFiles(t *testing.T) {
	t.Setenv("GROK_HOME", t.TempDir())
	project := t.TempDir()
	a := adapter{}
	if !a.Capabilities().Memory {
		t.Fatal("Memory=false")
	}
	user, err := a.WriteMemory(agent.MemoryUser, project, "global rules")
	if err != nil || user.Path != filepath.Join(configBase(), "AGENTS.md") {
		t.Fatalf("user memory = %#v, %v", user, err)
	}
	proj, err := a.WriteMemory(agent.MemoryProject, project, "project rules")
	if err != nil || proj.Path != filepath.Join(project, "AGENTS.md") {
		t.Fatalf("project memory = %#v, %v", proj, err)
	}
}

func TestPromptsUseNativeAgentsCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := adapter{}
	if !a.Capabilities().PromptTemplates {
		t.Fatal("PromptTemplates=false")
	}
	p, err := a.WritePrompt(agent.MemoryUser, "", "review", "Review this change.")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".agents", "commands", "review.md")
	if p.Path != want {
		t.Fatalf("path = %q, want %q", p.Path, want)
	}
	if got, err := os.ReadFile(want); err != nil || string(got) != p.Content {
		t.Fatalf("prompt content = %q, err = %v", got, err)
	}
	if err := a.DeletePrompt(agent.MemoryUser, "", "review"); err != nil {
		t.Fatal(err)
	}
}
