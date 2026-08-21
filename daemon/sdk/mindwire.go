package mindwire

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"iter"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/setup"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// maxSetup bounds a background toolchain install, matching the HTTP daemon's own cap (api.maxSetup).
const maxSetup = 20 * time.Minute

// Options configures a Client. The zero value is usable: it opens "agent-state.json" in the current
// directory and drives the daemon's default agent with no extra notifier.
type Options struct {
	// Agent is the default agent type for scoped calls (e.g. "claude-code"). Empty selects the
	// daemon's default (the first registered adapter by ID). Override per call with ForAgent.
	Agent string
	// CWD is the working directory turns run in unless a TurnRequest overrides it. Empty uses the
	// process directory (a doctor Workspace check warns when unset, matching the daemon).
	CWD string
	// StatePath is the JSON state file backing sessions, runs, config, and creds. Empty defaults to
	// "agent-state.json" — the same default the daemon binary uses.
	StatePath string
	// Notifier, if set, receives turn notifications alongside the built-in env-configured channels
	// (a webhook, notify/file, notify/exec). nil = just the built-ins.
	Notifier Notifier
}

// core is the state shared by a Client and every WithAgent view of it: the one supervisor/store/hub
// per StatePath, plus the set of run ids this client tree started (so Close can cancel them). It is
// pointer-shared, never copied, so the mutex and run set stay singular across WithAgent.
type core struct {
	store   *session.Store
	hub     *stream.Hub
	sup     *orchestrator.Supervisor
	tracker *setup.Tracker
	cwd     string

	mu     sync.Mutex
	runs   map[string]struct{} // run ids started via this client tree, cancelled on Close
	closed bool
}

// Client is an in-process handle to the mindwire engine: the same orchestrator.Supervisor the daemon
// runs, exposed as a Go library with no HTTP server, subprocess, or network in between. Its method set
// mirrors the TypeScript SDK's Mindwire class one-for-one; every call maps to the same core operation
// the matching HTTP route invokes, so behavior (including the capability gates and 400/404/409 error
// semantics) is identical across the two SDKs.
//
// A Client is safe for concurrent use. One Client owns one StatePath: the one-turn-per-chat gate and
// the JSON store are per-supervisor, so two Clients over the same state file is unsupported (the TS
// SDK's process-global client sidesteps this; a second Go instance would race the file).
type Client struct {
	core         *core
	defaultAgent string
	// Auth is the step-flow authentication sub-API (methods → begin → step → status), scoped to this
	// client's default agent and rebound by WithAgent. Reachable as client.Auth.
	Auth *Auth
	// Prompts is the persistent prompt/memory sub-API (memory files + saved prompt templates), scoped
	// to this client's default agent and rebound by WithAgent. Reachable as client.Prompts.
	Prompts *Prompts
	// MCP is the persistent MCP-server sub-API (the config an agent loads every run, distinct from a
	// turn's per-turn Options.MCPServers), scoped to this client's default agent and rebound by
	// WithAgent. Reachable as client.MCP.
	MCP *MCP
	// Providers is the custom-LLM-provider sub-API (register an OpenAI-compatible endpoint the built-in
	// catalog has never heard of), scoped to this client's default agent and rebound by WithAgent.
	// Reachable as client.Providers.
	Providers *Providers
}

// New constructs a Client, wiring the engine exactly as daemon/cmd/daemon/main.go does minus the HTTP
// server: open the state store, reconcile any run left "running" by a prior process, build the event
// hub, compose the notification fan-out (built-in channels + any Options.Notifier), and construct the
// supervisor over every registered adapter. Import side effects (register.go) must have populated the
// adapter and notify registries first — they do, transitively, from importing this package.
func New(opts Options) (*Client, error) {
	statePath := opts.StatePath
	if statePath == "" {
		statePath = "agent-state.json"
	}
	store, err := session.Open(statePath)
	if err != nil {
		return nil, &Error{Message: "open state file: " + err.Error(), Cause: err}
	}
	// A prior process that died mid-turn left runs marked "running" that nothing will ever finish;
	// mark them errored so a caller reattaching doesn't hang on a topic no one publishes to.
	_ = store.ReconcileRunning("interrupted by daemon restart")

	hub := stream.New()

	channels := notify.All(store)
	if opts.Notifier != nil {
		channels = append(channels, opts.Notifier)
	}
	notifier := notify.Fanout(channels)

	sup := orchestrator.New(store, hub, notifier, opts.CWD, opts.Agent)

	co := &core{
		store: store, hub: hub, sup: sup, tracker: setup.NewTracker(), cwd: opts.CWD,
		runs: map[string]struct{}{},
	}
	c := &Client{core: co, defaultAgent: opts.Agent}
	c.Auth = &Auth{c: c}
	c.Prompts = &Prompts{c: c}
	c.MCP = &MCP{c: c}
	c.Providers = &Providers{c: c}
	return c, nil
}

// WithAgent returns a lightweight view of this Client whose default agent is agentType. It shares the
// same underlying engine (store, hub, supervisor, in-flight run set) — only the scoped-agent default
// and the Auth binding differ — so it is cheap and needs no Close of its own.
func (c *Client) WithAgent(agentType string) *Client {
	view := &Client{core: c.core, defaultAgent: agentType}
	view.Auth = &Auth{c: view}
	view.Prompts = &Prompts{c: view}
	view.MCP = &MCP{c: view}
	view.Providers = &Providers{c: view}
	return view
}

// Close cancels every in-flight turn this client tree started, then blocks until those turns have
// fully wound down — including the terminal state persist each one writes. Draining before returning
// is what makes it safe to tear down the state file/working directory right after Close (an embedder's
// teardown, or a test's TempDir cleanup): without the drain a just-cancelled turn's async SaveRun would
// race the removal. It is idempotent and safe to call from any WithAgent view (they share one run set).
// It does NOT close the state store — a process holds one store for its lifetime — it only stops turns
// this client owns. A second concurrent Close returns immediately; the first one owns the drain.
func (c *Client) Close() error {
	c.core.mu.Lock()
	if c.core.closed {
		c.core.mu.Unlock()
		return nil
	}
	c.core.closed = true
	ids := make([]string, 0, len(c.core.runs))
	for id := range c.core.runs {
		ids = append(ids, id)
	}
	c.core.mu.Unlock()

	for _, id := range ids {
		c.core.sup.Cancel(id) // no-op/false for already-finished runs
	}
	c.core.sup.Wait() // drain: every cancelled turn's final SaveRun lands before we return
	return nil
}

// ---- agent scoping ---------------------------------------------------------

// ScopedOption tunes an agent-scoped call. The only option today is ForAgent.
type ScopedOption func(*scope)

type scope struct{ agent string }

// ForAgent overrides the target agent type for a single scoped call, ignoring the client's default.
func ForAgent(agentType string) ScopedOption {
	return func(s *scope) { s.agent = agentType }
}

// resolve maps a scoped call to its agent runtime: the client default unless ForAgent overrides it.
// An unknown type yields APIError{400 unknown agent}, matching the HTTP layer's agentFor.
func (c *Client) resolve(opts []ScopedOption) (*orchestrator.Agent, error) {
	sc := scope{agent: c.defaultAgent}
	for _, o := range opts {
		o(&sc)
	}
	ag, ok := c.core.sup.Resolve(sc.agent)
	if !ok {
		return nil, &APIError{Message: "unknown agent", Status: http.StatusBadRequest, Op: "resolve"}
	}
	return ag, nil
}

// ---- health & catalog ------------------------------------------------------

// Health reports the daemon-level liveness snapshot (the /healthz payload): always ok in-process,
// plus the default agent type and the core's version.
type Health struct {
	OK      bool   `json:"ok"`
	Agent   string `json:"agent"`
	Version string `json:"version"`
}

// Health returns the liveness snapshot. It cannot fail in-process.
func (c *Client) Health() Health {
	return Health{OK: true, Agent: c.core.sup.Default(), Version: agent.Version}
}

// processStarted anchors the daemon-process uptime the /stats snapshot reports; set once at package
// load so Stats.UptimeSeconds measures how long this process has been running.
var processStarted = time.Now()

// Stats is a cheap, on-demand snapshot of the daemon *process itself* — its Go runtime memory and
// scheduler counters, not machine-wide RAM/CPU (which pure stdlib can't read portably). It mirrors
// GET /stats and is meant to be read when someone's actually looking, never polled.
type Stats struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	GoVersion     string `json:"goVersion"`
	NumCPU        int    `json:"numCpu"`
	NumGoroutine  int    `json:"numGoroutine"`
	MemAllocBytes uint64 `json:"memAllocBytes"`
	MemSysBytes   uint64 `json:"memSysBytes"`
	NumGC         uint32 `json:"numGc"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

// Stats returns the process resource snapshot. It reads the Go runtime at call time and cannot fail.
func (c *Client) Stats() Stats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return Stats{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GoVersion:     runtime.Version(),
		NumCPU:        runtime.NumCPU(),
		NumGoroutine:  runtime.NumGoroutine(),
		MemAllocBytes: m.HeapAlloc,
		MemSysBytes:   m.Sys,
		NumGC:         m.NumGC,
		UptimeSeconds: int64(time.Since(processStarted).Seconds()),
	}
}

// Catalog is the picker payload: the core version plus every agent this build supports.
type Catalog struct {
	Version string         `json:"version"`
	Agents  []CatalogEntry `json:"agents"`
}

// Catalog lists every registered agent (ID-ordered) with the core version.
func (c *Client) Catalog() Catalog {
	return Catalog{Version: agent.Version, Agents: agent.Catalog()}
}

// Models lists the models available to the scoped agent (mirrors GET /models). Only agents that
// implement the model-listing module answer; others return a 400-status APIError. The list may be
// empty (no credentials / not-yet-fetched) — that is a valid result, not an error.
func (c *Client) Models(opts ...ScopedOption) ([]ModelInfo, error) {
	ag, err := c.resolve(opts)
	if err != nil {
		return nil, err
	}
	mod, ok := ag.Adapter.(agent.ModelsModule)
	if !ok {
		return nil, &APIError{Message: "agent does not support model listing", Status: http.StatusBadRequest, Op: "Models"}
	}
	models, err := mod.Models(ag.Auth.EnvForRun())
	if err != nil {
		return nil, err
	}
	if models == nil {
		models = []ModelInfo{}
	}
	return models, nil
}

// ---- agent info, doctor, setup, config -------------------------------------

// AgentInfo is the full descriptor for one agent: version, identity, capabilities, dynamic settings
// schema, auth methods + resting status, installed CLI version, whether it's configured, and its
// user-editable config path. JSON tags match the HTTP /agent payload exactly.
type AgentInfo struct {
	Version          string         `json:"version"`
	AgentType        string         `json:"agentType"`
	Name             string         `json:"name"`
	Capabilities     Capabilities   `json:"capabilities"`
	Schema           SettingsSchema `json:"schema"`
	AuthMethods      []AuthMethod   `json:"authMethods"`
	AuthStatus       AuthStatus     `json:"authStatus"`
	InstalledVersion string         `json:"installedVersion"`
	Configured       bool           `json:"configured"`
	ConfigPath       string         `json:"configPath"`
}

// Agent returns the full descriptor for the scoped agent. ctx bounds the auth-status probe and the
// CLI-version subprocess (the HTTP handler has no such ctx; the Go SDK threads one so callers can
// cancel these external touches).
func (c *Client) Agent(ctx context.Context, opts ...ScopedOption) (AgentInfo, error) {
	ag, err := c.resolve(opts)
	if err != nil {
		return AgentInfo{}, err
	}
	status := ag.Auth.Status(ctx)
	return AgentInfo{
		Version:          agent.Version,
		AgentType:        ag.ID(),
		Name:             ag.Adapter.Meta().Name,
		Capabilities:     ag.Adapter.Capabilities(),
		Schema:           ag.Adapter.Settings(),
		AuthMethods:      ag.Auth.Methods(),
		AuthStatus:       status,
		InstalledVersion: c.core.sup.CLIVersion(ctx, ag),
		Configured:       status.Configured && agent.Configured(ag.Adapter.Settings(), ag.Creds.All()),
		ConfigPath:       ag.Adapter.ConfigPath(),
	}, nil
}

// Doctor runs daemon-level health checks (workspace, auth) plus the scoped agent's own checks.
func (c *Client) Doctor(ctx context.Context, opts ...ScopedOption) (DoctorReport, error) {
	ag, err := c.resolve(opts)
	if err != nil {
		return DoctorReport{}, err
	}
	return c.core.sup.Doctor(ctx, ag), nil
}

// Setup starts the scoped agent's toolchain install in the background and returns an immediate status
// snapshot. Poll SetupStatus for progress + completion. Idempotent: a concurrent Setup re-attaches to
// the in-flight job rather than starting a second install.
func (c *Client) Setup(opts ...ScopedOption) (SetupStatus, error) {
	return c.runSetup(false, opts)
}

// Update is Setup with force=true: it reinstalls/upgrades even when the toolchain is already present.
func (c *Client) Update(opts ...ScopedOption) (SetupStatus, error) {
	return c.runSetup(true, opts)
}

func (c *Client) runSetup(force bool, opts []ScopedOption) (SetupStatus, error) {
	ag, err := c.resolve(opts)
	if err != nil {
		return SetupStatus{}, err
	}
	steps, perr := setup.Plan(ag.Adapter.InstallSteps())
	if perr != nil {
		return SetupStatus{}, &APIError{Message: perr.Error(), Status: http.StatusInternalServerError, Op: "Setup", Cause: perr}
	}
	return c.core.tracker.Start(ag.ID(), steps, force, maxSetup), nil
}

// SetupStatus reports the scoped agent's current install job (running/ok/steps). A never-started
// agent reports the zero status.
func (c *Client) SetupStatus(opts ...ScopedOption) (SetupStatus, error) {
	ag, err := c.resolve(opts)
	if err != nil {
		return SetupStatus{}, err
	}
	return c.core.tracker.Status(ag.ID()), nil
}

// GetConfig returns the scoped agent's declared, non-secret settings (for prefilling a form). Secret
// and unrecognized keys are filtered out, exactly as the HTTP /config route does.
func (c *Client) GetConfig(opts ...ScopedOption) (map[string]string, error) {
	ag, err := c.resolve(opts)
	if err != nil {
		return nil, err
	}
	allow := agent.SettingsKeys(ag.Adapter.Settings())
	out := map[string]string{}
	for k, v := range ag.Creds.All() {
		if allow[k] {
			out[k] = v
		}
	}
	return out, nil
}

// SetConfig merges values into the scoped agent's namespaced config, ignoring any key the agent
// doesn't declare (a persistence failure surfaces as APIError{500}). Keys may be the agent's raw
// setting keys OR canonical keys (e.g. "reasoningEffort") that resolve to a declared non-secret
// field — the latter persist under the resolved raw key, mirroring the HTTP /config route.
func (c *Client) SetConfig(values map[string]string, opts ...ScopedOption) error {
	ag, err := c.resolve(opts)
	if err != nil {
		return err
	}
	schema := ag.Adapter.Settings()
	for k, v := range values {
		raw, ok := agent.ResolveSettingKey(schema, k)
		if !ok {
			continue
		}
		if err := ag.Creds.Set(raw, v); err != nil {
			return &APIError{Message: "failed to persist settings", Status: http.StatusInternalServerError, Op: "SetConfig", Cause: err}
		}
	}
	return nil
}

// ---- chats, messages, runs -------------------------------------------------

// Chats lists recorded chats (newest first). For each chat whose adapter owns a native title (a
// Titler, e.g. Claude Code's transcript ai-title), that title overrides the derived first-message
// snippet — the same enrichment the HTTP /chats route applies. A user rename (SetTitle via RenameChat)
// always wins over the native title.
func (c *Client) Chats() []ChatSummary {
	summaries := c.core.store.Chats()
	for i := range summaries {
		c.enrichNativeTitle(&summaries[i])
	}
	return summaries
}

// enrichNativeTitle overlays the agent's own auto-generated title (a Titler) onto a summary UNLESS the
// user has set an explicit title (a rename), which always wins. Precedence: user title > native
// auto-title > derived first-message snippet. Best-effort — a missing adapter/session/cwd/title leaves
// the summary's existing title intact. Mirrors api.go's enrichNativeTitle exactly.
func (c *Client) enrichNativeTitle(s *ChatSummary) {
	if c.core.store.Title(s.ChatID) != "" {
		return // a user rename overrides the agent's native auto-title
	}
	ag, ok := c.core.sup.Resolve(s.Agent)
	if !ok {
		return
	}
	titler, ok := ag.Adapter.(agent.Titler)
	if !ok {
		return
	}
	sid := c.core.store.Session(ag.ID(), s.ChatID)
	if sid == "" {
		return
	}
	cwd := c.core.store.ChatCWD(s.ChatID)
	if cwd == "" {
		cwd = c.core.sup.CWD()
	}
	if t := titler.Title(agent.HistoryQuery{ChatID: s.ChatID, SessionID: sid, CWD: cwd}); t != "" {
		s.Title = t
	}
}

// DeleteResult reports a chat delete (mirrors the HTTP DELETE /chats/{id} response): the bookkeeping was
// purged, plus which agents' native transcripts were removed vs. failed to remove (best-effort, per
// session the chat mapped to).
type DeleteResult struct {
	Deleted      bool     `json:"deleted"`
	Sessions     int      `json:"sessions"`
	NativePurged []string `json:"nativePurged,omitempty"`
	NativeFailed []string `json:"nativeFailed,omitempty"`
}

// RenameChat sets a user-chosen title for a chat (mirrors PUT /chats/{id}). The user title wins over the
// agent's native auto-title in every listing; an empty title clears the rename. Returns the updated
// summary. A persistence failure surfaces as APIError{500}.
func (c *Client) RenameChat(chatID, title string) (ChatSummary, error) {
	if err := c.core.store.SetTitle(chatID, strings.TrimSpace(title)); err != nil {
		return ChatSummary{}, &APIError{Message: "failed to persist title", Status: http.StatusInternalServerError, Op: "RenameChat", Cause: err}
	}
	summary := c.core.store.ChatSummaryFor(chatID)
	c.enrichNativeTitle(&summary)
	return summary, nil
}

// DeleteChat is a true, irreversible delete (mirrors DELETE /chats/{id}): it purges ALL of the chat's
// mindwire bookkeeping and, for every session the chat mapped to, removes that agent's native transcript
// (the source of truth). Returns APIError{409} if a turn is live. Native deletion is best-effort per
// agent — an adapter without HistoryDeleter (or a failing remove) still leaves the bookkeeping purged;
// the result reports what happened.
func (c *Client) DeleteChat(chatID string) (DeleteResult, error) {
	if c.core.sup.Busy(chatID) {
		return DeleteResult{}, &APIError{Message: "a turn is running for this chat", Status: http.StatusConflict, Op: "DeleteChat"}
	}
	refs, err := c.core.store.DeleteChat(chatID)
	if err != nil {
		return DeleteResult{}, &APIError{Message: "failed to delete chat", Status: http.StatusInternalServerError, Op: "DeleteChat", Cause: err}
	}
	res := DeleteResult{Deleted: true, Sessions: len(refs)}
	for _, ref := range refs {
		ag, ok := c.core.sup.Resolve(ref.Agent)
		if !ok {
			continue
		}
		del, ok := ag.Adapter.(agent.HistoryDeleter)
		if !ok {
			continue // agent keeps no deletable native transcript — bookkeeping purge is enough
		}
		cwd := ref.CWD
		if cwd == "" {
			cwd = c.core.sup.CWD()
		}
		removed, derr := del.DeleteHistory(agent.HistoryQuery{ChatID: chatID, SessionID: ref.SID, CWD: cwd})
		switch {
		case derr != nil:
			res.NativeFailed = append(res.NativeFailed, ref.Agent)
		case removed:
			res.NativePurged = append(res.NativePurged, ref.Agent)
		}
		// A genuinely-absent transcript (false, nil) goes to neither list — idempotent success
		// without a false "purged" claim.
	}
	return res, nil
}

// ForkChat clones a chat into a new id (mirrors POST /chats/{id}/fork). newChatID may be empty to
// generate one. The fork shares the source's native session until its first turn, which branches it
// (natively on Claude via --fork-session; a fresh session on agents without native fork). Returns
// APIError{409} if the source has a live turn, APIError{404} if the source is unknown, APIError{400} if
// the target id is already in use. Returns the new chat's summary.
func (c *Client) ForkChat(srcChatID, newChatID string) (ChatSummary, error) {
	if c.core.sup.Busy(srcChatID) {
		return ChatSummary{}, &APIError{Message: "a turn is running for this chat", Status: http.StatusConflict, Op: "ForkChat"}
	}
	newID := strings.TrimSpace(newChatID)
	if newID == "" {
		newID = newChatIDHex()
	}
	if err := c.core.store.ForkChat(srcChatID, newID); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		return ChatSummary{}, &APIError{Message: err.Error(), Status: status, Op: "ForkChat", Cause: err}
	}
	summary := c.core.store.ChatSummaryFor(newID)
	c.enrichNativeTitle(&summary)
	return summary, nil
}

// MessagesOptions parameterizes Messages: the target agent (empty = the client default) and a paging
// window (Limit = tail size, Before = return only messages strictly before this id). The zero value
// returns the whole transcript for the default agent.
type MessagesOptions struct {
	Agent  string
	Limit  int
	Before string
}

// Messages returns a chat's transcript. For an agent with native history it reads the agent's own
// transcript (falling back to the recorded log if that read is empty or errors); otherwise it returns
// the daemon's recorded log. Either way the result is windowed by Limit/Before. The recorded fallback
// is converted from the store's Message to the unified agent.Message shape.
func (c *Client) Messages(chatID string, opts MessagesOptions) ([]Message, error) {
	ag, ok := c.core.sup.Resolve(orDefault(opts.Agent, c.defaultAgent))
	if !ok {
		return nil, &APIError{Message: "unknown agent", Status: http.StatusBadRequest, Op: "Messages"}
	}
	if ag.Adapter.Capabilities().History == agent.SupportNative {
		cwd := c.core.store.ChatCWD(chatID)
		if cwd == "" {
			cwd = c.core.sup.CWD()
		}
		msgs, err := ag.Adapter.History(agent.HistoryQuery{
			ChatID: chatID, SessionID: c.core.store.Session(ag.ID(), chatID), CWD: cwd,
		})
		if err == nil && len(msgs) > 0 {
			return pageWindow(msgs, opts.Limit, opts.Before, func(m Message) string { return m.ID }), nil
		}
	}
	rec := c.core.store.Messages(chatID)
	out := make([]Message, len(rec))
	for i, m := range rec {
		out[i] = Message{ID: m.ID, ChatID: m.ChatID, Role: m.Role, Text: m.Text, CreatedAt: m.CreatedAt, Parts: m.Parts}
	}
	return pageWindow(out, opts.Limit, opts.Before, func(m Message) string { return m.ID }), nil
}

// LatestRun returns a handle to the chat's most recent run, or (nil, nil) if the chat has no runs yet.
func (c *Client) LatestRun(chatID string) (*Run, error) {
	rec, ok := c.core.store.LatestRun(chatID)
	if !ok {
		return nil, nil
	}
	return &Run{core: c.core, data: rec}, nil
}

// TurnRequest is one turn's parameters. Agent selects the target (empty = the client default); CWD
// overrides the working directory for this turn only; Options carries per-turn settings, prompts, and
// structured passthroughs.
type TurnRequest struct {
	ChatID  string
	Message string
	CWD     string
	Options TurnOptions
	Agent   string
}

// Turn starts a turn and returns a handle to the running Run. It enforces the same gates as POST
// /turns: an unsupported per-turn option → APIError{400}; a chat that already has a turn in flight →
// APIError{409}. The turn executes on the supervisor's own detached 30-minute context and is NOT bound
// to ctx — ctx is accepted for call symmetry and future use; stop a turn with Run.Cancel, not ctx.
func (c *Client) Turn(ctx context.Context, req TurnRequest) (*Run, error) {
	_ = ctx // the turn runs detached; see the doc comment above.
	ag, ok := c.core.sup.Resolve(orDefault(req.Agent, c.defaultAgent))
	if !ok {
		return nil, &APIError{Message: "unknown agent", Status: http.StatusBadRequest, Op: "Turn"}
	}
	if req.ChatID == "" || req.Message == "" {
		return nil, &APIError{Message: "chatId and message are required", Status: http.StatusBadRequest, Op: "Turn"}
	}
	if msg, ok := agent.UnsupportedTurnOption(ag.Adapter.Capabilities(), req.Options); !ok {
		return nil, &APIError{Message: msg, Status: http.StatusBadRequest, Op: "Turn"}
	}
	run, ok := c.core.sup.StartTurn(ag, orchestrator.StartTurnInput{
		ChatID: req.ChatID, Message: req.Message, CWD: req.CWD, Options: req.Options,
	})
	if !ok {
		return nil, &APIError{Message: "a turn is already running for this chat", Status: http.StatusConflict, Op: "Turn"}
	}
	c.core.track(run.ID)
	return &Run{core: c.core, data: run}, nil
}

// CompactRequest is the input to Compact: the chat to compact, an optional focus for the continuation
// summary (Instructions — Claude's `/compact <instructions>`; agents that don't honor focus still
// compact), and the target agent (empty = the client default).
type CompactRequest struct {
	ChatID       string `json:"chatId"`
	Instructions string `json:"instructions,omitempty"`
	Agent        string `json:"agent,omitempty"`
}

// Compact runs an on-demand conversation compaction as a first-class Run, mirroring POST
// /chats/{id}/compact. The agent folds prior context into a summary it carries forward; the Run streams
// a compaction event and records the boundary in history exactly like an auto-compaction. Gates match
// the HTTP route: the agent must implement compaction (Capabilities.CompactNow) or APIError{400}; the
// chat must already have a session to compact or APIError{400}; a live turn → APIError{409}. Like Turn,
// the compaction runs on the supervisor's detached context (not bound to ctx — stop it with Run.Cancel).
func (c *Client) Compact(ctx context.Context, req CompactRequest) (*Run, error) {
	_ = ctx // runs detached; see Turn's doc comment.
	ag, ok := c.core.sup.Resolve(orDefault(req.Agent, c.defaultAgent))
	if !ok {
		return nil, &APIError{Message: "unknown agent", Status: http.StatusBadRequest, Op: "Compact"}
	}
	if _, ok := ag.Adapter.(agent.CompactModule); !ok {
		return nil, &APIError{Message: "agent does not support on-demand compaction", Status: http.StatusBadRequest, Op: "Compact"}
	}
	if req.ChatID == "" {
		return nil, &APIError{Message: "chatId is required", Status: http.StatusBadRequest, Op: "Compact"}
	}
	if c.core.store.Session(ag.ID(), req.ChatID) == "" {
		return nil, &APIError{Message: "no conversation to compact yet (run a turn first)", Status: http.StatusBadRequest, Op: "Compact"}
	}
	run, ok := c.core.sup.StartCompact(ag, orchestrator.StartTurnInput{
		ChatID: req.ChatID, Message: strings.TrimSpace(req.Instructions), CWD: c.core.store.ChatCWD(req.ChatID),
	})
	if !ok {
		return nil, &APIError{Message: "a turn is already running for this chat", Status: http.StatusConflict, Op: "Compact"}
	}
	c.core.track(run.ID)
	return &Run{core: c.core, data: run}, nil
}

// ResolveRequest is the input to Resolve — the same shape as TurnRequest plus the resolve bounds.
// MaxIterations caps the auto-continued iterations before the run ends with StopReason "capped";
// Deadline is the overall wall-clock budget for the whole resolve. Both are optional: a zero value
// falls back to the daemon defaults.
type ResolveRequest struct {
	ChatID        string
	Message       string
	CWD           string
	Options       TurnOptions
	Agent         string
	MaxIterations int
	Deadline      time.Duration
}

// Resolve starts a GLOBAL-RESOLVE run and returns a handle to the PARENT Run, mirroring POST /turns
// with {mode:"resolve"}. Unlike Turn, the daemon holds the run open and auto-continues the agent's own
// multi-step work until the task is globally complete (the agent emits the completion sentinel), an
// unrecoverable error occurs, or a cap is hit; each auto-continued iteration is a child run whose events
// stream onto the parent's topic (one merged stream — Stream/Wait on the parent see the whole resolve
// and Wait returns the final aggregate result). Children returns the per-iteration runs. Gates match
// Turn: unknown agent / missing fields / an unsupported per-turn option → APIError{400}; a chat with a
// turn already in flight → APIError{409}. Like Turn it runs on the supervisor's detached context (bound
// by the resolve deadline, not ctx — stop it with Run.Cancel).
func (c *Client) Resolve(ctx context.Context, req ResolveRequest) (*Run, error) {
	_ = ctx // runs detached; see Turn's doc comment.
	ag, ok := c.core.sup.Resolve(orDefault(req.Agent, c.defaultAgent))
	if !ok {
		return nil, &APIError{Message: "unknown agent", Status: http.StatusBadRequest, Op: "Resolve"}
	}
	if req.ChatID == "" || req.Message == "" {
		return nil, &APIError{Message: "chatId and message are required", Status: http.StatusBadRequest, Op: "Resolve"}
	}
	if msg, ok := agent.UnsupportedTurnOption(ag.Adapter.Capabilities(), req.Options); !ok {
		return nil, &APIError{Message: msg, Status: http.StatusBadRequest, Op: "Resolve"}
	}
	run, ok := c.core.sup.StartResolve(ag, orchestrator.StartTurnInput{
		ChatID: req.ChatID, Message: req.Message, CWD: req.CWD, Options: req.Options,
	}, orchestrator.ResolveOptions{MaxIterations: req.MaxIterations, Deadline: req.Deadline})
	if !ok {
		return nil, &APIError{Message: "a turn is already running for this chat", Status: http.StatusConflict, Op: "Resolve"}
	}
	c.core.track(run.ID)
	return &Run{core: c.core, data: run}, nil
}

// Run returns a handle to an existing run by id, or APIError{404} if there is no such run.
func (c *Client) Run(id string) (*Run, error) {
	rec, ok := c.core.store.GetRun(id)
	if !ok {
		return nil, &APIError{Message: "not found", Status: http.StatusNotFound, Op: "Run"}
	}
	return &Run{core: c.core, data: rec}, nil
}

// Children returns handles to the child runs of a resolve parent, oldest→newest — the per-iteration
// turns of a global-resolve run (mirrors GET /runs/{id}/children). APIError{404} if the id is unknown;
// an ordinary turn (or a parent with no iterations yet) returns an empty slice, never an error.
func (c *Client) Children(parentID string) ([]*Run, error) {
	if _, ok := c.core.store.GetRun(parentID); !ok {
		return nil, &APIError{Message: "not found", Status: http.StatusNotFound, Op: "Children"}
	}
	recs := c.core.store.Children(parentID)
	out := make([]*Run, len(recs))
	for i, rec := range recs {
		out[i] = &Run{core: c.core, data: rec}
	}
	return out, nil
}

// ---- notifications ---------------------------------------------------------

// NotifyConfigStatus reports whether a notification webhook is wired (the token is never returned).
type NotifyConfigStatus struct {
	Configured bool   `json:"configured"`
	URL        string `json:"url"`
	Channel    string `json:"channel"`
}

// NotifyConfigInput provisions the notification webhook the daemon POSTs to.
type NotifyConfigInput struct {
	URL     string `json:"url"`
	Channel string `json:"channel"`
	Token   string `json:"token,omitempty"`
}

// GetNotifyConfig reports the provisioned notification webhook (without the token).
func (c *Client) GetNotifyConfig() NotifyConfigStatus {
	url, channel, _ := c.core.store.NotifyConfig()
	return NotifyConfigStatus{Configured: url != "" && channel != "", URL: url, Channel: channel}
}

// SetNotifyConfig provisions the notification webhook (url and channel required, or APIError{400}).
func (c *Client) SetNotifyConfig(in NotifyConfigInput) error {
	if in.URL == "" || in.Channel == "" {
		return &APIError{Message: "url and channel are required", Status: http.StatusBadRequest, Op: "SetNotifyConfig"}
	}
	if err := c.core.store.SetNotifyConfig(in.URL, in.Channel, in.Token); err != nil {
		return &APIError{Message: "failed to persist notification config", Status: http.StatusInternalServerError, Op: "SetNotifyConfig", Cause: err}
	}
	return nil
}

// Notifications streams the daemon's notifications — the same payloads pushed to the webhook — as an
// iter.Seq: replay buffer first, then live events until ctx is cancelled or the stream closes. The
// underlying subscription is always released when the range ends (break, return, or ctx-cancel).
func (c *Client) Notifications(ctx context.Context) iter.Seq[Notification] {
	return func(yield func(Notification) bool) {
		ch, replay, cancel := c.core.sup.Notes().Subscribe()
		defer cancel()
		for _, n := range replay {
			if !yield(n) {
				return
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case n, open := <-ch:
				if !open {
					return
				}
				if !yield(n) {
					return
				}
			}
		}
	}
}

// Processes streams live per-turn CPU/memory as an iter.Seq of ProcessFrames — the same on-demand,
// refcounted sampler the HTTP /processes/stream route serves, here in-process. Subscribing STARTS the
// sampler if it's the first watcher; ending the range (break, return, or ctx-cancel) releases the
// subscription and STOPS the sampler when it's the last — so no sampling runs unless someone iterates.
// When agent is non-empty, each frame's Samples are filtered to that agent. Frames are live snapshots
// (no replay); an empty Samples slice is a valid keep-alive frame. ok=false (sampler at capacity)
// yields nothing.
func (c *Client) Processes(ctx context.Context, agent string) iter.Seq[ProcessFrame] {
	return func(yield func(ProcessFrame) bool) {
		ch, cancel, ok := c.core.sup.ProcMon().Subscribe()
		if !ok {
			return
		}
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case f, open := <-ch:
				if !open {
					return
				}
				if agent != "" {
					kept := make([]ProcessSample, 0, len(f.Samples))
					for _, s := range f.Samples {
						if s.Agent == agent {
							kept = append(kept, s)
						}
					}
					f.Samples = kept
				}
				if !yield(f) {
					return
				}
			}
		}
	}
}

// ---- notification channels + rules (daemon-driven fan-out) -----------------
//
// Channels are named delivery targets (webhook / slack / discord / telegram); rules route matching
// notifications to them (global / per-agent / per-session, with optional event selection). Unlike
// the HTTP surface — which masks secrets on read — the in-process SDK returns the stored values
// verbatim (the caller already holds full trust over the state file). The Router picks up any change
// LIVE on the next notification, so no restart is needed.

// NotifyChannels returns every configured channel.
func (c *Client) NotifyChannels() []NotifyChannel { return c.core.store.NotifyChannels() }

// NotifyChannelByID returns one channel by id (APIError{404} if unknown).
func (c *Client) NotifyChannelByID(id string) (NotifyChannel, error) {
	ch, ok := c.core.store.NotifyChannel(id)
	if !ok {
		return NotifyChannel{}, &APIError{Message: "channel not found", Status: http.StatusNotFound, Op: "NotifyChannelByID"}
	}
	return ch, nil
}

// SetNotifyChannel upserts a channel, minting an id when the input omits one. Returns the stored
// channel. APIError{400} for an invalid type or a missing url.
func (c *Client) SetNotifyChannel(ch NotifyChannel) (NotifyChannel, error) {
	switch ch.Type {
	case "":
		ch.Type = ChannelWebhook
	case ChannelWebhook, ChannelSlack, ChannelDiscord, ChannelTelegram:
	default:
		return NotifyChannel{}, &APIError{Message: "invalid channel type", Status: http.StatusBadRequest, Op: "SetNotifyChannel"}
	}
	if ch.URL == "" {
		return NotifyChannel{}, &APIError{Message: "url is required", Status: http.StatusBadRequest, Op: "SetNotifyChannel"}
	}
	if ch.ID == "" {
		ch.ID = newChatIDHex()
	}
	if err := c.core.store.SetNotifyChannel(ch); err != nil {
		return NotifyChannel{}, &APIError{Message: "failed to persist channel", Status: http.StatusInternalServerError, Op: "SetNotifyChannel", Cause: err}
	}
	return ch, nil
}

// DeleteNotifyChannel removes a channel by id (idempotent).
func (c *Client) DeleteNotifyChannel(id string) error {
	if err := c.core.store.DeleteNotifyChannel(id); err != nil {
		return &APIError{Message: "failed to delete channel", Status: http.StatusInternalServerError, Op: "DeleteNotifyChannel", Cause: err}
	}
	return nil
}

// TestNotifyChannel delivers a synthetic notification to a channel and returns nil on a 2xx, else an
// error describing the failure. APIError{404} if the channel id is unknown.
func (c *Client) TestNotifyChannel(ctx context.Context, id string) error {
	ch, ok := c.core.store.NotifyChannel(id)
	if !ok {
		return &APIError{Message: "channel not found", Status: http.StatusNotFound, Op: "TestNotifyChannel"}
	}
	n := Notification{
		Condition: Finished,
		Title:     "Mindwire test notification",
		Body:      "If you can read this, this channel is wired correctly.",
		Agent:     "mindwire",
	}
	return notify.DeliverOne(ctx, nil, ch, n)
}

// NotifyRules returns every routing rule.
func (c *Client) NotifyRules() []NotifyRule { return c.core.store.NotifyRules() }

// NotifyRuleByID returns one rule by id (APIError{404} if unknown).
func (c *Client) NotifyRuleByID(id string) (NotifyRule, error) {
	r, ok := c.core.store.NotifyRule(id)
	if !ok {
		return NotifyRule{}, &APIError{Message: "rule not found", Status: http.StatusNotFound, Op: "NotifyRuleByID"}
	}
	return r, nil
}

// SetNotifyRule upserts a routing rule, minting an id when the input omits one. Returns the stored
// rule. APIError{400} for an incoherent scope or an empty channel set.
func (c *Client) SetNotifyRule(r NotifyRule) (NotifyRule, error) {
	switch r.Scope {
	case ScopeGlobal:
	case ScopeAgent:
		if r.Agent == "" {
			return NotifyRule{}, &APIError{Message: "agent is required for an agent-scoped rule", Status: http.StatusBadRequest, Op: "SetNotifyRule"}
		}
	case ScopeSession:
		if r.Session == "" {
			return NotifyRule{}, &APIError{Message: "session is required for a session-scoped rule", Status: http.StatusBadRequest, Op: "SetNotifyRule"}
		}
	default:
		return NotifyRule{}, &APIError{Message: "invalid rule scope", Status: http.StatusBadRequest, Op: "SetNotifyRule"}
	}
	if len(r.ChannelIDs) == 0 {
		return NotifyRule{}, &APIError{Message: "at least one channelId is required", Status: http.StatusBadRequest, Op: "SetNotifyRule"}
	}
	if r.ID == "" {
		r.ID = newChatIDHex()
	}
	if err := c.core.store.SetNotifyRule(r); err != nil {
		return NotifyRule{}, &APIError{Message: "failed to persist rule", Status: http.StatusInternalServerError, Op: "SetNotifyRule", Cause: err}
	}
	return r, nil
}

// DeleteNotifyRule removes a rule by id (idempotent).
func (c *Client) DeleteNotifyRule(id string) error {
	if err := c.core.store.DeleteNotifyRule(id); err != nil {
		return &APIError{Message: "failed to delete rule", Status: http.StatusInternalServerError, Op: "DeleteNotifyRule", Cause: err}
	}
	return nil
}

// ---- internal helpers ------------------------------------------------------

// track records a run id so Close can cancel it. No-op after Close.
func (co *core) track(id string) {
	co.mu.Lock()
	if !co.closed {
		co.runs[id] = struct{}{}
	}
	co.mu.Unlock()
}

// orDefault returns v if non-empty, else def.
func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// newChatIDHex generates a random chat id for a fork whose caller didn't supply one (16 random bytes,
// hex — the same scheme as api.newChatID). crypto/rand never fails in practice; on the vanishingly
// unlikely error it returns "", which ForkChat's store call then rejects as an empty target id.
func newChatIDHex() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// pageWindow returns the tail window of an oldest→newest slice: everything strictly BEFORE the id
// `before` (when set), then the last `limit` of what remains (when limit > 0). Mirrors api.pageWindow.
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
