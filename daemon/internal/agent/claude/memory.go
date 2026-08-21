package claude

import "github.com/oblien/mindwire/daemon/internal/agent"

// Persistent prompt/memory surface for Claude Code, exposed through the two optional core modules
// (type-asserted by the API, never part of the mandatory Adapter interface):
//
//   - memory file — CLAUDE.md, at project scope (<dir>/CLAUDE.md) and user scope (<base>/CLAUDE.md);
//   - prompt templates — custom slash-commands, at project scope (<dir>/.claude/commands/<name>.md)
//     and user scope (<base>/commands/<name>.md).
//
// All the IO lives in agent.MemoryLayout; the adapter just builds the layout per call (so a test that
// sets CLAUDE_CONFIG_DIR after init is honored) and delegates. None of these files is a secret — they
// are plain user content at the same trust level as a working directory.
var (
	_ agent.MemoryModule  = adapter{}
	_ agent.PromptsModule = adapter{}
)

// memLayout describes where Claude's memory + template files live. configBase() resolves
// CLAUDE_CONFIG_DIR (else ~/.claude) lazily, matching ConfigPath/History.
func (adapter) memLayout() agent.MemoryLayout {
	return agent.MemoryLayout{
		MemoryFile:       "CLAUDE.md",
		UserBase:         configBase(),
		ProjectPromptDir: ".claude/commands",
		UserPromptDir:    "commands",
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
