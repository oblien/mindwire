// Package mindwire is the native, in-process Go SDK for the mindwire engine — one API across every
// coding-agent harness (Claude Code, Codex, …), the same surface the TypeScript SDK exposes, but
// running the daemon's own orchestrator directly in your process. There is no HTTP server, no
// subprocess, and no network hop: New constructs an orchestrator.Supervisor exactly as the daemon
// binary does and Client calls it directly, so a turn you start here is the same turn the daemon
// would run, with the same capability gates, session store, event stream, and notifications.
//
// # Getting started
//
//	import mindwire "github.com/oblien/mindwire/daemon/sdk"
//
//	client, err := mindwire.New(mindwire.Options{Agent: "claude-code", CWD: "/path/to/project"})
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer client.Close()
//
//	run, err := client.Turn(ctx, mindwire.TurnRequest{ChatID: "chat-1", Message: "List the files here."})
//	if err != nil {
//		log.Fatal(err)
//	}
//	for ev := range run.Stream(ctx) {
//		if ev.Type == mindwire.EventText {
//			fmt.Print(ev.Text)
//		}
//	}
//	res, err := run.Wait(ctx)
//
// # Design
//
//   - Everything a consumer touches is spelled mindwire.* (see types.go). These are aliases of the
//     daemon core's public types, so returned values marshal to exactly the daemon's wire shape and no
//     conversion is needed — the SDK never asks you to import a daemon/internal/* package.
//   - Streaming is an iter.Seq[Event]: range over Run.Stream and the subscription is released for you
//     when the loop ends. Cancelling the context stops your read, not the turn (turns run on the
//     supervisor's detached context) — call Run.Cancel to actually stop one.
//   - Errors mirror the HTTP surface: an *APIError carries the status the daemon would have returned
//     (400/404/409/500), so you branch with errors.As just as you would on an HTTP client.
//   - Agent scoping: Client has a default agent (Options.Agent); override per call with ForAgent, or
//     take a cheap WithAgent view that shares the same engine.
//
// # One client per state file
//
// A Client owns one StatePath: the one-turn-per-chat gate and the JSON store are per-supervisor, so
// running two Clients over the same state file is unsupported. Construct a single Client and share it
// (it is safe for concurrent use); use WithAgent for per-agent views of that one engine.
package mindwire
