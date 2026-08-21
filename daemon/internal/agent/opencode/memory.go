package opencode

import (
	"path/filepath"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Persistent prompt/memory surface for opencode, exposed through the two optional core modules
// (type-asserted by the API, never part of the mandatory Adapter interface):
//
//   - memory file — AGENTS.md, at project scope (<dir>/AGENTS.md) and user scope (<configDir>/AGENTS.md);
//   - prompt templates — opencode "commands", at BOTH project scope (<dir>/.opencode/command/<name>.md)
//     and user scope (<configDir>/command/<name>.md). Unlike codex, opencode has a project-scope command
//     convention, so PromptScopes() includes project.
//
// All the IO lives in agent.MemoryLayout; the adapter builds the layout per call (so a test setting
// XDG_CONFIG_HOME after init is honored) and delegates. None of these files is a secret.
var (
	_ agent.MemoryModule  = adapter{}
	_ agent.PromptsModule = adapter{}
)

// memLayout describes where opencode's memory + command files live. configDir() resolves
// $XDG_CONFIG_HOME/opencode (else ~/.config/opencode) lazily, matching ConfigPath. The command
// directory name is opencode's own convention (SINGULAR "command").
func (adapter) memLayout() agent.MemoryLayout {
	return agent.MemoryLayout{
		MemoryFile:       "AGENTS.md",
		UserBase:         configDir(),
		ProjectPromptDir: filepath.Join(".opencode", "command"),
		UserPromptDir:    "command",
	}
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

func (a adapter) PromptScopes() []agent.MemoryScope { return a.memLayout().PromptScopes() }

func (a adapter) ListPrompts(dir string) ([]agent.PromptTemplate, error) {
	return a.memLayout().ListPrompts(dir)
}

func (a adapter) ReadPrompt(scope agent.MemoryScope, dir, name string) (agent.PromptTemplate, error) {
	return a.memLayout().ReadPrompt(scope, dir, name)
}

func (a adapter) WritePrompt(scope agent.MemoryScope, dir, name, content string) (agent.PromptTemplate, error) {
	return a.memLayout().WritePrompt(scope, dir, name, content)
}

func (a adapter) DeletePrompt(scope agent.MemoryScope, dir, name string) error {
	return a.memLayout().DeletePrompt(scope, dir, name)
}
