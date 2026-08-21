// Package driver is how the daemon DRIVES an agent for a turn — the control channel between the
// daemon and the agent, orthogonal to the agent's output format. An adapter's RunStream composes
// the driver matching its declared Protocol (agent.Capabilities.Protocol):
//
//   - CLI (here)  — spawn the agent's command line per turn and stream its stdout. The UNIVERSAL
//     FALLBACK: any agent that ships only a CLI uses it (Claude: `claude -p`).
//   - HTTP        — an agent with a native REST/SSE API implements Driver directly, calling its API.
//   - Persistent  — a long-lived process spoken to over a protocol implements Driver directly.
//
// The daemon core never dispatches on Protocol; it only ever sees unified agent.Events via `emit`.
// The adapter picks the driver. Adding a protocol-native agent = a new Driver impl, no core change.
package driver

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
)

// Driver runs one turn to unified events, however the agent is best driven.
type Driver interface {
	Run(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error)
}

// CLI drives an agent by running a shell command (via `bash -lc`) and handing its stdout to a
// per-agent Parse function that emits unified events. It owns the generic plumbing — process spawn,
// env, stderr capture, and the "no terminal result" error fallback — so a CLI-based adapter only
// supplies the command + the stream parser.
type CLI struct {
	Command string            // the shell command to run (already assembled from the turn)
	Env     map[string]string // extra env exported to the process (auth tokens, etc.)
	// Parse consumes the command's stdout, emits unified events, and returns the final result plus
	// whether a terminal result was seen. got=false ⇒ CLI surfaces stderr/exit as the error.
	Parse func(stdout io.Reader, emit agent.Emit) (agent.TurnResult, bool)
}

var _ Driver = CLI{}

func (c CLI) Run(ctx context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", c.Command)
	// Cancel/timeout must kill the whole tree — bash spawns the real agent (node/claude/codex);
	// killing only bash would leave it running, burning budget and mutating the workspace.
	proc.Group(cmd)
	// Auth/env goes through the process environment, not the shell string — no quoting, no injection.
	cmd.Env = os.Environ()
	for k, v := range c.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	// Expose this turn's process group to resource monitoring (no-op if none is wired on ctx).
	proc.Report(ctx, cmd.Process.Pid)

	result, got := c.Parse(stdout, emit)
	werr := cmd.Wait()
	return finish(result, got, stderr.String(), werr, emit)
}

// Persistent drives an agent over a long-lived process with an OPEN stdin, for a turn that pauses
// for the user mid-flight (permission approvals, a follow-up message, an interrupt). It owns the same
// generic plumbing as CLI — spawn, env, the reused stdout Parse, stderr fallback — plus the wiring CLI
// lacks: a StdinPipe and a single writer goroutine that first emits the Preamble (the control-protocol
// handshake + first user message) and then pumps each Inbound to stdin as one NDJSON line via Encode.
//
// Lifecycle is still ONE turn per process (no cross-turn reuse): with a stream-json input the CLI keeps
// stdin open waiting for more, so the driver closes stdin as soon as the parser emits the turn's
// terminal result — the process then exits and Parse returns on stdout EOF, exactly like CLI.
type Persistent struct {
	Command string            // the shell command to run (assembled without the message; it goes in Preamble)
	Env     map[string]string // extra env exported to the process (auth tokens, etc.)
	// Parse consumes the command's stdout, emits unified events, and returns the final result plus
	// whether a terminal result was seen — the SAME parser CLI uses (reused, not reimplemented).
	Parse func(stdout io.Reader, emit agent.Emit) (agent.TurnResult, bool)
	// Encode maps one Inbound to a single NDJSON line for stdin. ok=false ⇒ the message is skipped
	// (nothing to send). The adapter owns the native wire shape; the driver only frames + flushes it.
	Encode func(agent.Inbound) ([]byte, bool)
	// Preamble is written to stdin verbatim (one line each) before the pump starts — the initialize
	// handshake that arms bidirectional control routing, then the first user message.
	Preamble [][]byte
	// Inbound is the user's mid-turn ingress for this turn; the writer selects on it until the turn's
	// result is seen (stdin closed) or the context is cancelled.
	Inbound <-chan agent.Inbound
}

var _ Driver = Persistent{}

func (p Persistent) Run(ctx context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", p.Command)
	proc.Group(cmd) // kill the whole tree on cancel/interrupt, not just the bash parent
	cmd.Env = os.Environ()
	for k, v := range p.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return agent.TurnResult{Text: err.Error(), IsError: true}, err
	}
	// Expose this turn's process group to resource monitoring (no-op if none is wired on ctx).
	proc.Report(ctx, cmd.Process.Pid)

	// The turn's terminal result closes `done` (via the emit wrapper below); the writer then closes
	// stdin so the CLI — which would otherwise block reading stdin for another turn — exits, and Parse
	// returns on stdout EOF. This keeps the CLI hot-path lifecycle (one process = one turn) intact.
	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }

	go func() {
		w := bufio.NewWriter(stdin)
		writeLine := func(b []byte) bool {
			if _, e := w.Write(b); e != nil {
				return false
			}
			if e := w.WriteByte('\n'); e != nil {
				return false
			}
			return w.Flush() == nil
		}
		for _, line := range p.Preamble {
			if !writeLine(line) {
				break
			}
		}
		for {
			select {
			case <-ctx.Done():
				_ = stdin.Close()
				return
			case <-done:
				_ = stdin.Close() // turn's result seen → EOF stdin so the process finishes
				return
			case in, ok := <-p.Inbound:
				if !ok {
					_ = stdin.Close()
					return
				}
				if line, ok := p.Encode(in); ok {
					if !writeLine(line) {
						_ = stdin.Close()
						return
					}
				}
			}
		}
	}()

	// Wrap emit so the driver notices the terminal result without parsing the stream itself.
	wrapped := func(ev agent.Event) {
		emit(ev)
		if ev.Type == agent.EventResult {
			closeDone()
		}
	}

	result, got := p.Parse(stdout, wrapped)
	closeDone() // stdout ended without a result (error/EOF) → unblock the writer too
	werr := cmd.Wait()
	return finish(result, got, stderr.String(), werr, emit)
}

// finish is the shared terminal-result / stderr-fallback logic for both drivers: on a seen result it
// returns it; otherwise it surfaces stderr (then the wait error, then the parser's text) as the error.
func finish(result agent.TurnResult, got bool, stderrStr string, werr error, emit agent.Emit) (agent.TurnResult, error) {
	if !got {
		msg := strings.TrimSpace(stderrStr)
		if msg == "" && werr != nil {
			msg = werr.Error()
		}
		if msg == "" {
			msg = result.Text // scanner read error / "no result", surfaced by the parser
		}
		if msg == "" {
			msg = "no result from agent"
		}
		emit(agent.Event{Type: agent.EventError, Error: msg})
		return agent.TurnResult{Text: msg, IsError: true}, werr
	}
	return result, nil
}
