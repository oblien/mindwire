package claude

import (
	"path/filepath"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Persistent subagent-definition surface for Claude Code, exposed through the optional
// agent.SubagentsModule (type-asserted by the API, never part of the mandatory Adapter). Definitions
// are .claude/agents/<name>.md files at project scope (<dir>/.claude/agents) and user scope
// (<base>/agents). This is DISTINCT from the per-turn --agents passthrough (Capabilities.Subagents):
// here we read/write the on-disk definition store.
//
// All the IO lives in agent.SubagentLayout; the adapter just builds the layout per call (so a test that
// sets CLAUDE_CONFIG_DIR after init is honored) and delegates. These are plain user content at the same
// trust level as a working directory — no secrets.
var _ agent.SubagentsModule = adapter{}

// subLayout describes where Claude's subagent definitions live. configBase() resolves CLAUDE_CONFIG_DIR
// (else ~/.claude) lazily, matching memLayout/ConfigPath/History.
func (adapter) subLayout() agent.SubagentLayout {
	l := agent.SubagentLayout{ProjectDir: filepath.Join(".claude", "agents")}
	if base := configBase(); base != "" {
		l.UserDir = filepath.Join(base, "agents")
	}
	return l
}

func (a adapter) SubagentScopes() []agent.MemoryScope { return a.subLayout().SubagentScopes() }

func (a adapter) ListSubagents(dir string) ([]agent.Subagent, error) {
	return a.subLayout().ListSubagents(dir)
}

func (a adapter) ReadSubagent(scope agent.MemoryScope, dir, name string) (agent.Subagent, error) {
	return a.subLayout().ReadSubagent(scope, dir, name)
}

func (a adapter) WriteSubagent(scope agent.MemoryScope, dir, name, content string) (agent.Subagent, error) {
	return a.subLayout().WriteSubagent(scope, dir, name, content)
}

func (a adapter) DeleteSubagent(scope agent.MemoryScope, dir, name string) error {
	return a.subLayout().DeleteSubagent(scope, dir, name)
}
