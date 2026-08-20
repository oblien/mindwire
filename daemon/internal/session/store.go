// Package session is the daemon's local state, persisted to a JSON file on the
// sandbox disk. One daemon per sandbox hosts every agent adapter, so this store is
// SHARED across agents: chat transcripts and runs are keyed by chatId (globally
// unique), while per-agent state — CLI session ids and config/creds — is namespaced
// by agent type so adapters never collide (see the orchestrator's CredView and the
// (agent, chat) session keys below).
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

type Message struct {
	ID        string       `json:"id"`
	ChatID    string       `json:"chatId"`
	Role      string       `json:"role"` // "user" | "assistant"
	Text      string       `json:"text"`
	CreatedAt string       `json:"createdAt"`
	Parts     []agent.Part `json:"parts,omitempty"` // ordered rich transcript (text/thinking/tool)
}

// Run is one durable agent turn. The daemon owns it: it keeps running (and is
// recorded here) regardless of whether the app is still connected.
//
// Resolve mode introduces a parent/child tree: a "resolve" Run (Kind=="resolve") is the parent the
// caller waits on, and each auto-continued iteration is an ordinary child Run carrying ParentID.
// Ordinary single turns leave all of ParentID/Kind/StopReason/Iterations zero, so the shape is
// unchanged for existing callers.
type Run struct {
	ID        string `json:"id"`
	ChatID    string `json:"chatId"`
	Agent     string `json:"agent,omitempty"` // agent type that executed the turn
	Status    string `json:"status"`          // "running" | "done" | "error" | "cancelled"
	Error     string `json:"error,omitempty"`
	ReplyID   string `json:"replyId,omitempty"` // assistant message id when done
	CreatedAt string `json:"createdAt"`
	EndedAt   string `json:"endedAt,omitempty"`
	// Resolve-mode tree fields (all zero for an ordinary turn):
	ParentID   string `json:"parentId,omitempty"`   // set on a child iteration → its parent resolve Run id
	Kind       string `json:"kind,omitempty"`       // "" ordinary | "resolve" parent
	StopReason string `json:"stopReason,omitempty"` // parent only: "done" | "capped" | "error" — why the loop ended
	Iterations int    `json:"iterations,omitempty"` // parent only: how many child turns ran
}

type state struct {
	Sessions map[string]string `json:"sessions"` // "<agent>\x1f<chatId>" -> CLI session id
	Cwds     map[string]string `json:"cwds"`     // chatId -> the dir turns ran in (for native history)
	Titles   map[string]string `json:"titles"`   // chatId -> user-set title (wins over the native auto-title)
	Messages []Message         `json:"messages"`
	Runs     []Run             `json:"runs"`
	Notify   notifyConfig      `json:"notify,omitempty"` // legacy single notification webhook config
	// Channels + Rules are the daemon-driven notification fan-out: named delivery targets and the
	// per-agent/per-session/global rules that route matching notifications to them. Additive — the
	// legacy Notify webhook (above) and the file/exec/stream channels keep firing independently.
	Channels []agent.NotifyChannel `json:"channels,omitempty"`
	Rules    []agent.NotifyRule    `json:"rules,omitempty"`
	Config   map[string]string     `json:"config"` // "<agent>:<key>" -> value (namespaced settings/creds)
	// ForkPending marks a freshly-forked (agent, chat) whose FIRST turn must branch the native session
	// (ForkOnResume) so it can't pollute the source transcript. Keyed "<agent>\x1f<chatId>"; the runner
	// consumes and clears the marker on the first turn (see TakeForkPending).
	ForkPending map[string]bool `json:"forkPending,omitempty"`
}

// SessionRef identifies one agent's native session for a chat. DeleteChat returns these so the API
// layer can drive native-transcript deletion per agent (the store never learns transcript paths).
type SessionRef struct {
	Agent string
	SID   string
	CWD   string
}

// notifyConfig holds the notification webhook the client provisioned
// (the client PUTs the webhook to the daemon; the daemon POSTs notifications to it).
type notifyConfig struct {
	URL     string `json:"url,omitempty"`
	Channel string `json:"channel,omitempty"`
	Token   string `json:"token,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
	s    state
}

func Open(path string) (*Store, error) {
	st := &Store{path: path, s: newState()}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if uerr := json.Unmarshal(b, &st.s); uerr != nil {
			return nil, uerr
		}
		// Backfill maps that an older/partial state file may omit.
		if st.s.Sessions == nil {
			st.s.Sessions = map[string]string{}
		}
		if st.s.Cwds == nil {
			st.s.Cwds = map[string]string{}
		}
		if st.s.Titles == nil {
			st.s.Titles = map[string]string{}
		}
		if st.s.ForkPending == nil {
			st.s.ForkPending = map[string]bool{}
		}
		if st.s.Config == nil {
			st.s.Config = map[string]string{}
		}
	case os.IsNotExist(err):
		// fresh
	default:
		return nil, err
	}
	return st, nil
}

func newState() state {
	return state{
		Sessions:    map[string]string{},
		Cwds:        map[string]string{},
		Titles:      map[string]string{},
		ForkPending: map[string]bool{},
		Config:      map[string]string{},
	}
}

// sessionKey namespaces a CLI session id by agent type so two adapters addressing the
// same chatId never resume or read each other's session/transcript.
func sessionKey(agentType, chatID string) string { return agentType + "\x1f" + chatID }

func (st *Store) save() error {
	b, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// Session returns the CLI session id for (agent, chat), or "" if none. Keyed per agent so
// a chatId addressed by two adapters keeps independent resume state.
func (st *Store) Session(agentType, chatID string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Sessions[sessionKey(agentType, chatID)]
}

func (st *Store) SetSession(agentType, chatID, sessionID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Sessions[sessionKey(agentType, chatID)] = sessionID
	return st.save()
}

// ChatCWD returns the directory turns for this chat ran in (used to locate an agent's
// native transcript, which is path-scoped). "" if no turn has run yet.
func (st *Store) ChatCWD(chatID string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Cwds[chatID]
}

func (st *Store) SetChatCWD(chatID, cwd string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.s.Cwds[chatID] == cwd {
		return nil // unchanged — avoid a needless rewrite
	}
	st.s.Cwds[chatID] = cwd
	return st.save()
}

// Title returns the user-set title for a chat, or "" if none. This is the rename title, which the
// listing prefers over the agent's native auto-title (see Chats and the API's chats handler).
func (st *Store) Title(chatID string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Titles[chatID]
}

// SetTitle records a user-chosen title for a chat (a rename). An empty title clears it, falling back
// to the native auto-title / first-message snippet.
func (st *Store) SetTitle(chatID, title string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if title == "" {
		delete(st.s.Titles, chatID)
	} else {
		st.s.Titles[chatID] = title
	}
	return st.save()
}

// ChatExists reports whether the store has ANY record of a chat — a session, cwd, title, message, or
// run. Used to 404 a fork of an unknown source and to reject a fork onto an id already in use.
func (st *Store) ChatExists(chatID string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.chatExistsLocked(chatID)
}

func (st *Store) chatExistsLocked(chatID string) bool {
	if _, ok := st.s.Cwds[chatID]; ok {
		return true
	}
	if _, ok := st.s.Titles[chatID]; ok {
		return true
	}
	suffix := "\x1f" + chatID
	for k := range st.s.Sessions {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	for i := range st.s.Messages {
		if st.s.Messages[i].ChatID == chatID {
			return true
		}
	}
	for i := range st.s.Runs {
		if st.s.Runs[i].ChatID == chatID {
			return true
		}
	}
	return false
}

// DeleteChat removes ALL of a chat's mindwire bookkeeping — its per-agent session pointers, cwd,
// title, fork markers, messages, and runs — and returns a SessionRef for every (agent, session) the
// chat mapped to so the caller can delete each agent's native transcript (the source of truth). The
// store deliberately does not touch native files: it doesn't know their paths. Idempotent — deleting a
// chat with no records succeeds with an empty ref list.
func (st *Store) DeleteChat(chatID string) ([]SessionRef, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	cwd := st.s.Cwds[chatID]
	suffix := "\x1f" + chatID
	var refs []SessionRef
	for k, sid := range st.s.Sessions {
		if strings.HasSuffix(k, suffix) {
			refs = append(refs, SessionRef{Agent: strings.TrimSuffix(k, suffix), SID: sid, CWD: cwd})
			delete(st.s.Sessions, k)
		}
	}
	for k := range st.s.ForkPending {
		if strings.HasSuffix(k, suffix) {
			delete(st.s.ForkPending, k)
		}
	}
	delete(st.s.Cwds, chatID)
	delete(st.s.Titles, chatID)

	msgs := make([]Message, 0, len(st.s.Messages))
	for _, m := range st.s.Messages {
		if m.ChatID != chatID {
			msgs = append(msgs, m)
		}
	}
	st.s.Messages = msgs

	runs := make([]Run, 0, len(st.s.Runs))
	for _, r := range st.s.Runs {
		if r.ChatID != chatID {
			runs = append(runs, r)
		}
	}
	st.s.Runs = runs

	return refs, st.save()
}

// ForkChat clones a chat's session mapping into a new chat id so the fork transparently shows the
// shared native history until its first turn branches it. It seeds Sessions[agent, newChatID] from the
// source for every agent that has one, copies the cwd and title, and records a fork-pending marker per
// agent (consumed by the runner to force ForkOnResume on turn one). Messages are NOT copied — GET
// messages is native-first, so the fork reads the source transcript until it branches. Errors if the
// source is unknown or the target id is already in use.
func (st *Store) ForkChat(srcChatID, newChatID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if newChatID == "" {
		return errors.New("new chat id is required")
	}
	if srcChatID == newChatID {
		return errors.New("fork target must differ from the source chat")
	}
	if !st.chatExistsLocked(srcChatID) {
		return fmt.Errorf("chat %q not found", srcChatID)
	}
	if st.chatExistsLocked(newChatID) {
		return fmt.Errorf("chat %q already exists", newChatID)
	}

	srcSuffix := "\x1f" + srcChatID
	for k, sid := range st.s.Sessions {
		if strings.HasSuffix(k, srcSuffix) {
			agentType := strings.TrimSuffix(k, srcSuffix)
			newKey := sessionKey(agentType, newChatID)
			st.s.Sessions[newKey] = sid
			st.s.ForkPending[newKey] = true
		}
	}
	if cwd, ok := st.s.Cwds[srcChatID]; ok {
		st.s.Cwds[newChatID] = cwd
	}
	if t, ok := st.s.Titles[srcChatID]; ok {
		st.s.Titles[newChatID] = t
	}
	return st.save()
}

// TakeForkPending reports whether a fork-on-first-turn marker is set for (agent, chat) and atomically
// clears it. The runner calls this once at the start of a turn so a forked chat's first turn branches
// the native session (ForkOnResume) regardless of client flags, then never again.
func (st *Store) TakeForkPending(agentType, chatID string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	key := sessionKey(agentType, chatID)
	if st.s.ForkPending[key] {
		delete(st.s.ForkPending, key)
		_ = st.save()
		return true
	}
	return false
}

func (st *Store) Messages(chatID string) []Message {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := []Message{}
	for _, m := range st.s.Messages {
		if m.ChatID == chatID {
			out = append(out, m)
		}
	}
	return out
}

func (st *Store) AddMessage(m Message) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Messages = append(st.s.Messages, m)
	return st.save()
}

func (st *Store) GetRun(id string) (Run, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, r := range st.s.Runs {
		if r.ID == id {
			return r, true
		}
	}
	return Run{}, false
}

func (st *Store) SaveRun(run Run) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, r := range st.s.Runs {
		if r.ID == run.ID {
			st.s.Runs[i] = run
			return st.save()
		}
	}
	st.s.Runs = append(st.s.Runs, run)
	return st.save()
}

// LatestRun returns the most recent TOP-LEVEL run for a chat (runs are append-ordered, so the last
// match wins). The app uses this to reattach a chat's in-flight turn stream after reopening. Resolve
// child iterations (ParentID != "") are skipped: their events stream on the PARENT topic, so a
// reattach must target the parent — which, as the last top-level run, is exactly what this returns.
func (st *Store) LatestRun(chatID string) (Run, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := len(st.s.Runs) - 1; i >= 0; i-- {
		if st.s.Runs[i].ChatID == chatID && st.s.Runs[i].ParentID == "" {
			return st.s.Runs[i], true
		}
	}
	return Run{}, false
}

// Children returns every child iteration of a resolve parent run, oldest→newest (append order). It is
// the run tree the caller reads to inspect per-iteration outcomes; an ordinary run or an unknown id
// yields an empty slice.
func (st *Store) Children(parentID string) []Run {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Run
	for _, r := range st.s.Runs {
		if r.ParentID == parentID {
			out = append(out, r)
		}
	}
	return out
}

// ReconcileRunning marks any run still "running" as errored — called once at startup. A daemon
// restart abandons in-flight turns (they ran on the process's context), so without this a stale
// run stays "running" forever and a client reattaching would hang on a topic nothing publishes to.
func (st *Store) ReconcileRunning(reason string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	changed := false
	for i := range st.s.Runs {
		if st.s.Runs[i].Status == "running" {
			st.s.Runs[i].Status = "error"
			st.s.Runs[i].Error = reason
			st.s.Runs[i].EndedAt = time.Now().UTC().Format(time.RFC3339)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return st.save()
}

// Config returns a copy of the whole namespaced config map. Callers scope to one agent
// through the orchestrator's CredView (key prefix "<agent>:"); the runner and API never
// read this raw map directly.
func (st *Store) Config() map[string]string {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make(map[string]string, len(st.s.Config))
	for k, v := range st.s.Config {
		out[k] = v
	}
	return out
}

// ---- namespaced key/value backing the orchestrator's per-agent CredView ----------
// Keys are already agent-prefixed by the CredView ("<agent>:<key>"); the store is a
// dumb map underneath.

func (st *Store) Get(key string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Config[key]
}

func (st *Store) Set(key, val string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Config[key] = val
	return st.save()
}

// NotifyConfig returns the provisioned notification webhook (url, channel, token).
func (st *Store) NotifyConfig() (url, channel, token string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Notify.URL, st.s.Notify.Channel, st.s.Notify.Token
}

// ChatSummary is the daemon's view of a chat, for listing sessions.
type ChatSummary struct {
	ChatID     string `json:"chatId"`
	Agent      string `json:"agent,omitempty"` // agent type that last ran this chat
	Title      string `json:"title"`
	Messages   int    `json:"messages"`
	UpdatedAt  string `json:"updatedAt"`
	LastStatus string `json:"lastStatus,omitempty"`
	LastRunID  string `json:"lastRunId,omitempty"` // latest run id — lets a list badge/deep-attach a live turn
}

// Chats lists the chats the daemon has recorded (newest activity first), with a
// title derived from the first user message.
func (st *Store) Chats() []ChatSummary {
	st.mu.Lock()
	defer st.mu.Unlock()

	type agg struct {
		title    string
		titleSet bool
		count    int
		updated  string
	}
	m := map[string]*agg{}
	order := []string{}
	for _, msg := range st.s.Messages {
		a := m[msg.ChatID]
		if a == nil {
			a = &agg{}
			m[msg.ChatID] = a
			order = append(order, msg.ChatID)
		}
		a.count++
		if !a.titleSet && msg.Role == "user" {
			a.title = chatTitle(msg.Text)
			a.titleSet = true
		}
		if msg.CreatedAt > a.updated {
			a.updated = msg.CreatedAt
		}
	}
	status := map[string]string{}
	lastAgent := map[string]string{}
	lastRun := map[string]string{}
	for _, r := range st.s.Runs {
		if r.ParentID != "" {
			continue // a resolve child iteration; the parent is the chat's representative run
		}
		status[r.ChatID] = r.Status // runs are appended in order, so last wins
		lastRun[r.ChatID] = r.ID
		if r.Agent != "" {
			lastAgent[r.ChatID] = r.Agent
		}
	}
	out := make([]ChatSummary, 0, len(order))
	for _, id := range order {
		a := m[id]
		t := a.title
		if t == "" {
			t = "New chat"
		}
		// A user-set title (a rename) wins over the derived first-message snippet. The API layer applies
		// the same precedence against the native auto-title: user title > native title > derived snippet.
		if ut := st.s.Titles[id]; ut != "" {
			t = ut
		}
		out = append(out, ChatSummary{
			ChatID: id, Agent: lastAgent[id], Title: t,
			Messages: a.count, UpdatedAt: a.updated, LastStatus: status[id], LastRunID: lastRun[id],
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// ChatSummaryFor builds the listing summary for a single chat id, mirroring the per-chat aggregation
// Chats does. It is best-effort and works for a message-less chat (e.g. a fresh fork, whose native
// history is shared until its first turn branches it): such a chat reports zero messages and the user
// title (or "New chat"). The rename/fork API handlers use this to return the updated/new summary.
func (st *Store) ChatSummaryFor(chatID string) ChatSummary {
	st.mu.Lock()
	defer st.mu.Unlock()

	sum := ChatSummary{ChatID: chatID}
	titleSet := false
	for _, msg := range st.s.Messages {
		if msg.ChatID != chatID {
			continue
		}
		sum.Messages++
		if !titleSet && msg.Role == "user" {
			sum.Title = chatTitle(msg.Text)
			titleSet = true
		}
		if msg.CreatedAt > sum.UpdatedAt {
			sum.UpdatedAt = msg.CreatedAt
		}
	}
	for _, r := range st.s.Runs {
		if r.ChatID != chatID || r.ParentID != "" {
			continue // skip resolve child iterations; the parent is the representative run
		}
		sum.LastStatus = r.Status // runs are appended in order, so last wins
		sum.LastRunID = r.ID
		if r.Agent != "" {
			sum.Agent = r.Agent
		}
	}
	if sum.Title == "" {
		sum.Title = "New chat"
	}
	if ut := st.s.Titles[chatID]; ut != "" {
		sum.Title = ut
	}
	return sum
}

func chatTitle(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}

// SetNotifyConfig stores the notification webhook the client provisioned.
func (st *Store) SetNotifyConfig(url, channel, token string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Notify = notifyConfig{URL: url, Channel: channel, Token: token}
	return st.save()
}

// ---- daemon-driven notification channels + rules ------------------------------------------------
// These back the /notify/channels and /notify/rules REST surface (and the notify.Router fan-out).
// Every getter deep-copies (including a channel's Headers map) so a caller can never mutate stored
// state through the returned value; every setter locks, upserts by id, and persists via save().

func cloneChannel(c agent.NotifyChannel) agent.NotifyChannel {
	if c.Headers != nil {
		h := make(map[string]string, len(c.Headers))
		for k, v := range c.Headers {
			h[k] = v
		}
		c.Headers = h
	}
	return c
}

func cloneRule(r agent.NotifyRule) agent.NotifyRule {
	if r.Conditions != nil {
		r.Conditions = append([]agent.Condition(nil), r.Conditions...)
	}
	if r.ChannelIDs != nil {
		r.ChannelIDs = append([]string(nil), r.ChannelIDs...)
	}
	return r
}

// NotifyChannels returns a deep copy of every configured channel.
func (st *Store) NotifyChannels() []agent.NotifyChannel {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]agent.NotifyChannel, len(st.s.Channels))
	for i, c := range st.s.Channels {
		out[i] = cloneChannel(c)
	}
	return out
}

// NotifyChannel returns a deep copy of one channel by id, or ok=false if unknown.
func (st *Store) NotifyChannel(id string) (agent.NotifyChannel, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, c := range st.s.Channels {
		if c.ID == id {
			return cloneChannel(c), true
		}
	}
	return agent.NotifyChannel{}, false
}

// SetNotifyChannel upserts a channel by id (replacing a same-id entry, else appending).
func (st *Store) SetNotifyChannel(ch agent.NotifyChannel) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	ch = cloneChannel(ch)
	for i, c := range st.s.Channels {
		if c.ID == ch.ID {
			st.s.Channels[i] = ch
			return st.save()
		}
	}
	st.s.Channels = append(st.s.Channels, ch)
	return st.save()
}

// DeleteNotifyChannel removes a channel by id. Idempotent — deleting an unknown id is a no-op.
func (st *Store) DeleteNotifyChannel(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := st.s.Channels[:0]
	removed := false
	for _, c := range st.s.Channels {
		if c.ID == id {
			removed = true
			continue
		}
		out = append(out, c)
	}
	st.s.Channels = out
	if !removed {
		return nil
	}
	return st.save()
}

// NotifyRules returns a deep copy of every configured routing rule.
func (st *Store) NotifyRules() []agent.NotifyRule {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]agent.NotifyRule, len(st.s.Rules))
	for i, r := range st.s.Rules {
		out[i] = cloneRule(r)
	}
	return out
}

// NotifyRule returns a deep copy of one rule by id, or ok=false if unknown.
func (st *Store) NotifyRule(id string) (agent.NotifyRule, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, r := range st.s.Rules {
		if r.ID == id {
			return cloneRule(r), true
		}
	}
	return agent.NotifyRule{}, false
}

// SetNotifyRule upserts a rule by id (replacing a same-id entry, else appending).
func (st *Store) SetNotifyRule(r agent.NotifyRule) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	r = cloneRule(r)
	for i, existing := range st.s.Rules {
		if existing.ID == r.ID {
			st.s.Rules[i] = r
			return st.save()
		}
	}
	st.s.Rules = append(st.s.Rules, r)
	return st.save()
}

// DeleteNotifyRule removes a rule by id. Idempotent — deleting an unknown id is a no-op.
func (st *Store) DeleteNotifyRule(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := st.s.Rules[:0]
	removed := false
	for _, r := range st.s.Rules {
		if r.ID == id {
			removed = true
			continue
		}
		out = append(out, r)
	}
	st.s.Rules = out
	if !removed {
		return nil
	}
	return st.save()
}
