// Package api is the daemon's unified HTTP surface — the one protocol the app speaks
// regardless of which agent runs. It is thin glue over the orchestrator.Supervisor:
// it decodes/authorizes/streams, and delegates agent resolution and turn supervision.
// Every agent-specific route accepts ?agent=<type> (default otherwise); turns/runs/chats
// are shared (keyed by chatId), while auth/config/creds are isolated per agent type.
package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/procmon"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/setup"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

const (
	// maxBody caps request bodies (turn messages / config are small); guards against an
	// oversized or slow-streamed body exhausting daemon memory.
	maxBody = 1 << 20 // 1 MiB
	// maxSetup bounds a toolchain install run. Setup runs on a DETACHED context so a client
	// disconnect can't abort a long `npm i -g` mid-write; only this timeout stops it.
	maxSetup = 20 * time.Minute
	// sseHeartbeat keeps idle SSE connections alive through intermediaries and lets a dead
	// client be detected on the next write.
	sseHeartbeat = 25 * time.Second
)

type API struct {
	store *session.Store
	hub   *stream.Hub
	sup   *orchestrator.Supervisor

	// Per-agent toolchain install state. Setup runs in the BACKGROUND (npm can take minutes) so a
	// client request never hangs through the proxy; the client polls GET /setup for progress.
	tracker *setup.Tracker

	// started stamps daemon boot so GET /stats can report uptime without any external timer.
	started time.Time
}

// New builds the HTTP API over the supervisor (which hosts all agents) and the shared store.
func New(store *session.Store, hub *stream.Hub, sup *orchestrator.Supervisor) *API {
	return &API{store: store, hub: hub, sup: sup, tracker: setup.NewTracker(), started: time.Now()}
}

// Route is one HTTP route as data (method + Go 1.22 mux pattern + handler). Keeping the surface
// as a table lets Register register it in a loop AND lets the OpenAPI parity test enumerate the
// live routes and assert they match daemon/openapi.json — so the spec can't silently drift.
type Route struct {
	Method  string
	Pattern string
	Handler http.HandlerFunc
}

// Routes is the full authenticated API surface. Every agent-specific route accepts ?agent=<type>.
func (a *API) Routes() []Route {
	return []Route{
		// Turns + streaming
		{"POST", "/turns", a.turn},
		{"GET", "/runs/{id}", a.getRun},
		{"GET", "/runs/{id}/children", a.runChildren},
		{"POST", "/runs/{id}/cancel", a.cancelRun},
		{"POST", "/runs/{id}/respond", a.respondRun},
		{"POST", "/runs/{id}/input", a.inputRun},
		{"POST", "/runs/{id}/interrupt", a.interruptRun},
		{"POST", "/runs/{id}/set-model", a.setModelRun},
		{"POST", "/runs/{id}/set-permission-mode", a.setPermissionModeRun},
		{"GET", "/runs/{id}/stream", a.streamRun},
		{"GET", "/doctor", a.doctor},
		{"GET", "/stats", a.stats},
		{"GET", "/processes/stream", a.processesStreamHandler},
		{"GET", "/chats", a.chats},
		{"GET", "/chats/{id}/messages", a.messages},
		{"GET", "/chats/{id}/run", a.chatRun},
		// Session lifecycle: rename (user title wins over the native auto-title), true delete (purges
		// mindwire bookkeeping AND every mapped agent's native transcript — irreversible), and fork
		// (clone the chat into a new id; the first turn branches the native session, Claude natively).
		{"PUT", "/chats/{id}", a.renameChat},
		{"DELETE", "/chats/{id}", a.deleteChat},
		{"POST", "/chats/{id}/fork", a.forkChat},
		{"POST", "/chats/{id}/compact", a.compactChat},
		// Agent lifecycle (daemon owns it; app fetches/triggers).
		{"GET", "/catalog", a.catalog},
		{"GET", "/agent", a.agentInfo},
		{"GET", "/models", a.models},
		{"POST", "/setup", a.setup},
		{"POST", "/update", a.update},
		{"GET", "/setup", a.setupStatus},
		{"GET", "/config", a.getConfig},
		{"PUT", "/config", a.setConfig},
		// Persistent prompt/memory surface (optional per agent — type-asserted in the handler; a
		// non-supporting agent 400s). Memory = CLAUDE.md / AGENTS.md by canonical scope; prompts =
		// saved slash-command / prompt templates.
		{"GET", "/memory", a.getMemory},
		{"PUT", "/memory", a.setMemory},
		{"DELETE", "/memory", a.deleteMemory},
		{"GET", "/prompts", a.listPrompts},
		{"GET", "/prompts/{name}", a.getPrompt},
		{"PUT", "/prompts/{name}", a.setPrompt},
		{"DELETE", "/prompts/{name}", a.deletePrompt},
		// Persistent subagent definitions (optional per agent — type-asserted; Claude-only today). The
		// on-disk .claude/agents/*.md store, distinct from the per-turn --agents passthrough.
		{"GET", "/subagents", a.listSubagents},
		{"GET", "/subagents/{name}", a.getSubagent},
		{"PUT", "/subagents/{name}", a.setSubagent},
		{"DELETE", "/subagents/{name}", a.deleteSubagent},
		// Persistent MCP-server config (optional per agent — type-asserted). The on-disk store Claude
		// (.claude.json / .mcp.json) and Codex (config.toml [mcp_servers.*]) load every run, distinct
		// from the per-turn --mcp-config passthrough.
		{"GET", "/mcp", a.listMCP},
		{"GET", "/mcp/{name}", a.getMCP},
		{"PUT", "/mcp/{name}", a.setMCP},
		{"DELETE", "/mcp/{name}", a.deleteMCP},
		// Custom-LLM-provider registration (optional per agent — type-asserted). Writes the harness's
		// native custom-endpoint config (opencode.json provider.<id>, Codex config.toml
		// [model_providers.<id>]); the key is stored in the daemon and referenced only via an env-var
		// placeholder, never written literally.
		{"GET", "/providers", a.listProviders},
		{"GET", "/providers/{id}", a.getProvider},
		{"PUT", "/providers/{id}", a.setProvider},
		{"DELETE", "/providers/{id}", a.deleteProvider},
		// Auth (step-flow; client sees an options list, adapter handles each).
		{"GET", "/auth/methods", a.authMethods},
		{"POST", "/auth/begin", a.authBegin},
		{"POST", "/auth/step", a.authStep},
		{"GET", "/auth/status", a.authStatusHandler},
		// Notifications: daemon-wide (the client PUTs a webhook URL + optional token).
		{"PUT", "/notify/config", a.setNotifyConfig},
		{"GET", "/notify/config", a.notifyConfig},
		{"GET", "/notify/stream", a.notifyStreamHandler},
		// Daemon-driven fan-out: named channels (webhook/slack/discord/telegram) + routing rules
		// (global / per-agent / per-session, with event selection). Secrets (token/secret/header
		// values) are write-only — masked on read, merge-preserved on write.
		{"GET", "/notify/channels", a.listNotifyChannels},
		{"POST", "/notify/channels", a.createNotifyChannel},
		{"PUT", "/notify/channels/{id}", a.setNotifyChannel},
		{"DELETE", "/notify/channels/{id}", a.deleteNotifyChannel},
		{"POST", "/notify/channels/{id}/test", a.testNotifyChannel},
		{"GET", "/notify/rules", a.listNotifyRules},
		{"POST", "/notify/rules", a.createNotifyRule},
		{"PUT", "/notify/rules/{id}", a.setNotifyRule},
		{"DELETE", "/notify/rules/{id}", a.deleteNotifyRule},
	}
}

// PublicRoutes are served OUTSIDE the auth middleware (registered directly in cmd/daemon, not
// here). Listed as data so the OpenAPI parity test can cover them.
var PublicRoutes = []Route{
	{Method: "GET", Pattern: "/healthz"},
}

func (a *API) Register(mux *http.ServeMux) {
	for _, rt := range a.Routes() {
		mux.HandleFunc(rt.Method+" "+rt.Pattern, rt.Handler)
	}
}

// Auth wraps a handler with a static bearer-token check (constant-time). Empty token (dev)
// passes through.
func Auth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// agentFor resolves the request's agent runtime (?agent=<type>, else default), writing a
// 400 and returning nil if the type is unknown.
func (a *API) agentFor(w http.ResponseWriter, r *http.Request) *orchestrator.Agent {
	ag, ok := a.sup.Resolve(r.URL.Query().Get("agent"))
	if !ok {
		badRequest(w, "unknown agent")
		return nil
	}
	return ag
}

// ---- turns -----------------------------------------------------------------

type turnReq struct {
	ChatID  string            `json:"chatId"`
	Message string            `json:"message"`
	Cwd     string            `json:"cwd,omitempty"`     // run this turn in a specific dir (a project workdir); else the daemon default
	Options agent.TurnOptions `json:"options,omitempty"` // per-turn options (canon-addressed overrides + structured fields)
	// Mode selects the run shape. "" / "turn" (default) = one ordinary turn that ends at the CLI's
	// terminal result. "resolve" = a GLOBAL-RESOLVE run: the daemon holds the run open and auto-continues
	// the agent's own multi-step work until the task is globally complete, returning a parent Run whose
	// children are the per-iteration turns. Any other value is a 400.
	Mode string `json:"mode,omitempty"`
	// Resolve bounds a resolve-mode run (ignored unless Mode == "resolve"). Both fields are optional; a
	// zero value falls back to the daemon defaults (max iterations / overall deadline).
	Resolve *resolveOpts `json:"resolve,omitempty"`
}

// resolveOpts is the optional per-request bound on a resolve run (see turnReq.Resolve). MaxIterations
// caps the auto-continued iterations before StopReason "capped"; DeadlineSeconds is the overall
// wall-clock budget for the whole resolve. Zero on either falls back to the daemon default.
type resolveOpts struct {
	MaxIterations   int `json:"maxIterations,omitempty"`
	DeadlineSeconds int `json:"deadlineSeconds,omitempty"`
}

func (a *API) turn(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	var req turnReq
	if err := decode(w, r, &req); err != nil || req.ChatID == "" || req.Message == "" {
		badRequest(w, "chatId and message are required")
		return
	}
	// Honest capability gate: reject a turn carrying an option the selected agent can't honor (a 400)
	// rather than silently dropping it. Covers both entry points for the prompt overrides — the typed
	// TurnOptions field and the canon-addressed setting.
	if msg, ok := agent.UnsupportedTurnOption(ag.Adapter.Capabilities(), req.Options); !ok {
		badRequest(w, msg)
		return
	}
	in := orchestrator.StartTurnInput{
		ChatID: req.ChatID, Message: req.Message, CWD: req.Cwd, Options: req.Options,
	}
	// Mode routing: "resolve" holds the run open and auto-continues to global completion (a parent Run);
	// "" / "turn" is the unchanged single-turn path. Any other value is a caller error.
	switch req.Mode {
	case "", "turn":
		run, ok := a.sup.StartTurn(ag, in)
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a turn is already running for this chat"})
			return
		}
		writeJSON(w, http.StatusAccepted, run)
	case "resolve":
		var ro orchestrator.ResolveOptions
		if req.Resolve != nil {
			ro.MaxIterations = req.Resolve.MaxIterations
			ro.Deadline = time.Duration(req.Resolve.DeadlineSeconds) * time.Second
		}
		run, ok := a.sup.StartResolve(ag, in, ro)
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a turn is already running for this chat"})
			return
		}
		writeJSON(w, http.StatusAccepted, run)
	default:
		badRequest(w, "mode must be \"turn\" or \"resolve\"")
	}
}

func (a *API) getRun(w http.ResponseWriter, r *http.Request) {
	run, ok := a.store.GetRun(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// statsResp is the daemon PROCESS's own resource snapshot — deliberately cheap and dependency-free:
// everything here comes from the Go runtime (no /proc parsing, no cgo, no sampling), so it costs a
// single ReadMemStats and is safe to serve on demand. It reports the daemon's footprint (heap in use,
// memory reserved from the OS, goroutines, GC cycles) plus host facts (cores, platform, uptime) — NOT
// the whole machine's RAM/CPU, which can't be read cheaply cross-platform from pure stdlib.
type statsResp struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	GoVersion     string `json:"goVersion"`
	NumCPU        int    `json:"numCpu"`        // logical cores visible to the daemon
	NumGoroutine  int    `json:"numGoroutine"`  // live goroutines (rough concurrency gauge)
	MemAllocBytes uint64 `json:"memAllocBytes"` // heap objects currently in use (runtime HeapAlloc)
	MemSysBytes   uint64 `json:"memSysBytes"`   // total memory reserved from the OS (runtime Sys)
	NumGC         uint32 `json:"numGc"`         // completed GC cycles since boot
	UptimeSeconds int64  `json:"uptimeSeconds"` // seconds since the daemon started
}

// stats reports the daemon process's resource snapshot. On-demand only — the client fetches it when a
// user opens a daemon's page; the daemon never samples in the background.
func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	writeJSON(w, http.StatusOK, statsResp{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GoVersion:     runtime.Version(),
		NumCPU:        runtime.NumCPU(),
		NumGoroutine:  runtime.NumGoroutine(),
		MemAllocBytes: m.HeapAlloc,
		MemSysBytes:   m.Sys,
		NumGC:         m.NumGC,
		UptimeSeconds: int64(time.Since(a.started).Seconds()),
	})
}

// runChildren returns the child runs of a resolve parent, oldest→newest — the per-iteration turns of a
// global-resolve run (each streams onto the parent's topic). 404 if the run id is unknown; an ordinary
// turn (or a parent with no iterations yet) returns an empty list, never an error.
func (a *API) runChildren(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := a.store.GetRun(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	children := a.store.Children(id)
	if children == nil {
		children = []session.Run{}
	}
	writeJSON(w, http.StatusOK, children)
}

// chatRun returns the latest run for a chat — the app's reattach anchor: on reopen it fetches this
// and, if the run is still "running", subscribes to /runs/{id}/stream to resume the live turn.
// 204 when the chat has no runs yet.
func (a *API) chatRun(w http.ResponseWriter, r *http.Request) {
	run, ok := a.store.LatestRun(r.PathValue("id"))
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// cancelRun stops an in-flight turn (cancels its context, which kills the CLI). The run
// then ends as "cancelled". 404 if no turn is currently running for that id; 400 if the
// run's agent declares no cancel capability.
func (a *API) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := a.store.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if ag, ok := a.sup.Resolve(run.Agent); ok && !ag.Adapter.Capabilities().Cancel {
		badRequest(w, "this agent does not support cancellation")
		return
	}
	if !a.sup.Cancel(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no running turn for that id"})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// respondReq is the body of POST /runs/{id}/respond: the user's answer to a mid-turn interaction
// (a permission approval or an AskUserQuestion/ExitPlanMode reply). interactionId ties the answer to
// the interaction the turn is waiting on; decision is the approval verdict (allow/deny) and/or text
// is the free-form answer; options carries a multi-select answer.
type respondReq struct {
	InteractionID string   `json:"interactionId,omitempty"`
	Decision      string   `json:"decision,omitempty"`
	Options       []string `json:"options,omitempty"`
	Text          string   `json:"text,omitempty"`
}

// respondRun feeds the user's answer to a waiting interaction back into the running turn. 404 if no
// turn is currently accepting ingress for that id; 400 if the run's agent declares no respond capability.
func (a *API) respondRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := a.store.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if ag, ok := a.sup.Resolve(run.Agent); ok && !ag.Adapter.Capabilities().Respond {
		badRequest(w, "this agent does not support responding to interactions")
		return
	}
	var req respondReq
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid request body")
		return
	}
	if !a.sup.Respond(id, req.InteractionID, req.Decision, req.Options, req.Text) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no running turn accepting input for that id"})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// inputReq is the body of POST /runs/{id}/input: a follow-up message steered into the running turn.
type inputReq struct {
	Text string `json:"text"`
}

// inputRun queues a follow-up user message into the running turn (without cancelling it). 404 if no
// turn is accepting ingress for that id; 400 if the run's agent declares no input capability, or the
// message is empty.
func (a *API) inputRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := a.store.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if ag, ok := a.sup.Resolve(run.Agent); ok && !ag.Adapter.Capabilities().Input {
		badRequest(w, "this agent does not support mid-turn input")
		return
	}
	var req inputReq
	if err := decode(w, r, &req); err != nil || req.Text == "" {
		badRequest(w, "text is required")
		return
	}
	if !a.sup.SendInput(id, req.Text) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no running turn accepting input for that id"})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// interruptRun soft-stops a running turn (asks the agent to halt current work) without the hard
// context kill /cancel does — the turn stays active for a follow-up over /input. 404 if no turn is
// accepting ingress for that id; 400 if the run's agent declares no interrupt capability.
func (a *API) interruptRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := a.store.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if ag, ok := a.sup.Resolve(run.Agent); ok && !ag.Adapter.Capabilities().Interrupt {
		badRequest(w, "this agent does not support interrupts")
		return
	}
	if !a.sup.Interrupt(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no running turn accepting input for that id"})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// setModelReq is the body of POST /runs/{id}/set-model: the model to switch the live turn to. An
// empty model resets the turn to the agent/CLI default.
type setModelReq struct {
	Model string `json:"model,omitempty"`
}

// setModelRun switches the model of a live turn over the control channel. Only meaningful on a
// persistent (non-bypass) turn; on a one-shot turn the send is a documented best-effort no-op (still
// 202). 404 if no turn is accepting ingress for that id; 400 if the run's agent declares no set-model
// capability.
func (a *API) setModelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := a.store.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if ag, ok := a.sup.Resolve(run.Agent); ok && !ag.Adapter.Capabilities().SetModel {
		badRequest(w, "this agent does not support switching the model mid-turn")
		return
	}
	var req setModelReq
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid request body")
		return
	}
	if !a.sup.SetModel(id, req.Model) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no running turn accepting input for that id"})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// setPermissionModeReq is the body of POST /runs/{id}/set-permission-mode: the permission mode to
// switch the live turn to (required).
type setPermissionModeReq struct {
	Mode string `json:"mode"`
}

// setPermissionModeRun switches the permission mode of a live turn over the control channel. Same
// persistent-only best-effort semantics as setModelRun. 404 if no turn is accepting ingress for that
// id; 400 if the run's agent declares no set-permission-mode capability, or the mode is empty.
func (a *API) setPermissionModeRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := a.store.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if ag, ok := a.sup.Resolve(run.Agent); ok && !ag.Adapter.Capabilities().SetPermissionMode {
		badRequest(w, "this agent does not support switching the permission mode mid-turn")
		return
	}
	var req setPermissionModeReq
	if err := decode(w, r, &req); err != nil || req.Mode == "" {
		badRequest(w, "mode is required")
		return
	}
	if !a.sup.SetPermissionMode(id, req.Mode) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no running turn accepting input for that id"})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// doctor reports daemon-level health (workspace, auth) plus the selected agent's own
// checks (Adapter.Doctor) — one report, generic at the daemon, extended per agent. The report is
// built by the supervisor so the HTTP surface and the in-process Go SDK stay identical.
func (a *API) doctor(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	writeJSON(w, http.StatusOK, a.sup.Doctor(r.Context(), ag))
}

// streamRun is the unified SSE event stream for a run: replay buffer then live events.
// A run that already reached a terminal state (including one persisted before a daemon
// restart, for which the hub holds no live topic) replays and closes rather than hanging.
func (a *API) streamRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := a.store.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	sseHeaders(w)

	replay, ch, done, cancel := a.hub.Subscribe(id)
	defer cancel()

	send := func(ev agent.Event) {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}
	// Immediate sentinel: a real, decodable event flushed the instant the stream opens, BEFORE any
	// replay or model output. On a proxy that streams per-event the client sees this in <1s and knows
	// the transport is live; on a buffering proxy it (like everything else) is withheld until the run
	// ends, so the client's first-event watchdog correctly flags "not live". The client ignores it.
	send(agent.Event{Type: agent.EventStatus, Meta: map[string]any{"stream": "open"}})
	for _, ev := range replay {
		send(ev)
	}
	// Terminal run: nothing more will be published. Close a phantom topic (freshly created
	// by Subscribe after a restart) so it gets reaped, and end the stream.
	if done || run.Status != "running" {
		if !done {
			a.hub.Close(id)
		}
		return
	}

	ctx := r.Context()
	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_, _ = w.Write([]byte(": ping\n\n")) // comment keepalive; the client skips it
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			send(ev)
		}
	}
}

// chats lists the chats the daemon has recorded (for a sessions sidebar). Shared across
// agents (keyed by chatId); each summary carries the agent that last ran it.
func (a *API) chats(w http.ResponseWriter, _ *http.Request) {
	summaries := a.store.Chats()
	for i := range summaries {
		a.enrichNativeTitle(&summaries[i])
	}
	writeJSON(w, http.StatusOK, summaries)
}

// enrichNativeTitle overlays the AGENT's own auto-generated title (a Titler, e.g. Claude reads its
// transcript's ai-title record) onto a summary — UNLESS the user has set an explicit title (a rename),
// which always wins. Precedence: user title > native auto-title > derived first-message snippet.
// Best-effort — a missing adapter/session/cwd/title leaves the summary's existing title intact.
func (a *API) enrichNativeTitle(s *session.ChatSummary) {
	if a.store.Title(s.ChatID) != "" {
		return // a user rename overrides the agent's native auto-title
	}
	ag, ok := a.sup.Resolve(s.Agent)
	if !ok {
		return
	}
	titler, ok := ag.Adapter.(agent.Titler)
	if !ok {
		return
	}
	sid := a.store.Session(ag.ID(), s.ChatID)
	if sid == "" {
		return
	}
	cwd := a.store.ChatCWD(s.ChatID)
	if cwd == "" {
		cwd = a.sup.CWD()
	}
	if t := titler.Title(agent.HistoryQuery{ChatID: s.ChatID, SessionID: sid, CWD: cwd}); t != "" {
		s.Title = t
	}
}

// renameChat sets a user-chosen title for a chat (PUT /chats/{id}, body {title}). The user title wins
// over the agent's native auto-title in every listing. An empty title clears the rename (reverting to
// the native/derived title). Returns the updated summary.
func (a *API) renameChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Title string `json:"title"`
	}
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	if err := a.store.SetTitle(id, strings.TrimSpace(req.Title)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist title"})
		return
	}
	summary := a.store.ChatSummaryFor(id)
	a.enrichNativeTitle(&summary)
	writeJSON(w, http.StatusOK, summary)
}

// deleteResult is the DELETE /chats/{id} response: the bookkeeping was purged, plus which agents' native
// transcripts were removed vs. failed to remove (best-effort, per session the chat mapped to).
type deleteResult struct {
	Deleted      bool     `json:"deleted"`
	Sessions     int      `json:"sessions"`               // how many (agent, session) mappings the chat had
	NativePurged []string `json:"nativePurged,omitempty"` // agent types whose native transcript was removed
	NativeFailed []string `json:"nativeFailed,omitempty"` // agent types whose native delete errored
}

// deleteChat is a true, irreversible delete: it purges ALL of the chat's mindwire bookkeeping and, for
// every session the chat mapped to, removes that agent's native transcript (the source of truth). 409
// if a turn is live. Native deletion is best-effort per agent — an adapter without HistoryDeleter (or a
// failing remove) still leaves the bookkeeping purged; the response reports what happened.
func (a *API) deleteChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if a.sup.Busy(id) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a turn is running for this chat"})
		return
	}
	refs, err := a.store.DeleteChat(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete chat"})
		return
	}
	res := deleteResult{Deleted: true, Sessions: len(refs)}
	for _, ref := range refs {
		ag, ok := a.sup.Resolve(ref.Agent)
		if !ok {
			continue
		}
		del, ok := ag.Adapter.(agent.HistoryDeleter)
		if !ok {
			continue // agent keeps no deletable native transcript — bookkeeping purge is enough
		}
		cwd := ref.CWD
		if cwd == "" {
			cwd = a.sup.CWD()
		}
		removed, derr := del.DeleteHistory(agent.HistoryQuery{ChatID: id, SessionID: ref.SID, CWD: cwd})
		switch {
		case derr != nil:
			res.NativeFailed = append(res.NativeFailed, ref.Agent)
		case removed:
			res.NativePurged = append(res.NativePurged, ref.Agent)
		}
		// A genuinely-absent transcript (false, nil) goes to neither list — idempotent success
		// without a false "purged" claim.
	}
	writeJSON(w, http.StatusOK, res)
}

// forkChat clones a chat into a new id (POST /chats/{id}/fork, body {newChatId?} — generated if
// omitted). The fork shares the source's native session until its first turn, which branches it
// (natively on Claude via --fork-session; a fresh session on agents without native fork). 409 if the
// source has a live turn; 404 if the source is unknown; 400 if the target id is already in use.
func (a *API) forkChat(w http.ResponseWriter, r *http.Request) {
	src := r.PathValue("id")
	if a.sup.Busy(src) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a turn is running for this chat"})
		return
	}
	var req struct {
		NewChatID string `json:"newChatId"`
	}
	// An empty body is allowed (the id is generated); only a malformed body is a 400.
	if r.ContentLength != 0 {
		if err := decode(w, r, &req); err != nil {
			badRequest(w, "invalid body")
			return
		}
	}
	newID := strings.TrimSpace(req.NewChatID)
	if newID == "" {
		newID = newChatID()
	}
	if err := a.store.ForkChat(src, newID); err != nil {
		// A missing source is a 404; a name clash or other validation is a 400.
		if strings.Contains(err.Error(), "not found") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			badRequest(w, err.Error())
		}
		return
	}
	summary := a.store.ChatSummaryFor(newID)
	a.enrichNativeTitle(&summary)
	writeJSON(w, http.StatusOK, summary)
}

// compactChat runs an on-demand conversation compaction as a first-class run (POST /chats/{id}/compact,
// body {instructions?}). It's the compact-now surface for Feature 4: the run streams and records the
// compaction boundary exactly like an auto-compaction (an EventCompaction + a "compaction" transcript
// part). Gated on the selected agent implementing agent.CompactModule (400 otherwise) and on the chat
// having a native session to compact (400 otherwise). Optional `instructions` focus the summary
// (Claude's `/compact <instructions>`); agents that ignore focus still compact. 202 with the Run, like a
// turn; 409 if a turn (or another compaction) is already running for the chat.
func (a *API) compactChat(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	if _, ok := ag.Adapter.(agent.CompactModule); !ok {
		badRequest(w, "agent does not support on-demand compaction")
		return
	}
	id := r.PathValue("id")
	if a.store.Session(ag.ID(), id) == "" {
		badRequest(w, "no conversation to compact yet (run a turn first)")
		return
	}
	var req struct {
		Instructions string `json:"instructions"`
	}
	// An empty body is allowed (compact with no focus); only a malformed body is a 400.
	if r.ContentLength != 0 {
		if err := decode(w, r, &req); err != nil {
			badRequest(w, "invalid body")
			return
		}
	}
	run, ok := a.sup.StartCompact(ag, orchestrator.StartTurnInput{
		ChatID: id, Message: strings.TrimSpace(req.Instructions), CWD: a.store.ChatCWD(id),
	})
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a turn is already running for this chat"})
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (a *API) messages(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	chatID := r.PathValue("id")
	// Pagination (additive, back-compat): no params = whole transcript. `limit` caps to the newest N;
	// `before` is a cursor (message id) that trims to everything strictly older than it, for the app's
	// scroll-to-top load-more. Applied to BOTH the native transcript and the store fallback.
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before := r.URL.Query().Get("before")
	if ag.Adapter.Capabilities().History == agent.SupportNative {
		// Native transcripts are path-scoped: use the dir this chat actually ran in.
		cwd := a.store.ChatCWD(chatID)
		if cwd == "" {
			cwd = a.sup.CWD()
		}
		msgs, err := ag.Adapter.History(agent.HistoryQuery{
			ChatID: chatID, SessionID: a.store.Session(ag.ID(), chatID), CWD: cwd,
		})
		if err == nil && len(msgs) > 0 {
			writeJSON(w, http.StatusOK, pageWindow(msgs, limit, before, func(m agent.Message) string { return m.ID }))
			return
		}
	}
	writeJSON(w, http.StatusOK, pageWindow(a.store.Messages(chatID), limit, before, func(m session.Message) string { return m.ID }))
}

// pageWindow returns the tail window of an oldest→newest message slice: everything strictly BEFORE
// the `before` id (a cursor for older-history paging), then the last `limit` of that (limit 0 = all).
func pageWindow[T any](msgs []T, limit int, before string, id func(T) string) []T {
	if before != "" {
		for i := range msgs {
			if id(msgs[i]) == before {
				msgs = msgs[:i]
				break
			}
		}
	}
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs
}

// ---- agent lifecycle -------------------------------------------------------

func (a *API) catalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": agent.Version, "agents": agent.Catalog()})
}

func (a *API) agentInfo(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	status := ag.Auth.Status(r.Context())
	// modelProviders: the models.dev catalog providers this agent runs, for the client to source/enrich
	// the model picker from the live catalog (the daemon no longer stores it). Always an array on the
	// wire ([] when the agent self-enumerates its full list, e.g. Claude's account API).
	modelProviders := []string{}
	if m, ok := ag.Adapter.(agent.ConfiguredModelCatalogModule); ok {
		modelProviders = m.ConfiguredModelCatalogProviders(ag.Creds)
	} else if m, ok := ag.Adapter.(agent.ModelCatalogModule); ok {
		if ps := m.ModelCatalogProviders(); len(ps) > 0 {
			modelProviders = ps
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          agent.Version,
		"agentType":        ag.ID(),
		"name":             ag.Adapter.Meta().Name,
		"capabilities":     ag.Adapter.Capabilities(),
		"schema":           ag.Adapter.Settings(),
		"authMethods":      ag.Auth.Methods(),
		"authStatus":       status,
		"installedVersion": a.sup.CLIVersion(r.Context(), ag),
		"configured":       status.Configured && agent.Configured(ag.Adapter.Settings(), ag.Creds.All()),
		"configPath":       ag.Adapter.ConfigPath(),
		"modelProviders":   modelProviders,
	})
}

// models lists the models the selected agent can run for the configured account. 400 when the agent
// can't enumerate models (no ModelsModule) — the model field is free text for that agent. An empty
// list is a valid 200 (no credentials yet / offline), never an error.
func (a *API) models(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.ModelsModule)
	if !ok {
		badRequest(w, "agent does not support model listing")
		return
	}
	models, err := mod.Models(ag.Auth.EnvForRun())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if models == nil {
		models = []agent.ModelInfo{}
	}
	writeJSON(w, http.StatusOK, models)
}

func (a *API) setup(w http.ResponseWriter, r *http.Request)  { a.runSetup(w, r, false) }
func (a *API) update(w http.ResponseWriter, r *http.Request) { a.runSetup(w, r, true) }

// runSetup kicks off the agent's toolchain install in the BACKGROUND and returns immediately, so
// the client's request never hangs through the proxy for the length of an `npm i` (which times the
// proxy out). The client polls GET /setup for progress + completion. Only one install runs at a
// time per agent (a concurrent POST re-attaches to the in-flight job — idempotent, atomic).
func (a *API) runSetup(w http.ResponseWriter, r *http.Request, force bool) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	steps, err := setup.Plan(ag.Adapter.InstallSteps())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, a.tracker.Start(ag.ID(), steps, force, maxSetup))
}

// setupStatus reports the current toolchain install state for the agent (polled by the client).
func (a *API) setupStatus(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	writeJSON(w, http.StatusOK, a.tracker.Status(ag.ID()))
}

// setConfig merges ONLY recognized settings keys into the agent's namespaced config.
func (a *API) setConfig(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	var cfg map[string]string
	if err := decode(w, r, &cfg); err != nil {
		badRequest(w, "invalid body")
		return
	}
	schema := ag.Adapter.Settings()
	for k, v := range cfg {
		raw, ok := agent.ResolveSettingKey(schema, k)
		if !ok {
			continue // neither a declared non-secret raw key nor a canon resolving to one
		}
		if err := ag.Creds.Set(raw, v); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist settings"})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// getConfig returns only the declared, non-secret settings for the agent so a client can prefill.
func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	allow := agent.SettingsKeys(ag.Adapter.Settings())
	out := map[string]string{}
	for k, v := range ag.Creds.All() {
		if allow[k] {
			out[k] = v
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- prompts & memory ------------------------------------------------------

// dirParam resolves the project directory a project-scope memory/prompt op targets: an explicit
// ?dir= wins, else the daemon's own cwd (matching how chats/messages resolve a chat's cwd). Keeps
// os out of api.go via agent.ResolveDir.
func (a *API) dirParam(r *http.Request) string {
	return agent.ResolveDir(r.URL.Query().Get("dir"), a.sup.CWD())
}

// promptScope reads the ?scope= query for a single-template op, defaulting to user — the scope every
// prompt-supporting agent has (Codex is user-only; Claude also has project). An unknown value falls
// through to the module, which rejects it with a 400.
func promptScope(r *http.Request) agent.MemoryScope {
	if s := strings.TrimSpace(r.URL.Query().Get("scope")); s != "" {
		return agent.MemoryScope(s)
	}
	return agent.MemoryUser
}

// getMemory returns every memory doc the agent exposes — one per supported scope, each carrying its
// resolved path and whether the file exists. 400 when the agent has no memory module.
func (a *API) getMemory(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.MemoryModule)
	if !ok {
		badRequest(w, "agent does not support memory files")
		return
	}
	dir := a.dirParam(r)
	out := []agent.MemoryDoc{}
	for _, scope := range mod.MemoryScopes() {
		doc, err := mod.ReadMemory(scope, dir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out = append(out, doc)
	}
	writeJSON(w, http.StatusOK, out)
}

// setMemory writes one scope's memory file and returns the resulting doc (resolved path, exists:true).
// A bad scope or a missing project directory is a caller error (400).
func (a *API) setMemory(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.MemoryModule)
	if !ok {
		badRequest(w, "agent does not support memory files")
		return
	}
	var req struct {
		Scope   agent.MemoryScope `json:"scope"`
		Content string            `json:"content"`
	}
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	doc, err := mod.WriteMemory(req.Scope, a.dirParam(r), req.Content)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// deleteMemory removes one scope's memory file (?scope=, default user) and returns the resulting doc
// (exists:false). Idempotent — deleting an absent file still succeeds. 400 for a bad scope or a
// non-supporting agent.
func (a *API) deleteMemory(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.MemoryModule)
	if !ok {
		badRequest(w, "agent does not support memory files")
		return
	}
	doc, err := mod.DeleteMemory(promptScope(r), a.dirParam(r))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// listPrompts returns the agent's saved templates across its supported scopes (content omitted). A
// missing prompt directory yields an empty list, not an error. 400 when the agent has no prompts module.
func (a *API) listPrompts(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.PromptsModule)
	if !ok {
		badRequest(w, "agent does not support prompt templates")
		return
	}
	prompts, err := mod.ListPrompts(a.dirParam(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, prompts)
}

// getPrompt returns one template's full content. 404 when it doesn't exist; 400 for an invalid name
// or unsupported scope.
func (a *API) getPrompt(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.PromptsModule)
	if !ok {
		badRequest(w, "agent does not support prompt templates")
		return
	}
	tpl, err := mod.ReadPrompt(promptScope(r), a.dirParam(r), r.PathValue("name"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "prompt not found"})
	case err != nil:
		badRequest(w, err.Error())
	default:
		writeJSON(w, http.StatusOK, tpl)
	}
}

// setPrompt writes one template and returns it. 400 for an invalid name or unsupported scope.
func (a *API) setPrompt(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.PromptsModule)
	if !ok {
		badRequest(w, "agent does not support prompt templates")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	tpl, err := mod.WritePrompt(promptScope(r), a.dirParam(r), r.PathValue("name"), req.Content)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tpl)
}

// deletePrompt removes one template at a scope (?scope=, default user). Idempotent — deleting an
// absent template still succeeds. 400 for an invalid name or unsupported scope.
func (a *API) deletePrompt(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.PromptsModule)
	if !ok {
		badRequest(w, "agent does not support prompt templates")
		return
	}
	if err := mod.DeletePrompt(promptScope(r), a.dirParam(r), r.PathValue("name")); err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---- subagent definitions --------------------------------------------------

// listSubagents returns the agent's persistent subagent definitions across its supported scopes (raw
// Content omitted, parsed Meta kept). A missing definitions directory yields an empty list, not an
// error. 400 when the agent has no subagent-definition module.
func (a *API) listSubagents(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.SubagentsModule)
	if !ok {
		badRequest(w, "agent does not support subagent definitions")
		return
	}
	subs, err := mod.ListSubagents(a.dirParam(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

// getSubagent returns one definition's full raw Content plus parsed Meta. 404 when it doesn't exist;
// 400 for an invalid name or unsupported scope.
func (a *API) getSubagent(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.SubagentsModule)
	if !ok {
		badRequest(w, "agent does not support subagent definitions")
		return
	}
	sub, err := mod.ReadSubagent(promptScope(r), a.dirParam(r), r.PathValue("name"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "subagent not found"})
	case err != nil:
		badRequest(w, err.Error())
	default:
		writeJSON(w, http.StatusOK, sub)
	}
}

// setSubagent writes one definition verbatim and returns it (raw Content + parsed Meta). 400 for an
// invalid name or unsupported scope.
func (a *API) setSubagent(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.SubagentsModule)
	if !ok {
		badRequest(w, "agent does not support subagent definitions")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	sub, err := mod.WriteSubagent(promptScope(r), a.dirParam(r), r.PathValue("name"), req.Content)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// deleteSubagent removes one definition at a scope (?scope=, default user). Idempotent — deleting an
// absent definition still succeeds. 400 for an invalid name or unsupported scope.
func (a *API) deleteSubagent(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.SubagentsModule)
	if !ok {
		badRequest(w, "agent does not support subagent definitions")
		return
	}
	if err := mod.DeleteSubagent(promptScope(r), a.dirParam(r), r.PathValue("name")); err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---- persistent MCP config -------------------------------------------------

// listMCP returns the agent's persistent MCP servers across every supported scope, keyed
// scope→name→server. A missing config file yields an empty object for that scope, not an error. 400
// when the agent has no persistent MCP-config module.
func (a *API) listMCP(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.MCPServerModule)
	if !ok {
		badRequest(w, "agent does not support persistent MCP config")
		return
	}
	dir := a.dirParam(r)
	out := map[agent.MemoryScope]map[string]agent.MCPServer{}
	for _, scope := range mod.MCPScopes() {
		servers, err := mod.ListMCPServers(scope, dir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if servers == nil {
			servers = map[string]agent.MCPServer{}
		}
		out[scope] = servers
	}
	writeJSON(w, http.StatusOK, out)
}

// getMCP returns one server by name at a scope (?scope=, default user). 404 when it isn't configured;
// 400 for an unsupported scope or a non-supporting agent.
func (a *API) getMCP(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.MCPServerModule)
	if !ok {
		badRequest(w, "agent does not support persistent MCP config")
		return
	}
	servers, err := mod.ListMCPServers(promptScope(r), a.dirParam(r))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	server, ok := servers[r.PathValue("name")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
		return
	}
	writeJSON(w, http.StatusOK, server)
}

// setMCP writes one server at a scope (?scope=, default user) and echoes it back. 400 for an invalid
// name or unsupported scope.
func (a *API) setMCP(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.MCPServerModule)
	if !ok {
		badRequest(w, "agent does not support persistent MCP config")
		return
	}
	var server agent.MCPServer
	if err := decode(w, r, &server); err != nil {
		badRequest(w, "invalid body")
		return
	}
	if err := mod.SetMCPServer(promptScope(r), a.dirParam(r), r.PathValue("name"), server); err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, server)
}

// deleteMCP removes one server at a scope (?scope=, default user). Idempotent — deleting an absent
// server still succeeds. 400 for an unsupported scope or a non-supporting agent.
func (a *API) deleteMCP(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.MCPServerModule)
	if !ok {
		badRequest(w, "agent does not support persistent MCP config")
		return
	}
	if err := mod.DeleteMCPServer(promptScope(r), a.dirParam(r), r.PathValue("name")); err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---- custom providers ------------------------------------------------------

// setProviderRequest is the PUT body: the CustomProvider fields plus the optional secret channels. Both
// are write-only — stored in the daemon and NEVER echoed by any GET (only HasKey / EnvVars names are).
// apiKey is a single key (custom endpoints, single-key catalog brands); secrets is a NAME→VALUE map for
// catalog providers whose entry declares MULTIPLE env vars (e.g. AWS Bedrock).
type setProviderRequest struct {
	agent.CustomProvider
	APIKey  string            `json:"apiKey,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

// listProviders returns the agent's registered custom LLM providers across every supported scope, keyed
// scope→id→provider. A missing config file yields an empty object for that scope, not an error. 400 when
// the agent has no custom-provider module. HasKey reports whether a secret is stored; the key is never
// returned.
func (a *API) listProviders(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.CustomProvidersModule)
	if !ok {
		badRequest(w, "agent does not support custom providers")
		return
	}
	dir := a.dirParam(r)
	out := map[agent.MemoryScope]map[string]agent.CustomProvider{}
	for _, scope := range mod.ProviderScopes() {
		providers, err := mod.ListProviders(ag.Creds, scope, dir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if providers == nil {
			providers = map[string]agent.CustomProvider{}
		}
		out[scope] = providers
	}
	writeJSON(w, http.StatusOK, out)
}

// getProvider returns one provider by id at a scope (?scope=, default user). 404 when it isn't
// configured; 400 for an unsupported scope or a non-supporting agent. The key is never returned.
func (a *API) getProvider(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.CustomProvidersModule)
	if !ok {
		badRequest(w, "agent does not support custom providers")
		return
	}
	providers, err := mod.ListProviders(ag.Creds, promptScope(r), a.dirParam(r))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	provider, ok := providers[r.PathValue("id")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "custom provider not found"})
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

// setProvider registers one provider at a scope (?scope=, default user) and echoes it back (HasKey
// reflects the stored key; the key itself is never returned). The path id wins over any id in the body.
// 400 for an invalid id/base URL or unsupported scope.
func (a *API) setProvider(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.CustomProvidersModule)
	if !ok {
		badRequest(w, "agent does not support custom providers")
		return
	}
	var req setProviderRequest
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	id := r.PathValue("id")
	req.CustomProvider.ID = id
	if err := mod.SetProvider(ag.Creds, promptScope(r), a.dirParam(r), id, req.CustomProvider, req.APIKey, req.Secrets); err != nil {
		badRequest(w, err.Error())
		return
	}
	// Re-list so the echo carries the authoritative stored state (HasKey, derived EnvVar) rather than
	// the request as sent.
	providers, err := mod.ListProviders(ag.Creds, promptScope(r), a.dirParam(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if p, ok := providers[id]; ok {
		writeJSON(w, http.StatusOK, p)
		return
	}
	writeJSON(w, http.StatusOK, req.CustomProvider)
}

// deleteProvider removes one provider at a scope (?scope=, default user) and clears its stored key.
// Idempotent — deleting an absent provider still succeeds. 400 for an unsupported scope or a
// non-supporting agent.
func (a *API) deleteProvider(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	mod, ok := ag.Adapter.(agent.CustomProvidersModule)
	if !ok {
		badRequest(w, "agent does not support custom providers")
		return
	}
	if err := mod.DeleteProvider(ag.Creds, promptScope(r), a.dirParam(r), r.PathValue("id")); err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---- auth (step-flow) ------------------------------------------------------

func (a *API) authMethods(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	writeJSON(w, http.StatusOK, ag.Auth.Methods())
}

func (a *API) authBegin(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	var req struct {
		Method string `json:"method"`
	}
	if err := decode(w, r, &req); err != nil || req.Method == "" {
		badRequest(w, "method is required")
		return
	}
	st, err := ag.Auth.Begin(r.Context(), req.Method)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) authStep(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	var input map[string]string
	if err := decode(w, r, &input); err != nil {
		badRequest(w, "invalid body")
		return
	}
	st, err := ag.Auth.Step(r.Context(), input)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) authStatusHandler(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	writeJSON(w, http.StatusOK, ag.Auth.Status(r.Context()))
}

// ---- notifications ----------------------------------------------------------

type notifyConfigReq struct {
	URL     string `json:"url"`
	Channel string `json:"channel"`
	Token   string `json:"token"`
}

// setNotifyConfig stores the notification webhook the client provisioned (daemon-wide).
func (a *API) setNotifyConfig(w http.ResponseWriter, r *http.Request) {
	var req notifyConfigReq
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	if req.URL == "" || req.Channel == "" {
		badRequest(w, "url and channel are required")
		return
	}
	if err := a.store.SetNotifyConfig(req.URL, req.Channel, req.Token); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist notification config"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// notifyConfig reports whether a channel is wired. The token is never returned.
func (a *API) notifyConfig(w http.ResponseWriter, _ *http.Request) {
	url, channel, _ := a.store.NotifyConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": url != "" && channel != "",
		"url":        url,
		"channel":    channel,
	})
}

// notifyStreamHandler is an SSE feed of the daemon's notifications — the same payloads
// pushed to the webhook — so a local client can show them live without a receiver. Replay first,
// then live.
func (a *API) notifyStreamHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	sseHeaders(w)

	ch, replay, cancel := a.sup.Notes().Subscribe()
	defer cancel()

	send := func(n agent.Notification) {
		b, err := json.Marshal(n)
		if err != nil {
			return
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}
	for _, n := range replay {
		send(n)
	}
	flusher.Flush()

	ctx := r.Context()
	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case n, open := <-ch:
			if !open {
				return
			}
			send(n)
		}
	}
}

// processesStreamHandler streams live per-turn CPU/memory as SSE. It Subscribe()s the on-demand
// sampler — which starts the moment this is the first subscriber and stops when the last client
// disconnects — so no sampling happens unless a page is watching. An optional ?agent= filters each
// frame's samples to one agent (the console's Agent page uses it). No secrets in the payload: labels
// + numbers only. The subscriber cap surfaces as 503 rather than blocking.
func (a *API) processesStreamHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	ch, cancel, ok := a.sup.ProcMon().Subscribe()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "too many resource watchers"})
		return
	}
	defer cancel()

	agentFilter := r.URL.Query().Get("agent")
	sseHeaders(w)

	send := func(f procmon.Frame) {
		if agentFilter != "" {
			kept := make([]procmon.Sample, 0, len(f.Samples))
			for _, s := range f.Samples {
				if s.Agent == agentFilter {
					kept = append(kept, s)
				}
			}
			f.Samples = kept
		}
		b, err := json.Marshal(f)
		if err != nil {
			return
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}
	flusher.Flush()

	ctx := r.Context()
	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case f, open := <-ch:
			if !open {
				return
			}
			send(f)
		}
	}
}

// ---- notification channels + rules (daemon-driven fan-out) ------------------
//
// Channels are named delivery targets; rules route matching notifications to them (global /
// per-agent / per-session, with optional event selection). Secrets — a channel's token, HMAC
// secret, and header VALUES — are write-only: masked on every read, and a PUT that omits them
// preserves the stored value (so a UI round-trip of the masked view can't wipe a secret). The
// channel URL is itself treated as secret (Slack/Discord/Telegram URLs embed tokens): reads
// return only its host, and an omitted/empty URL on PUT preserves the stored one.

// maskedChannel is the read-side DTO: it never carries the URL, token, HMAC secret, or header
// values — only their presence (and the URL host, as a display hint).
type maskedChannel struct {
	ID         string                  `json:"id"`
	Type       agent.NotifyChannelType `json:"type"`
	Label      string                  `json:"label,omitempty"`
	URLHost    string                  `json:"urlHost,omitempty"`
	HasURL     bool                    `json:"hasUrl"`
	HeaderKeys []string                `json:"headerKeys,omitempty"`
	HasToken   bool                    `json:"hasToken"`
	HasSecret  bool                    `json:"hasSecret"`
	Enabled    bool                    `json:"enabled"`
}

func maskChannel(c agent.NotifyChannel) maskedChannel {
	keys := make([]string, 0, len(c.Headers))
	for k := range c.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	host := ""
	if c.URL != "" {
		if u, err := url.Parse(c.URL); err == nil {
			host = u.Host
		}
	}
	return maskedChannel{
		ID: c.ID, Type: c.Type, Label: c.Label,
		URLHost: host, HasURL: c.URL != "",
		HeaderKeys: keys, HasToken: c.Token != "", HasSecret: c.Secret != "",
		Enabled: c.Enabled,
	}
}

// notifyChannelReq is the write-side body. Every field is a pointer (or, for headers, a map) so
// the handler can tell "field omitted" from "set to empty" — that distinction is what powers
// secret merge-preservation on PUT.
type notifyChannelReq struct {
	Type    *agent.NotifyChannelType `json:"type"`
	Label   *string                  `json:"label"`
	URL     *string                  `json:"url"`
	Headers map[string]string        `json:"headers"`
	Token   *string                  `json:"token"`
	Secret  *string                  `json:"secret"`
	Enabled *bool                    `json:"enabled"`
}

// applyTo merges the request onto a base channel (a zero value for create, the stored channel for
// update). URL/token/secret are applied only when present AND non-empty → an omitted or blank
// secret preserves whatever the base held; headers are replaced only when the key is present (send
// {} to clear them).
func (req notifyChannelReq) applyTo(c agent.NotifyChannel) agent.NotifyChannel {
	if req.Type != nil {
		c.Type = *req.Type
	}
	if req.Label != nil {
		c.Label = *req.Label
	}
	if req.URL != nil && *req.URL != "" {
		c.URL = *req.URL
	}
	if req.Headers != nil {
		c.Headers = req.Headers
	}
	if req.Token != nil && *req.Token != "" {
		c.Token = *req.Token
	}
	if req.Secret != nil && *req.Secret != "" {
		c.Secret = *req.Secret
	}
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
	return c
}

func validNotifyType(t agent.NotifyChannelType) bool {
	switch t {
	case agent.ChannelWebhook, agent.ChannelSlack, agent.ChannelDiscord, agent.ChannelTelegram:
		return true
	}
	return false
}

// listNotifyChannels returns every channel, masked (never any secret).
func (a *API) listNotifyChannels(w http.ResponseWriter, _ *http.Request) {
	chans := a.store.NotifyChannels()
	out := make([]maskedChannel, 0, len(chans))
	for _, c := range chans {
		out = append(out, maskChannel(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// createNotifyChannel mints a channel from the request (server-assigned id) and returns it masked.
func (a *API) createNotifyChannel(w http.ResponseWriter, r *http.Request) {
	var req notifyChannelReq
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	c := req.applyTo(agent.NotifyChannel{})
	if c.Type == "" {
		c.Type = agent.ChannelWebhook // most general shape (raw Notification JSON)
	}
	if req.Enabled == nil {
		c.Enabled = true // a freshly created channel is on unless explicitly disabled
	}
	if !validNotifyType(c.Type) {
		badRequest(w, "type must be one of webhook, slack, discord, telegram")
		return
	}
	if c.URL == "" {
		badRequest(w, "url is required")
		return
	}
	c.ID = newNotifyID()
	if err := a.store.SetNotifyChannel(c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist channel"})
		return
	}
	writeJSON(w, http.StatusCreated, maskChannel(c))
}

// setNotifyChannel updates a channel by id, merge-preserving omitted secrets. 404 if unknown.
func (a *API) setNotifyChannel(w http.ResponseWriter, r *http.Request) {
	existing, ok := a.store.NotifyChannel(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	var req notifyChannelReq
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	c := req.applyTo(existing)
	c.ID = existing.ID
	if !validNotifyType(c.Type) {
		badRequest(w, "type must be one of webhook, slack, discord, telegram")
		return
	}
	if c.URL == "" {
		badRequest(w, "url is required")
		return
	}
	if err := a.store.SetNotifyChannel(c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist channel"})
		return
	}
	writeJSON(w, http.StatusOK, maskChannel(c))
}

// deleteNotifyChannel removes a channel by id. Idempotent.
func (a *API) deleteNotifyChannel(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteNotifyChannel(r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete channel"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// testNotifyChannel delivers a synthetic notification to one channel and reports the outcome. The
// delivery result is DATA, not a transport error: a failed send still returns HTTP 200 with
// {ok:false, error}. 404 only when the channel id is unknown.
func (a *API) testNotifyChannel(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.store.NotifyChannel(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	n := agent.Notification{
		Condition: agent.Finished,
		Title:     "Mindwire test notification",
		Body:      "If you can read this, this channel is wired correctly.",
		Agent:     "mindwire",
	}
	if err := notify.DeliverOne(r.Context(), nil, ch, n); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func validNotifyScope(s agent.NotifyRuleScope) bool {
	switch s {
	case agent.ScopeGlobal, agent.ScopeAgent, agent.ScopeSession:
		return true
	}
	return false
}

// validateRule checks scope coherence and that at least one channel is targeted.
func validateRule(r agent.NotifyRule) string {
	if !validNotifyScope(r.Scope) {
		return "scope must be one of global, agent, session"
	}
	if r.Scope == agent.ScopeAgent && r.Agent == "" {
		return "agent is required for an agent-scoped rule"
	}
	if r.Scope == agent.ScopeSession && r.Session == "" {
		return "session is required for a session-scoped rule"
	}
	if len(r.ChannelIDs) == 0 {
		return "at least one channelId is required"
	}
	return ""
}

// listNotifyRules returns every routing rule (rules carry no secrets).
func (a *API) listNotifyRules(w http.ResponseWriter, _ *http.Request) {
	rules := a.store.NotifyRules()
	if rules == nil {
		rules = []agent.NotifyRule{}
	}
	writeJSON(w, http.StatusOK, rules)
}

// createNotifyRule mints a rule (server-assigned id) and returns it.
func (a *API) createNotifyRule(w http.ResponseWriter, r *http.Request) {
	var rule agent.NotifyRule
	if err := decode(w, r, &rule); err != nil {
		badRequest(w, "invalid body")
		return
	}
	if msg := validateRule(rule); msg != "" {
		badRequest(w, msg)
		return
	}
	rule.ID = newNotifyID()
	if err := a.store.SetNotifyRule(rule); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist rule"})
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

// setNotifyRule updates a rule by id. 404 if unknown.
func (a *API) setNotifyRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.store.NotifyRule(r.PathValue("id")); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
		return
	}
	var rule agent.NotifyRule
	if err := decode(w, r, &rule); err != nil {
		badRequest(w, "invalid body")
		return
	}
	if msg := validateRule(rule); msg != "" {
		badRequest(w, msg)
		return
	}
	rule.ID = r.PathValue("id")
	if err := a.store.SetNotifyRule(rule); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist rule"})
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

// deleteNotifyRule removes a rule by id. Idempotent.
func (a *API) deleteNotifyRule(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteNotifyRule(r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete rule"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---- helpers ---------------------------------------------------------------

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	// `no-cache` stops caching; `no-transform` forbids any intermediary (a reverse proxy) from
	// gzip-compressing this response. Compression is fatal to SSE: the compressor buffers frames
	// until its window fills, so clients that send `Accept-Encoding: gzip` (URLSession does by
	// default) see nothing until the stream closes — the whole turn looks like one HTTP response.
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	// Tell reverse proxies (nginx / a runtime proxy) NOT to buffer this response — without
	// it many proxies hold the whole body until the upstream closes, so SSE events arrive in one
	// batch at the end instead of live. Honored by nginx-family proxies; harmless elsewhere.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

// decode reads a JSON request body with a size cap (MaxBytesReader).
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	return json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}

// newChatID generates a random chat id for a fork whose caller didn't supply one (16 random bytes,
// hex) — the same shape the orchestrator uses for run ids.
func newChatID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// newNotifyID mints a random id for a notification channel or rule (same 16-byte hex shape).
func newNotifyID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
