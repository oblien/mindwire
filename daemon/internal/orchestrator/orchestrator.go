// Package orchestrator is the daemon's per-sandbox SUPERVISOR. It hosts every
// registered agent adapter as an Agent runtime (adapter + auth + per-turn runner +
// namespaced cred/config view), routes a request to the selected one, and supervises
// turn execution: one turn per chat, cancellation, timeout, and panic isolation so one
// misbehaving adapter can't take down the daemon (or the other agents' in-flight turns).
// It streams unified events to the hub and emits notifications. The HTTP layer (internal/api)
// is thin glue over this.
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/procmon"
	"github.com/oblien/mindwire/daemon/internal/runner"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// maxTurn bounds a single turn so a stuck CLI can't run forever.
const maxTurn = 30 * time.Minute

// Resolve-mode bounds and the completion contract. resolveSentinel is the exact line an agent emits
// to declare the whole task globally complete; the loop strips it from the surfaced text. The caps
// (iterations + overall deadline) are the backstop when an agent never emits it — the loop then ends
// with StopReason "capped" rather than running forever.
const (
	resolveMaxIterations = 20
	resolveDeadline      = 2 * time.Hour
	resolveSentinel      = "<<RESOLVE_DONE>>"
	// resolveContract is appended to the FIRST iteration's message: it puts the agent in an unattended
	// posture and defines the completion signal. Appended to the message (not a system prompt) so it is
	// fully agent-agnostic — Codex has no append-system-prompt mechanism, and replacing the system
	// prompt would strip an agent's own tool-use instructions.
	resolveContract = "\n\n---\n[MindWire resolve mode: you are running UNATTENDED until this task is fully complete. " +
		"Do not stop to ask for confirmation or permission — proceed with your best judgment on any decision. " +
		"When the entire task is genuinely and completely done, end your final reply with a line containing exactly:\n" +
		resolveSentinel + "]"
	// resolveContinue drives every later iteration: it either advances the work or, if the agent is
	// already finished, elicits the sentinel so the loop can stop.
	resolveContinue = "Continue working toward fully completing the original task. " +
		"If it is already completely done, reply with exactly " + resolveSentinel + " and nothing else."
)

// ResolveOptions bounds a resolve run. Zero values fall back to the package defaults
// (resolveMaxIterations / resolveDeadline), so a caller can request resolve with an empty struct.
type ResolveOptions struct {
	MaxIterations int           // cap on auto-continued iterations before StopReason "capped"
	Deadline      time.Duration // overall wall-clock budget for the whole resolve
}

// Agent is one hosted adapter and everything the supervisor needs to run it: its auth
// module, per-turn runner, and namespaced cred/config view. One per registered adapter.
type Agent struct {
	Adapter agent.Adapter
	Auth    agent.AuthModule
	Runner  *runner.Runner
	Creds   *CredView
}

// ID is the agent type (e.g. "claude-code").
func (a *Agent) ID() string { return a.Adapter.ID() }

// CredView is a per-agent-type namespaced view of the shared store: every key is prefixed
// with "<agentType>:", so each agent's creds AND settings (apiKey, oauthToken, authMethod,
// model, …) are isolated. Implements agent.CredStore.
//
// The ONE exception is the provider-connection subtree ("provider:<id>:apiKey" / ":envVar"):
// a connected LLM provider is SHARED across agents, so connect Google once and every agent that
// supports it reads the same key. Those keys are routed to a fixed cross-agent namespace
// (sharedCredNamespace) instead of "<agentType>:", so any agent's view — and thus any agent's
// EnvForRun via ProviderEnvForRun — sees the same connection. Everything else stays per-agent.
type CredView struct {
	store  *session.Store
	prefix string
}

func newCredView(store *session.Store, agentType string) *CredView {
	return &CredView{store: store, prefix: agentType + ":"}
}

// sharedCredNamespace is the store prefix for cross-agent credentials (provider connections). '#'
// can't begin an agent-type id (agent.ValidateProviderID / real adapter IDs are letter/digit-led),
// so this never collides with a real agent's "<agentType>:" namespace.
const sharedCredNamespace = "#shared:"

// sharedCred reports whether a BARE key (prefix already stripped) belongs to the shared cross-agent
// namespace rather than this agent's own settings. Only the provider-registry subtree is shared; the
// single-slot auth keys ("apiKey"/"provider"/"envVar") never carry the "provider:<id>:" prefix, so
// they stay per-agent as before.
func sharedCred(bareKey string) bool { return strings.HasPrefix(bareKey, "provider:") }

// ns maps a bare key to its fully-qualified store key: shared subtree → sharedCredNamespace, else the
// per-agent-type prefix.
func (v *CredView) ns(key string) string {
	if sharedCred(key) {
		return sharedCredNamespace + key
	}
	return v.prefix + key
}

func (v *CredView) Get(key string) string     { return v.store.Get(v.ns(key)) }
func (v *CredView) Set(key, val string) error { return v.store.Set(v.ns(key), val) }

// All returns this agent's keys with the prefix stripped (bare keys like "model", "apiKey"), unioned
// with the shared cross-agent subtree (provider connections). Stale per-agent "provider:*" keys from
// before providers were shared are ignored — the shared namespace is authoritative — so an old
// connection simply needs re-connecting once.
func (v *CredView) All() map[string]string {
	out := map[string]string{}
	for k, val := range v.store.Config() {
		if rest, ok := strings.CutPrefix(k, sharedCredNamespace); ok {
			out[rest] = val
			continue
		}
		if rest, ok := strings.CutPrefix(k, v.prefix); ok {
			if sharedCred(rest) {
				continue // provider:* lives only in the shared namespace now
			}
			out[rest] = val
		}
	}
	return out
}

// Supervisor hosts all agents and supervises their turns for one sandbox.
type Supervisor struct {
	store    *session.Store
	hub      *stream.Hub
	notifier notify.Notifier
	notes    *notify.Stream   // local SSE broadcaster (in-app live notifications)
	mon      *procmon.Monitor // on-demand per-turn CPU/mem sampler (idle until a client subscribes)
	cwd      string

	agents map[string]*Agent // by agent type
	def    string            // default agent type when a request omits ?agent=

	mu      sync.Mutex
	active  map[string]bool               // chatId -> a turn is currently running
	cancels map[string]context.CancelFunc // runId -> cancel the running turn
	// User-in-loop ingress, parallel to cancels (same register-before-return + defer close). inputs
	// holds each running turn's inbound channel (present only for agents declaring an ingress
	// capability); pending records the run's respondable interactions so Respond can echo their
	// adapter correlators (Interaction.Meta) back on the Inbound without the adapter keeping state.
	inputs  map[string]chan agent.Inbound
	pending map[string]map[string]agent.Interaction // runId -> interactionId -> interaction
	// Drain accounting: inflight counts live execRun/execResolve goroutines (each decrements in a
	// defer that runs AFTER its terminal SaveRun), and idle broadcasts when it reaches zero. Wait()
	// parks on idle so a caller can Cancel every run and then tear down the state directory without
	// racing a late persist. Guarded by mu, like the maps above.
	inflight int
	idle     *sync.Cond
}

// New builds a supervisor over EVERY registered adapter (each with its own auth + runner +
// namespaced creds). defaultAgent selects the agent for requests that omit ?agent=; if it
// isn't registered, the first adapter by ID is used (deterministic).
func New(store *session.Store, hub *stream.Hub, notifier notify.Notifier, cwd, defaultAgent string) *Supervisor {
	s := &Supervisor{
		store: store, hub: hub, notifier: notifier, notes: notify.NewStream(), mon: procmon.NewMonitor(), cwd: cwd,
		agents: map[string]*Agent{}, def: defaultAgent,
		active: map[string]bool{}, cancels: map[string]context.CancelFunc{},
		inputs: map[string]chan agent.Inbound{}, pending: map[string]map[string]agent.Interaction{},
	}
	s.idle = sync.NewCond(&s.mu)
	for _, ad := range agent.All() {
		creds := newCredView(store, ad.ID())
		au := ad.Auth(creds)
		s.agents[ad.ID()] = &Agent{
			Adapter: ad, Auth: au, Creds: creds,
			Runner: runner.New(store, ad, au, creds, hub, cwd),
		}
	}
	if s.agents[s.def] == nil {
		if all := agent.All(); len(all) > 0 { // sorted by ID → deterministic
			s.def = all[0].ID()
		}
	}
	return s
}

// Resolve returns the agent runtime for a type (empty = the default). ok=false = unknown.
func (s *Supervisor) Resolve(agentType string) (*Agent, bool) {
	if agentType == "" {
		agentType = s.def
	}
	a, ok := s.agents[agentType]
	return a, ok
}

// Default is the agent type used when a request omits a selector.
func (s *Supervisor) Default() string { return s.def }

// Notes is the local notification SSE broadcaster (the API's /notify/stream subscribes here).
func (s *Supervisor) Notes() *notify.Stream { return s.notes }

// ProcMon is the on-demand per-turn CPU/mem sampler (the API's /processes/stream subscribes here).
// It is idle until a client subscribes and stops when the last one leaves — no background work, no leak.
func (s *Supervisor) ProcMon() *procmon.Monitor { return s.mon }

// CWD is the daemon's default working directory (a turn may override it per request).
func (s *Supervisor) CWD() string { return s.cwd }

// StartTurnInput is one turn's request from the API layer. Bundled into a value object so the
// StartTurn signature doesn't grow per-field as per-turn options expand.
type StartTurnInput struct {
	ChatID  string
	Message string
	CWD     string // run this turn in a specific dir (a project workdir); else the daemon default
	Options agent.TurnOptions
}

// StartTurn records the user message, creates a running Run, registers cancellation, and
// launches execution on a DETACHED context (survives app disconnect). It guards one turn
// per chat: ok=false means a turn is already running for that chat.
func (s *Supervisor) StartTurn(a *Agent, req StartTurnInput) (session.Run, bool) {
	return s.start(a, req, false)
}

// StartCompact runs an on-demand compaction as a first-class run through the IDENTICAL execRun path as
// a turn, so the compaction boundary streams and records exactly like an auto-compaction. It differs
// from StartTurn in two ways: it records no user message (the /compact trigger isn't a user turn) and
// allocates no ingress channel (a compaction never pauses for the user). It still takes the per-chat
// turn lock — a compaction is mutually exclusive with a turn. ok=false ⇒ a turn is already running for
// that chat. The API gates this on the adapter implementing agent.CompactModule.
func (s *Supervisor) StartCompact(a *Agent, req StartTurnInput) (session.Run, bool) {
	return s.start(a, req, true)
}

// StartResolve launches a GLOBAL-RESOLVE run: instead of returning after one turn, the daemon holds
// the run open and auto-continues the agent's own multi-step work until the task is globally complete
// (the agent emits the completion sentinel), an unrecoverable error occurs, or a cap is hit. It
// creates a PARENT run (Kind "resolve") the caller waits on and streams under; each auto-continued
// iteration is a child run whose events publish onto the parent's topic (one merged stream). Like
// start() it takes the per-chat turn lock for the whole resolve (ok=false ⇒ a turn is already running
// for that chat) and records the user message once. It allocates NO ingress channel: resolve runs
// unattended, and with no Inbound channel neither adapter can enter its pausing transport (both gate
// persistent/app-server on Inbound != nil), so the loop can't stall on an approval.
func (s *Supervisor) StartResolve(a *Agent, req StartTurnInput, ro ResolveOptions) (session.Run, bool) {
	if ro.MaxIterations <= 0 {
		ro.MaxIterations = resolveMaxIterations
	}
	if ro.Deadline <= 0 {
		ro.Deadline = resolveDeadline
	}
	s.mu.Lock()
	if s.active[req.ChatID] {
		s.mu.Unlock()
		return session.Run{}, false
	}
	s.active[req.ChatID] = true
	// The parent context bounds the WHOLE resolve (overall deadline); each child turn derives a 30-min
	// timeout from it. Register the cancel under the parent id BEFORE returning, so an immediate cancel
	// can't race an unregistered run into a 404 (same anti-race as start()).
	ctx, cancel := context.WithTimeout(context.Background(), ro.Deadline)
	parent := session.Run{ID: newID(), ChatID: req.ChatID, Agent: a.ID(), Status: "running", Kind: "resolve", CreatedAt: nowISO()}
	s.cancels[parent.ID] = cancel
	s.inflight++ // paired with execResolve's deferred runDone; unconditional launch below
	s.mu.Unlock()

	// Resolve reuses one process group per child turn; the reporter re-Tracks under the parent id on
	// each spawn (last spawn wins, which is the currently-live turn). execResolve's teardown Untracks.
	ctx = proc.WithReporter(ctx, func(pid int) { s.mon.Track(parent.ID, a.ID(), parent.ChatID, pid) })

	_ = s.store.AddMessage(session.Message{
		ID: newID(), ChatID: req.ChatID, Role: "user", Text: req.Message, CreatedAt: nowISO(),
	})
	_ = s.store.SaveRun(parent)

	go s.execResolve(ctx, cancel, a, parent, req, ro)
	return parent, true
}

// start is the shared launch path for StartTurn (compact=false) and StartCompact (compact=true).
func (s *Supervisor) start(a *Agent, req StartTurnInput, compact bool) (session.Run, bool) {
	s.mu.Lock()
	if s.active[req.ChatID] {
		s.mu.Unlock()
		return session.Run{}, false
	}
	s.active[req.ChatID] = true
	// Create + register the cancel func BEFORE returning the run id, so a client that
	// cancels immediately after POST /turns can't race an unregistered run into a 404.
	ctx, cancel := context.WithTimeout(context.Background(), maxTurn)
	run := session.Run{ID: newID(), ChatID: req.ChatID, Agent: a.ID(), Status: "running", CreatedAt: nowISO()}
	s.cancels[run.ID] = cancel
	// Same anti-race for user-in-loop: if the agent can take ingress (respond/input/interrupt),
	// register its inbound channel + pending map now, so a client that answers an interaction
	// immediately can't race an unallocated channel into a 404. A compaction takes no ingress, so
	// it's allocated only for ordinary turns.
	var inbound chan agent.Inbound
	if !compact {
		caps := a.Adapter.Capabilities()
		if caps.Respond || caps.Input || caps.Interrupt || caps.SetModel || caps.SetPermissionMode {
			inbound = make(chan agent.Inbound, 16)
			s.inputs[run.ID] = inbound
			s.pending[run.ID] = map[string]agent.Interaction{}
		}
	}
	s.inflight++ // paired with execRun's deferred runDone; unconditional launch below
	s.mu.Unlock()

	// Make this turn's process group visible to on-demand resource monitoring. The reporter rides on
	// ctx (survives the driver's per-turn spawn) and Tracks the group leader pid the instant a spawn
	// site reports it; execRun's teardown Untracks. No-op unless a client is watching /processes/stream.
	ctx = proc.WithReporter(ctx, func(pid int) { s.mon.Track(run.ID, a.ID(), run.ChatID, pid) })

	// Thin recorded log (universal fallback; native-history agents are read from their store). A
	// compaction records no user message — the /compact trigger isn't a user turn; only its
	// compaction boundary (accumulated as a Part on the assistant reply) belongs in the transcript.
	if !compact {
		_ = s.store.AddMessage(session.Message{
			ID: newID(), ChatID: req.ChatID, Role: "user", Text: req.Message, CreatedAt: nowISO(),
		})
	}
	_ = s.store.SaveRun(run)

	go s.execRun(ctx, cancel, a, run, req, inbound, compact)
	return run, true
}

// Busy reports whether a turn is currently running for a chat. The API uses it to 409 a delete or
// fork while the chat is live (a rename is safe to proceed). Reads s.active under the same lock
// StartTurn/execRun mutate it.
func (s *Supervisor) Busy(chatID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[chatID]
}

// runDone marks one supervised run goroutine finished and wakes any Wait once the last one exits.
// Deferred first in execRun/execResolve so it runs after their terminal SaveRun and lock teardown —
// the guarantee Wait relies on: when inflight hits zero, every persist has landed.
func (s *Supervisor) runDone() {
	s.mu.Lock()
	s.inflight--
	if s.inflight == 0 {
		s.idle.Broadcast()
	}
	s.mu.Unlock()
}

// Wait blocks until every in-flight turn goroutine has fully finished — including its terminal
// SaveRun — so a caller can Cancel all runs and then safely tear down the state directory without
// racing a late persist. It does NOT itself cancel; a clean drain is Cancel-all followed by Wait.
// Safe to call concurrently and repeatedly (a later turn that starts mid-drain is simply waited on
// too, never a WaitGroup-style panic).
func (s *Supervisor) Wait() {
	s.mu.Lock()
	for s.inflight > 0 {
		s.idle.Wait()
	}
	s.mu.Unlock()
}

// Cancel stops an in-flight turn (cancels its context, killing the CLI). false = no turn
// is currently running for that run id.
func (s *Supervisor) Cancel(runID string) bool {
	s.mu.Lock()
	cancel := s.cancels[runID]
	s.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// Respond delivers the user's answer to a mid-turn interaction (a permission approval, or an
// AskUserQuestion / ExitPlanMode reply) into the running turn. It echoes the interaction's adapter
// correlators — recorded in pending from Interaction.Meta (respondVia, toolUseId) — back on the
// Inbound so the adapter frames the native wire response statelessly. false = no turn with an open
// ingress channel for that run id (unknown, finished, or the agent takes no ingress).
func (s *Supervisor) Respond(runID, interactionID, decision string, options []string, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var meta map[string]string
	if p := s.pending[runID]; p != nil {
		if it, ok := p[interactionID]; ok {
			meta = metaStrings(it.Meta)
		}
	}
	return s.trySend(runID, agent.Inbound{
		Kind: "response", InteractionID: interactionID, Decision: decision, Options: options, Text: text, Meta: meta,
	})
}

// SendInput queues a follow-up user message into a running turn (steer/append without cancelling).
// false = no open ingress channel for that run id.
func (s *Supervisor) SendInput(runID, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trySend(runID, agent.Inbound{Kind: "input", Text: text})
}

// Interrupt soft-stops a running turn (asks the agent to halt current work) WITHOUT the hard context
// kill Cancel does — the turn stays active for a follow-up over /input. false = no open ingress
// channel for that run id.
func (s *Supervisor) Interrupt(runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trySend(runID, agent.Inbound{Kind: "interrupt"})
}

// SetModel switches the model of a live turn (empty model resets to the agent/CLI default). Only a
// persistent transport reads this; on a one-shot turn the send succeeds into the buffer but the CLI
// never consumes it (a documented best-effort no-op). false = no open ingress channel for that run id.
func (s *Supervisor) SetModel(runID, model string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trySend(runID, agent.Inbound{Kind: "set_model", Text: model})
}

// SetPermissionMode switches the permission mode of a live turn (mode is required). Same
// persistent-only best-effort semantics as SetModel. false = no open ingress channel for that run id.
func (s *Supervisor) SetPermissionMode(runID, mode string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trySend(runID, agent.Inbound{Kind: "set_permission_mode", Text: mode})
}

// recordPending stores a respondable interaction so Respond can recover its correlators. No-op when
// the run has no ingress channel (its pending map was never allocated).
func (s *Supervisor) recordPending(runID string, it agent.Interaction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.pending[runID]; p != nil {
		p[it.ID] = it
	}
}

// trySend does a NON-BLOCKING send of an Inbound to a run's ingress channel. It MUST be called with
// s.mu held so the channel can't be closed by execRun's teardown mid-send (teardown deletes under the
// same lock, so a deleted channel reads as nil here — never a send on a closed channel). false = the
// run has no channel (unknown/finished) or its buffer is momentarily full.
func (s *Supervisor) trySend(runID string, in agent.Inbound) bool {
	ch := s.inputs[runID]
	if ch == nil {
		return false
	}
	select {
	case ch <- in:
		return true
	default:
		return false
	}
}

// metaStrings flattens an Interaction.Meta (map[string]any) into the map[string]string an Inbound
// carries, stringifying non-string scalars so adapter correlators survive the round-trip.
func metaStrings(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if str, ok := v.(string); ok {
			out[k] = str
		} else {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

// execRun streams the turn (to the hub) on the daemon's own context so it survives app
// disconnects, records the result, and notifies. Runs in its own goroutine with panic
// isolation.
func (s *Supervisor) execRun(ctx context.Context, cancel context.CancelFunc, a *Agent, run session.Run, req StartTurnInput, inbound chan agent.Inbound, compact bool) {
	defer s.runDone() // registered first ⇒ runs last, after the terminal SaveRun and teardown
	defer cancel()
	defer func() {
		s.mu.Lock()
		delete(s.active, run.ChatID)
		delete(s.cancels, run.ID)
		// Tear down ingress under the same lock that Respond/SendInput/Interrupt send under: once the
		// channel is out of s.inputs, a concurrent send sees nil and returns false, so we never send on
		// (or close) a channel a caller still holds — no send-on-closed race. Closing wakes the driver's
		// writer if it is still selecting (it has usually already exited via the turn's terminal result).
		if ch := s.inputs[run.ID]; ch != nil {
			delete(s.inputs, run.ID)
			close(ch)
		}
		delete(s.pending, run.ID)
		s.mu.Unlock()
		s.mon.Untrack(run.ID) // PID lifecycle == turn lifecycle: stop reporting this turn's resources
	}()
	// Panic isolation: an adapter panic must not crash the daemon and every other agent's
	// in-flight turn. Record the run as errored and close its stream.
	defer func() {
		if rec := recover(); rec != nil {
			run.Status = "error"
			run.Error = fmt.Sprintf("agent crashed: %v", rec)
			run.EndedAt = nowISO()
			_ = s.store.SaveRun(run)
			s.hub.Publish(run.ID, agent.Event{Type: agent.EventError, Error: run.Error})
			s.hub.Close(run.ID)
			s.emit(a, run, agent.Errored, snippet(run.Error))
		}
	}()

	// Watch the live event stream for an interaction that needs the user (a question or a plan to
	// approve) and fire a "waiting for you" notification. Runs concurrently; exits when the run's
	// hub topic closes at turn end. Separate from the terminal finished/error emits below.
	go s.watchInteractions(a, run)

	turn := runner.Turn{
		ChatID: run.ChatID, Message: req.Message, RunID: run.ID, CWD: req.CWD, Options: req.Options,
		Inbound: inbound,
	}
	var res agent.TurnResult
	var parts []agent.Part
	if compact {
		// RunCompact drives the adapter's CompactModule through the same accumulation path. ok=false
		// only if the adapter doesn't implement it — impossible here (the API route type-asserts the
		// same interface before calling StartCompact), but stay honest if reached: surface an error.
		var ok bool
		res, parts, ok = a.Runner.RunCompact(ctx, turn)
		if !ok {
			res = agent.TurnResult{Text: "agent does not support compaction", IsError: true}
		}
	} else {
		res, parts = a.Runner.RunTurn(ctx, turn)
	}
	run.EndedAt = nowISO()

	// A user-initiated cancel surfaces as context.Canceled (vs DeadlineExceeded for the
	// turn timeout). Record it as cancelled, no error notification.
	if ctx.Err() == context.Canceled {
		run.Status = "cancelled"
		_ = s.store.SaveRun(run)
		s.hub.Close(run.ID)
		return
	}
	if res.IsError {
		run.Status = "error"
		run.Error = res.Text
		_ = s.store.SaveRun(run)
		s.emit(a, run, agent.Errored, snippet(res.Text)) // notify + publish result BEFORE closing
		s.hub.Close(run.ID)
		return
	}

	reply := session.Message{ID: newID(), ChatID: run.ChatID, Role: "assistant", Text: res.Text, Parts: parts, CreatedAt: nowISO()}
	_ = s.store.AddMessage(reply)
	run.Status = "done"
	run.ReplyID = reply.ID
	_ = s.store.SaveRun(run)
	s.emit(a, run, agent.Finished, snippet(res.Text)) // notify + publish result BEFORE closing
	s.hub.Close(run.ID)
}

// execResolve is the resolve-mode supervisor loop. It owns the parent's hub topic — every child turn
// streams onto it via RunTurn (RunID = parent.ID; the runner never closes the hub, so the topic stays
// continuous across iterations) and only this function closes it, at the true end. Each iteration:
// publishes a continuation boundary, runs one autonomous child turn (no ingress channel), persists the
// child reply + child Run, then decides whether to stop (sentinel/error) or auto-continue (a
// continuable stop, or a completion probe). Runs in its own goroutine with the same panic isolation
// and lock/cancel teardown as execRun.
func (s *Supervisor) execResolve(ctx context.Context, cancel context.CancelFunc, a *Agent, parent session.Run, req StartTurnInput, ro ResolveOptions) {
	defer s.runDone() // registered first ⇒ runs last, after the terminal SaveRun and teardown
	defer cancel()
	defer func() {
		s.mu.Lock()
		delete(s.active, parent.ChatID)
		delete(s.cancels, parent.ID)
		s.mu.Unlock()
		s.mon.Untrack(parent.ID) // PID lifecycle == resolve lifecycle
	}()
	// Panic isolation: an adapter panic mid-resolve records the parent as errored and closes the topic,
	// rather than crashing the daemon and every other agent's in-flight turn.
	defer func() {
		if rec := recover(); rec != nil {
			parent.Status = "error"
			parent.Error = fmt.Sprintf("agent crashed: %v", rec)
			parent.StopReason = "error"
			parent.EndedAt = nowISO()
			_ = s.store.SaveRun(parent)
			s.hub.Publish(parent.ID, agent.Event{Type: agent.EventError, Error: parent.Error})
			s.hub.Close(parent.ID)
			s.emit(a, parent, agent.Errored, snippet(parent.Error))
		}
	}()

	var lastText, lastReplyID, stopReason string
	iterations := 0
	reason := "start" // why THIS iteration is running (carried on its continuation boundary)

	for i := 0; i < ro.MaxIterations; i++ {
		if ctx.Err() != nil { // parent cancelled or overall deadline hit before this iteration
			break
		}

		child := session.Run{ID: newID(), ChatID: parent.ChatID, Agent: a.ID(), Status: "running", ParentID: parent.ID, CreatedAt: nowISO()}
		_ = s.store.SaveRun(child)
		s.hub.Publish(parent.ID, agent.Event{Type: agent.EventContinuation, Continuation: &agent.ContinuationInfo{
			Iteration: i, Reason: reason, ChildRunID: child.ID,
		}})

		// First iteration carries the task + the unattended/completion contract; later iterations carry
		// the continue nudge and drop first-turn-only heavy options (attachments/schema/explicit session
		// controls) so each continue simply resumes the just-updated session.
		msg := req.Message + resolveContract
		opts := req.Options
		if i > 0 {
			msg = resolveContinue
			opts.Attachments = nil
			opts.OutputSchema = nil
			opts.SystemPrompt = ""
			opts.SessionID = ""
			opts.ContinueLatest = false
			opts.ForkOnResume = false
		}

		childCtx, childCancel := context.WithTimeout(ctx, maxTurn)
		res, parts := a.Runner.RunTurn(childCtx, runner.Turn{
			ChatID: parent.ChatID, Message: msg, RunID: parent.ID, CWD: req.CWD, Options: opts,
		})
		childCancel()
		iterations = i + 1
		child.EndedAt = nowISO()

		// A parent cancel surfaces as context.Canceled on the child; stop the whole resolve.
		if ctx.Err() == context.Canceled {
			child.Status = "cancelled"
			_ = s.store.SaveRun(child)
			stopReason = "cancelled"
			break
		}

		text := res.Text
		done := strings.Contains(text, resolveSentinel)
		if done {
			text = strings.TrimSpace(strings.ReplaceAll(text, resolveSentinel, ""))
		}

		if res.IsError {
			child.Status = "error"
			child.Error = res.Text
			_ = s.store.SaveRun(child)
			if res.Text != "" {
				lastText = res.Text
			}
			stopReason = "error"
			break
		}

		// Persist a child reply only when it carries visible text — a pure-sentinel confirm adds no
		// empty transcript message and keeps the prior substantive answer as the aggregate.
		if text != "" {
			reply := session.Message{ID: newID(), ChatID: parent.ChatID, Role: "assistant", Text: text, Parts: parts, CreatedAt: nowISO()}
			_ = s.store.AddMessage(reply)
			child.ReplyID = reply.ID
			lastText = text
			lastReplyID = reply.ID
		}
		child.Status = "done"
		_ = s.store.SaveRun(child)

		if done {
			stopReason = "done"
			break
		}
		// Not done: decide why the NEXT iteration runs. A continuable subtype means the agent hit its
		// own budget (resume it); a clean settle with no sentinel means we probe for completion.
		switch {
		case res.Subtype == "error_max_turns":
			reason = "max_turns"
		case res.Subtype == "error_max_budget_usd":
			reason = "max_budget"
		default:
			reason = "probe"
		}
	}

	if stopReason == "" { // loop exhausted without done/error
		if ctx.Err() == context.Canceled {
			stopReason = "cancelled"
		} else {
			stopReason = "capped"
		}
	}

	parent.EndedAt = nowISO()
	parent.Iterations = iterations
	parent.StopReason = stopReason
	parent.ReplyID = lastReplyID

	switch stopReason {
	case "cancelled":
		parent.Status = "cancelled"
		_ = s.store.SaveRun(parent)
		s.hub.Close(parent.ID)
		return
	case "error":
		parent.Status = "error"
		parent.Error = lastText
		_ = s.store.SaveRun(parent)
		s.hub.Publish(parent.ID, agent.Event{Type: agent.EventContinuation, Continuation: &agent.ContinuationInfo{
			Iteration: iterations, StopReason: stopReason,
		}})
		s.emit(a, parent, agent.Errored, snippet(lastText)) // notify + trailing status BEFORE close
		s.hub.Close(parent.ID)
		return
	}

	// Done or capped: a final continuation boundary carries the stop reason, then the AGGREGATE result
	// is published LAST (so wait() returns it, not a sub-turn's result), then the finished notification.
	parent.Status = "done"
	_ = s.store.SaveRun(parent)
	s.hub.Publish(parent.ID, agent.Event{Type: agent.EventContinuation, Continuation: &agent.ContinuationInfo{
		Iteration: iterations, StopReason: stopReason,
	}})
	s.hub.Publish(parent.ID, agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: lastText}})
	s.emit(a, parent, agent.Finished, snippet(lastText))
	s.hub.Close(parent.ID)
}

// emit builds a notification for a condition (using the agent's declared UX) and fans it to
// the external webhook (or no-op if unconfigured) AND the local SSE stream.
func (s *Supervisor) emit(a *Agent, run session.Run, cond agent.Condition, body string) {
	n := s.buildNote(a, cond, body, run.ChatID, run.ID)
	// Bounded so a slow webhook can't stall the turn's stream-close (emit runs just before Close).
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	err := s.notifier.Notify(ctx, n)
	_ = s.notes.Notify(context.Background(), n)

	summary := "sent ✓"
	if err != nil {
		summary = err.Error()
	}
	log.Printf("notify: cond=%s chat=%s → %s", cond, run.ChatID, summary)
	// Surface the outcome on the run stream so the CLIENT can see whether the daemon's webhook POST
	// succeeded (the POST is otherwise invisible to the client). Published while the hub topic is
	// still open (emit is called just before Close); a no-op if already closed.
	s.hub.Publish(run.ID, agent.Event{Type: agent.EventStatus, Meta: map[string]any{"notify": summary}})
}

// watchInteractions subscribes to a run's event stream and fires a single "waiting for you"
// notification the first time the turn surfaces an interaction that NeedsResponse (a question or a
// plan to approve — not TodoWrite progress). Runs in its own goroutine; the range over the live
// channel ends when execRun closes the hub topic at turn completion, so the goroutine cannot leak.
// One notification per run avoids spamming a turn that asks repeatedly.
func (s *Supervisor) watchInteractions(a *Agent, run session.Run) {
	replay, ch, done, cancel := s.hub.Subscribe(run.ID)
	defer cancel()
	fired := false
	consider := func(ev agent.Event) {
		if ev.Type != agent.EventInteraction || ev.Interaction == nil || !ev.Interaction.NeedsResponse {
			return
		}
		// Record every respondable interaction so Respond can echo its adapter correlators (respondVia,
		// toolUseId — kept in Interaction.Meta) back on the outbound Inbound, keeping the adapter stateless.
		s.recordPending(run.ID, *ev.Interaction)
		if fired {
			return
		}
		fired = true
		cond := agent.WaitingFeedback
		if ev.Interaction.Kind == "approval" {
			cond = agent.WaitingApproval
		}
		body := ev.Interaction.Title
		if body == "" {
			body = ev.Interaction.Detail
		}
		s.emit(a, run, cond, snippet(body))
	}
	for _, ev := range replay {
		consider(ev)
	}
	if done {
		return
	}
	for ev := range ch {
		consider(ev)
	}
}

// buildNote fills a Notification from the agent's NotificationSpec — its per-condition title
// + actions, falling back to a default title.
func (s *Supervisor) buildNote(a *Agent, cond agent.Condition, body, chatID, runID string) agent.Notification {
	title := agent.DefaultTitle(cond, a.Adapter.Meta().Name)
	var actions []agent.Action
	if ux, ok := a.Adapter.Notifications().For(cond); ok {
		if ux.Title != "" {
			title = ux.Title
		}
		actions = ux.Actions
	}
	return agent.Notification{
		Condition: cond, Title: title, Body: body,
		Agent: a.ID(), ChatID: chatID, RunID: runID, Actions: actions,
	}
}

// ---- helpers ---------------------------------------------------------------

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

// snippet is a short, rune-safe, single-line summary for a notification body.
func snippet(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Turn complete."
	}
	if r := []rune(s); len(r) > 100 {
		return strings.TrimSpace(string(r[:100])) + "…"
	}
	return s
}
