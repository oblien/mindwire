package mindwire

// register.go is the SDK's ONLY side-effecting file. Blank-importing each adapter and notify channel
// runs its init(), which self-registers into the agent registry (agent.All) and the notify factory
// list (notify.All) — exactly as daemon/cmd/daemon/main.go does before it constructs a supervisor.
// A consumer who imports this package therefore gets every built-in agent and every env-configured
// notification channel wired automatically, with no registration boilerplate of their own.
//
// To host a narrower or custom set of agents, a consumer can instead import the adapters they want
// directly (each still self-registers on import) and skip this package's conveniences — but the common
// path is "import the SDK, get everything".
import (
	_ "github.com/oblien/mindwire/daemon/internal/agent/claude"
	_ "github.com/oblien/mindwire/daemon/internal/agent/codex"
	_ "github.com/oblien/mindwire/daemon/internal/agent/opencode"
	_ "github.com/oblien/mindwire/daemon/internal/notify/exec"
	_ "github.com/oblien/mindwire/daemon/internal/notify/file"
)
