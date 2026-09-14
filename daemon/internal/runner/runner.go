// Package runner drives a single turn: it builds the TurnInput (session id for
// resume, the agent's own settings, auth env), invokes the adapter's streaming run,
// publishes the unified events to the SSE hub, and persists the agent's session id.
//
// All per-agent state flows through the namespaced CredView, never the raw store:
// Config is the agent's non-secret settings (prefix stripped), Env is its auth. Session
// ids are keyed per (agent, chat) so adapters can't collide on a shared chatId.
package runner

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// resolveCWD returns dir with symlinks resolved (matching the project slug Claude Code writes the
// transcript under). EvalSymlinks requires the path to exist; when it doesn't, the un-resolved dir is
// returned — the readers resolve again at lookup time and fall back to a cross-project glob.
func resolveCWD(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

type Runner struct {
	store   *session.Store
	adapter agent.Adapter
	auth    agent.AuthModule
	creds   agent.CredStore // the agent's namespaced view (settings + creds), prefix stripped
	hub     *stream.Hub
	cwd     string
}

func New(store *session.Store, adapter agent.Adapter, auth agent.AuthModule, creds agent.CredStore, hub *stream.Hub, cwd string) *Runner {
	return &Runner{store: store, adapter: adapter, auth: auth, creds: creds, hub: hub, cwd: cwd}
}

// Turn is one turn's request as assembled by the supervisor and handed to the runner. Bundled so
// the signature doesn't grow per-field as per-turn options expand.
type Turn struct {
	ChatID  string
	Message string
	RunID   string
	CWD     string // run this turn in a specific dir (a project workdir); empty = daemon default
	Options agent.TurnOptions
	// Inbound is the user's mid-turn ingress channel (approval answers, follow-up input, interrupts),
	// owned by the supervisor. Receive-only for the adapter; nil for a one-shot turn.
	Inbound <-chan agent.Inbound
}

// RunTurn executes one turn, streaming unified events to the hub under t.RunID and persisting the
// agent's session id. Returns the final result plus the accumulated rich transcript.
func (r *Runner) RunTurn(ctx context.Context, t Turn) (agent.TurnResult, []agent.Part) {
	return r.run(ctx, t, r.adapter.RunStream)
}

// RunCompact drives an on-demand compaction through the SAME streaming + transcript-accumulation path
// as RunTurn (via the adapter's CompactModule), so the compaction boundary lands in history and the
// SSE stream exactly as an auto-compaction does. The bool reports whether the adapter implements
// CompactModule — false ⇒ the agent can't compact (the API route is gated on the same assertion, but
// the runner stays honest if it's reached directly).
func (r *Runner) RunCompact(ctx context.Context, t Turn) (agent.TurnResult, []agent.Part, bool) {
	mod, ok := r.adapter.(agent.CompactModule)
	if !ok {
		return agent.TurnResult{}, nil, false
	}
	res, parts := r.run(ctx, t, mod.Compact)
	return res, parts, true
}

// run is the shared turn engine: it resolves the cwd/history anchor, folds per-turn overrides, builds
// TurnInput, then streams fn's unified events to the hub while accumulating the rich transcript. Both
// RunTurn (fn = adapter.RunStream) and RunCompact (fn = CompactModule.Compact) go through it, so a
// compaction is recorded and streamed identically to an ordinary turn.
func (r *Runner) run(ctx context.Context, t Turn, fn func(context.Context, agent.TurnInput, agent.Emit) (agent.TurnResult, error)) (agent.TurnResult, []agent.Part) {
	chatID, message, runID := t.ChatID, t.Message, t.RunID
	dir := t.CWD
	if dir == "" {
		dir = r.cwd
	}
	// Pin this chat's history anchor to the FIRST turn's directory (symlink-resolved, matching the
	// slug Claude Code itself writes under) so native-history lookups find the path-scoped transcript
	// later, even when a messages request carries no cwd. First-turn-wins keeps the anchor stable: an
	// explicit per-turn cwd still drives where the turn RUNS (dir, below), but a later run elsewhere
	// doesn't move where history is read from. Resolving at record time also survives the dir being
	// removed afterward; the cross-project transcript glob is the final fallback for any drift.
	if r.store.ChatCWD(chatID) == "" {
		_ = r.store.SetChatCWD(chatID, resolveCWD(dir))
	}

	// Fold per-turn canon-addressed overrides into Config here (the single choke point), then clear
	// Options.Settings so the adapter reads overrides only from Config — never the unresolved canons.
	opts := t.Options
	opts.Settings = nil
	// Fork-safe first turn: a chat freshly forked from another shares the source's native session id
	// (seeded by ForkChat). Force ForkOnResume on the first turn — consumed and cleared atomically — so
	// the turn BRANCHES a new native session instead of continuing (and polluting) the source's, no
	// matter what session flags the client passed. Only the resuming case actually forks (the adapter
	// adds --fork-session only when a base session exists), so a fork with no seeded session is a no-op.
	if r.store.TakeForkPending(r.adapter.ID(), chatID) {
		opts.ForkOnResume = true
	}
	in := agent.TurnInput{
		Message:   message,
		SessionID: r.store.Session(r.adapter.ID(), chatID),
		CWD:       dir,
		Config:    r.settings(t.Options),
		Env:       r.auth.EnvForRun(),
		Options:   opts,
		Inbound:   t.Inbound,
	}

	// Persistence and reattachment use the same reducer. A resolve child keeps its own
	// transcript while the hub accumulates every iteration on the parent's topic.
	var sessionID string
	var transcript stream.Transcript
	var emitMu sync.Mutex
	accepting := true
	emit := func(ev agent.Event) {
		emitMu.Lock()
		defer emitMu.Unlock()
		if !accepting {
			return
		}
		if ev.At == "" {
			ev.At = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if ev.SessionID != "" {
			sessionID = ev.SessionID
		}
		transcript.Apply(ev)
		r.hub.Publish(runID, ev)
	}

	res, err := fn(ctx, in, emit)
	if err != nil {
		if !res.IsError || res.Text == "" {
			res.Text = err.Error()
		}
		res.IsError = true
		emit(agent.Event{Type: agent.EventError, Error: res.Text})
	}
	emitMu.Lock()
	accepting = false
	parts := transcript.Snapshot(true).Parts
	sid := agent.FirstNonEmpty(res.SessionID, sessionID)
	emitMu.Unlock()
	if !res.IsError && res.Text != "" {
		hasText := false
		for _, part := range parts {
			hasText = hasText || part.Type == "text" && part.Text != ""
		}
		if !hasText {
			parts = append(parts, agent.Part{Type: "text", Text: res.Text})
		}
	}

	if sid != "" {
		_ = r.store.SetSession(r.adapter.ID(), chatID, sid)
	}
	return res, parts
}

// settings is the agent's applied, non-secret settings for a turn: its namespaced sticky config
// (prefix already stripped by the CredView) with per-turn Options.Settings overlaid, filtered to
// the keys the adapter declares. Secrets are excluded here — they reach the CLI only via Env
// (AuthModule.EnvForRun).
func (r *Runner) settings(opts agent.TurnOptions) map[string]string {
	return resolveSettings(r.adapter.Settings(), r.creds.All(), opts)
}

// resolveSettings applies per-turn canon-addressed overrides on top of the sticky settings, filtered
// to the schema's declared non-secret keys. This is the single security choke point for per-turn
// options: a canon that resolves to no declared key, or to a key outside the non-secret allow-list,
// is dropped — so an override can neither introduce an unknown key nor reach a secret (secrets are
// absent from the settings schema entirely). Overrides win over sticky values.
func resolveSettings(schema agent.SettingsSchema, sticky map[string]string, opts agent.TurnOptions) map[string]string {
	allow := agent.SettingsKeys(schema)
	out := make(map[string]string, len(allow))
	for k, v := range sticky {
		if allow[k] {
			out[k] = v
		}
	}
	for canon, v := range opts.Settings {
		if key, ok := agent.CanonToKey(schema, canon); ok && allow[key] {
			out[key] = v
		}
	}
	return out
}
