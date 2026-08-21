package codex

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// --- Stage-7 hard gate: live app-server smoke ---------------------------------------------------
//
// These drive the REAL `codex app-server` binary end to end against a live, authenticated session —
// the one thing the scripted TestAppServerApprovalFlow (in-memory pipes) structurally cannot prove.
// They are skipped unless CODEX_LIVE=1 because they need a working Codex credential (an env key or a
// fresh ChatGPT login) and network. They still COMPILE in CI, so any drift in appServer's fields or
// the agent contracts is caught by `go build`/`go vet`; only the runtime is gated.
//
// The handshake layer (initialize → initialized → thread/start, with response correlation and the
// thread/started notification) is already verified live against codex-cli 0.146.0; these tests close
// the remaining surface: turn/start → item/* notifications → an approval ServerRequest → the respond
// round-trip (turn/steer / turn/interrupt via the same inbound pump).
//
// Run with, e.g.:
//   CODEX_LIVE=1 CODEX_API_KEY=sk-... go test -run TestAppServerLive ./internal/agent/codex/ -v
// or, with a fresh `codex login`, just:
//   CODEX_LIVE=1 go test -run TestAppServerLive ./internal/agent/codex/ -v

func liveSecrets() map[string]string {
	env := map[string]string{}
	for _, k := range []string{
		"CODEX_API_KEY", "OPENAI_API_KEY", "CODEX_ACCESS_TOKEN",
		"OPENAI_BASE_URL", "OPENAI_ORGANIZATION", "OPENAI_PROJECT",
	} {
		if v := os.Getenv(k); v != "" {
			env[k] = v
		}
	}
	return env
}

func dirNames(es []os.DirEntry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// TestAppServerLive drives a full turn under an asking approval policy: the model's file write should
// raise an approval ServerRequest, which the driver surfaces as an interaction; we answer "allow" as a
// real client would, and the turn then completes. Asserts the session + result events, that at least
// one approval interaction surfaced and was answered (the respond round-trip), and logs the workspace
// so a human can confirm the approved patch actually landed a file.
func TestAppServerLive(t *testing.T) {
	if os.Getenv("CODEX_LIVE") != "1" {
		t.Skip("set CODEX_LIVE=1 (+ a working Codex credential) to run the live app-server smoke test")
	}

	ws := t.TempDir()
	inbound := make(chan agent.Inbound, 8)
	col := &collector{}

	var approved int32
	emit := func(ev agent.Event) {
		col.emit(ev)
		// Answer every approval/input request the instant it appears, exactly as a client would.
		if ev.Type == agent.EventInteraction && ev.Interaction != nil && ev.Interaction.NeedsResponse {
			inbound <- agent.Inbound{
				Kind:          "response",
				InteractionID: ev.Interaction.ID,
				Decision:      "allow",
			}
			atomic.AddInt32(&approved, 1)
		}
	}

	a := appServer{
		command:  "codex app-server",
		env:      liveSecrets(),
		message:  "Create a file named hello.txt in the current working directory with the exact contents: hi. Then stop.",
		sandbox:  "workspace-write",
		approval: "on-request",
		cwd:      ws,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := a.Run(ctx, agent.TurnInput{Inbound: inbound}, emit)
	if err != nil {
		t.Fatalf("live app-server Run errored: %v (result=%q)", err, res.Text)
	}

	evs := col.snapshot()
	if firstOfType(evs, agent.EventSession) == nil {
		t.Error("no session event — thread/start did not yield a session id")
	}
	if firstOfType(evs, agent.EventResult) == nil {
		t.Error("no result event — the turn did not reach a terminal turn/completed")
	}
	if atomic.LoadInt32(&approved) == 0 {
		t.Error("no approval interaction surfaced — an on-request file write should have prompted; the respond round-trip was not exercised")
	}

	entries, _ := os.ReadDir(ws)
	t.Logf("approvals answered=%d; workspace after turn=%v; result=%q",
		atomic.LoadInt32(&approved), dirNames(entries), res.Text)
}

// TestAppServerLiveInterrupt starts a long turn and interrupts it the instant the model emits output,
// asserting the turn still reaches a terminal result (turn/interrupt honored, no hang). The context
// timeout is the backstop that would catch a driver that fails to close out on interrupt.
func TestAppServerLiveInterrupt(t *testing.T) {
	if os.Getenv("CODEX_LIVE") != "1" {
		t.Skip("set CODEX_LIVE=1 (+ a working Codex credential) to run the live app-server interrupt test")
	}

	ws := t.TempDir()
	inbound := make(chan agent.Inbound, 4)
	col := &collector{}

	var fired int32
	emit := func(ev agent.Event) {
		col.emit(ev)
		// The moment the model produces any output, interrupt the turn.
		if (ev.Type == agent.EventText || ev.Type == agent.EventThinking) &&
			atomic.CompareAndSwapInt32(&fired, 0, 1) {
			inbound <- agent.Inbound{Kind: "interrupt"}
		}
	}

	a := appServer{
		command:  "codex app-server",
		env:      liveSecrets(),
		message:  "Count slowly from 1 to 100, writing a full sentence about each number as you go.",
		sandbox:  "read-only",
		approval: "on-request",
		cwd:      ws,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := a.Run(ctx, agent.TurnInput{Inbound: inbound}, emit)
	if err != nil {
		t.Fatalf("live interrupt Run errored: %v (result=%q)", err, res.Text)
	}
	if firstOfType(col.snapshot(), agent.EventResult) == nil {
		t.Error("no terminal result after interrupt — turn/interrupt may not have halted the turn")
	}
	t.Logf("interrupt fired=%d; result=%q", atomic.LoadInt32(&fired), res.Text)
}
