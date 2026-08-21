package grok

import (
	"os"
	"path/filepath"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Grok Build reads AGENTS.md from the workspace and ~/.grok/AGENTS.md globally.
// The shared layout gives that native convention the unified memory API without
// inventing a second store.
var (
	_ agent.MemoryModule  = adapter{}
	_ agent.PromptsModule = adapter{}
)

func (adapter) memLayout() agent.MemoryLayout {
	return agent.MemoryLayout{MemoryFile: "AGENTS.md", UserBase: configBase()}
}
func (a adapter) MemoryScopes() []agent.MemoryScope { return a.memLayout().MemoryScopes() }
func (a adapter) ReadMemory(scope agent.MemoryScope, dir string) (agent.MemoryDoc, error) {
	return a.memLayout().ReadMemory(scope, dir)
}
func (a adapter) WriteMemory(scope agent.MemoryScope, dir, content string) (agent.MemoryDoc, error) {
	return a.memLayout().WriteMemory(scope, dir, content)
}
func (a adapter) DeleteMemory(scope agent.MemoryScope, dir string) (agent.MemoryDoc, error) {
	return a.memLayout().DeleteMemory(scope, dir)
}

// Grok discovers user-invocable commands from ~/.agents/commands. They are
// Markdown prompt files, exactly the contract represented by PromptsModule.
// This is deliberately a separate layout from AGENTS.md: GROK_HOME controls
// Grok's own configuration, while the documented compatible command location
// is home-relative.
func (adapter) promptLayout() agent.MemoryLayout {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return agent.MemoryLayout{}
	}
	return agent.MemoryLayout{UserBase: filepath.Join(home, ".agents"), UserPromptDir: "commands"}
}

func (a adapter) PromptScopes() []agent.MemoryScope { return a.promptLayout().PromptScopes() }
func (a adapter) ListPrompts(dir string) ([]agent.PromptTemplate, error) {
	return a.promptLayout().ListPrompts(dir)
}
func (a adapter) ReadPrompt(scope agent.MemoryScope, dir, name string) (agent.PromptTemplate, error) {
	return a.promptLayout().ReadPrompt(scope, dir, name)
}
func (a adapter) WritePrompt(scope agent.MemoryScope, dir, name, content string) (agent.PromptTemplate, error) {
	return a.promptLayout().WritePrompt(scope, dir, name, content)
}
func (a adapter) DeletePrompt(scope agent.MemoryScope, dir, name string) error {
	return a.promptLayout().DeletePrompt(scope, dir, name)
}
