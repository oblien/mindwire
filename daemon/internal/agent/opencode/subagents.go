package opencode

import (
	"path/filepath"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Persistent subagent-definition surface for opencode, exposed through the optional
// agent.SubagentsModule (type-asserted by the API, never part of the mandatory Adapter). opencode calls
// these "agents": <name>.md files with YAML frontmatter at project scope (<dir>/.opencode/agent) and
// user scope (<configDir>/agent). The directory name is opencode's own convention (SINGULAR "agent").
//
// All the IO lives in agent.SubagentLayout; the adapter builds the layout per call (so a test setting
// XDG_CONFIG_HOME after init is honored) and delegates. These are plain user content — no secrets.
var _ agent.SubagentsModule = adapter{}

// subLayout describes where opencode's agent definitions live. configDir() resolves
// $XDG_CONFIG_HOME/opencode (else ~/.config/opencode) lazily, matching memLayout/ConfigPath.
func (adapter) subLayout() agent.SubagentLayout {
	l := agent.SubagentLayout{ProjectDir: filepath.Join(".opencode", "agent")}
	if base := configDir(); base != "" {
		l.UserDir = filepath.Join(base, "agent")
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
