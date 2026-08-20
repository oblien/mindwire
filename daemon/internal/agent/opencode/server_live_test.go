package opencode

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// --- Live smoke: real `opencode serve` end to end -----------------------------------------------
//
// These drive the REAL opencode binary over its native HTTP + SSE server — the one thing the scripted
// TestConverse* tests (httptest) structurally cannot prove: that the captured wire shapes (part/tool/
// idle/permission field names, the prompt_async + permissions routes) match a live opencode. They are
// skipped unless OPENCODE_LIVE=1 because they need a working provider credential (a provider API key in
// the environment) and network. They still COMPILE in CI, so any drift in the server struct's fields or
// the agent contracts is caught by `go build`/`go vet`; only the runtime is gated.
//
// Run with, e.g.:
//   OPENCODE_LIVE=1 ANTHROPIC_API_KEY=sk-... OPENCODE_MODEL=anthropic/claude-haiku-4-5-20251001 \
//     go test -run TestServerLive ./internal/agent/opencode/ -v

// liveModel resolves the provider/model to drive from OPENCODE_MODEL (opencode's provider/model id); an
// empty return lets opencode fall back to its configured default.
func liveModel() (provider, model string) { return splitModel(os.Getenv("OPENCODE_MODEL")) }

// liveServer builds a server for a live turn, inheriting provider keys from the ambient environment
// (opencode reads them straight from the process env — Run already overlays os.Environ()).
func liveServer(msg, cwd string, interactive bool) server {
	p, m := liveModel()
	return server{message: msg, provider: p, model: m, cwd: cwd, interactive: interactive}
}

// TestServerLive drives a full turn that writes a file. Under an interactive (ask) posture opencode may
// raise a permission ask, which the driver surfaces and we answer "allow"; whether one is raised depends
// on opencode's own permission config, so the approval count is logged (and only hard-asserted when
// OPENCODE_EXPECT_APPROVAL=1). Session + terminal result are always required.
func TestServerLive(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("set OPENCODE_LIVE=1 (+ a provider API key in the env) to run the live opencode smoke test")
	}

	ws := t.TempDir()
	inbound := make(chan agent.Inbound, 8)
	col := &collector{}

	var approved int32
	emit := func(ev agent.Event) {
		col.emit(ev)
		if ev.Type == agent.EventInteraction && ev.Interaction != nil && ev.Interaction.NeedsResponse {
			inbound <- agent.Inbound{Kind: "response", InteractionID: ev.Interaction.ID, Decision: "allow"}
			atomic.AddInt32(&approved, 1)
		}
	}

	srv := liveServer("Create a file named hello.txt in the current working directory with the exact contents: hi. Then stop.", ws, true)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := srv.Run(ctx, agent.TurnInput{Inbound: inbound}, emit)
	if err != nil {
		t.Fatalf("live opencode Run errored: %v (result=%q)", err, res.Text)
	}

	evs := col.snapshot()
	if firstOfType(evs, agent.EventSession) == nil {
		t.Error("no session event — POST /session did not yield a session id")
	}
	if firstOfType(evs, agent.EventResult) == nil {
		t.Error("no result event — the turn did not reach a terminal session.idle")
	}
	if os.Getenv("OPENCODE_EXPECT_APPROVAL") == "1" && atomic.LoadInt32(&approved) == 0 {
		t.Error("expected an approval interaction (OPENCODE_EXPECT_APPROVAL=1) but none surfaced")
	}

	entries, _ := os.ReadDir(ws)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Logf("approvals answered=%d; workspace after turn=%v; result=%q", atomic.LoadInt32(&approved), names, res.Text)
}

// TestServerLiveInterrupt starts a long turn and interrupts it the instant the model emits output,
// asserting the turn still reaches a terminal result (abort honored, no hang). The context timeout is
// the backstop that would catch a driver that fails to close out on interrupt.
func TestServerLiveInterrupt(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("set OPENCODE_LIVE=1 (+ a provider API key in the env) to run the live opencode interrupt test")
	}

	ws := t.TempDir()
	inbound := make(chan agent.Inbound, 4)
	col := &collector{}

	var fired int32
	emit := func(ev agent.Event) {
		col.emit(ev)
		if (ev.Type == agent.EventText || ev.Type == agent.EventThinking) &&
			atomic.CompareAndSwapInt32(&fired, 0, 1) {
			inbound <- agent.Inbound{Kind: "interrupt"}
		}
	}

	srv := liveServer("Count slowly from 1 to 100, writing a full sentence about each number as you go.", ws, false)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := srv.Run(ctx, agent.TurnInput{Inbound: inbound}, emit)
	if err != nil {
		t.Fatalf("live interrupt Run errored: %v (result=%q)", err, res.Text)
	}
	if firstOfType(col.snapshot(), agent.EventResult) == nil {
		t.Error("no terminal result after interrupt — abort may not have halted the turn")
	}
	t.Logf("interrupt fired=%d; result=%q", atomic.LoadInt32(&fired), res.Text)
}
