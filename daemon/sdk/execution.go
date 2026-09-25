package mindwire

import "github.com/oblien/mindwire/daemon/internal/workspaceexec"

// Execution uses the exact native service behind /workspace/{host,files,exec,terminals}.
// All harness views share it; Close terminates its PTYs. No transport is needed in Go.
type WorkspaceExecution = workspaceexec.Service
type WorkspaceHost = workspaceexec.Info
type HostResources = workspaceexec.Resources
type WorkspaceFile = workspaceexec.File
type WorkspaceFileContent = workspaceexec.Content
type WorkspaceExecRequest = workspaceexec.ExecRequest
type WorkspaceExecResult = workspaceexec.ExecResult
type Terminals = workspaceexec.Terminals
type Terminal = workspaceexec.Terminal
type TerminalRequest = workspaceexec.TerminalRequest
type TerminalState = workspaceexec.TerminalState
type TerminalEvent = workspaceexec.TerminalEvent
type TerminalInput = workspaceexec.TerminalInput
