package integrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/bots"
	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/shortcuts"
	"github.com/oblien/mindwire/packages/daemon/internal/stream"
)

const configKind = "integration-config"
const eventKind = "integration-event"
const threadKind = "integration-thread"

type BotRuntime interface {
	Delegate(registry.Bot, string, bots.TaskRequest) (bots.Task, error)
	Task(registry.Bot, string, string) (bots.Task, error)
	CancelTask(registry.Bot, string, string) (bots.Task, error)
}

type Service struct {
	db             *registry.Store
	bots           BotRuntime
	hub            *stream.Hub
	http           *http.Client
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	wg             sync.WaitGroup
	wake           chan struct{}
	active         map[string]bool
	activeThreads  map[string]bool
	activeStreams  int
	ownedRuns      map[string]string
	closed         bool
	started        bool
	teamsMu        sync.Mutex
	teamsKeys      map[string]teamKey
	teamsKeysUntil time.Time
	teamsRefreshAt time.Time
	tokens         map[string]accessToken
	receivers      map[string]*receiver
	receiverMu     sync.Mutex
	receiverStatus map[string]ReceiverStatus
	commandMu      sync.Mutex
}

// Options allows a host to supply its existing HTTP transport.
type Options struct{ HTTPClient *http.Client }

func New(db *registry.Store, runtime BotRuntime, hub *stream.Hub, options ...Options) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{db: db, bots: runtime, hub: hub, http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), active: map[string]bool{}, activeThreads: map[string]bool{}, tokens: map[string]accessToken{}}
	if len(options) > 0 && options[0].HTTPClient != nil {
		s.http = options[0].HTTPClient
	}
	s.ownedRuns = map[string]string{}
	s.receivers = map[string]*receiver{}
	s.receiverStatus = map[string]ReceiverStatus{}
	return s
}

func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return
	}
	s.started = true
	s.wg.Add(1)
	go s.dispatch()
	s.signal()
}

func (s *Service) Channels() []shortcuts.Channel {
	connections, err := s.List()
	if err != nil {
		return nil
	}
	out := []shortcuts.Channel{}
	for _, c := range connections {
		if c.Enabled {
			out = append(out, shortcuts.Channel{ID: "integration:" + c.ID, Name: c.Name, Type: c.Provider})
		}
	}
	return out
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	runs := s.ownedRuns
	s.ownedRuns = map[string]string{}
	s.mu.Unlock()
	for runID, botID := range runs {
		_, _ = s.bots.CancelTask(registry.Bot{}, botID, runID)
	}
}

func invalid(message string) error { return fmt.Errorf("%w: %s", registry.ErrInvalid, message) }

func (s *Service) Save(id string, spec Spec) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Connection{}, errors.New("integrations are closed")
	}
	var saved savedConnection
	if id != "" {
		if err := s.db.SurfaceGet(configKind, id, &saved); err != nil {
			return Connection{}, err
		}
		if saved.Revision != spec.ExpectedRevision {
			return Connection{}, registry.ErrConflict
		}
		if saved.Provider != spec.Provider || saved.BotID != spec.BotID || saved.Dispatch != spec.Dispatch {
			return Connection{}, invalid("provider, bot and dispatch cannot change; create a new connection")
		}
	} else {
		connections, err := s.List()
		if err != nil {
			return Connection{}, err
		}
		if len(connections) >= 64 {
			return Connection{}, invalid("a daemon supports up to 64 messaging connections")
		}
		id = "int-" + rand.Text()
	}
	if len(spec.Name) == 0 || len(spec.Name) > 120 || len(spec.ChannelIDs) == 0 || len(spec.ChannelIDs) > 100 || len(spec.UserIDs) > 100 {
		return Connection{}, invalid("a name and 1–100 allowed channels are required")
	}
	for _, value := range append(slices.Clone(spec.ChannelIDs), spec.UserIDs...) {
		if strings.TrimSpace(value) == "" || len(value) > 256 {
			return Connection{}, invalid("invalid channel or user ID")
		}
	}
	if !slices.Contains([]string{"slack", "teams", "discord", "telegram"}, spec.Provider) || !slices.Contains([]string{"notifications", "commands", "chat", "trigger"}, spec.Mode) {
		return Connection{}, invalid("unsupported provider or mode")
	}
	if spec.Transport == "" {
		spec.Transport = "webhook"
	}
	if !slices.Contains([]string{"webhook", "outbound"}, spec.Transport) || spec.Transport == "outbound" && spec.Provider == "teams" {
		return Connection{}, invalid("outbound connections support Slack, Discord and Telegram")
	}
	if !slices.Contains([]string{"", "bot", "host"}, spec.Dispatch) {
		return Connection{}, invalid("unsupported dispatch")
	}
	if spec.Dispatch == "host" {
		if spec.BotID != "" || spec.Notify || spec.Mode == "trigger" {
			return Connection{}, invalid("host dispatch has no bot or bot notifications")
		}
	} else {
		bot, err := s.db.Bot(spec.BotID)
		if err != nil {
			return Connection{}, err
		}
		if bot.Archived {
			return Connection{}, invalid("restore the bot before connecting it")
		}
	}
	if spec.Command == "" {
		spec.Command = "/mindwire"
	}
	if !strings.HasPrefix(spec.Command, "/") || strings.ContainsAny(spec.Command, " \n\t") || len(spec.Command) > 64 {
		return Connection{}, invalid("command must be a slash command without spaces")
	}
	if err := validateCommands(spec); err != nil {
		return Connection{}, err
	}
	if spec.Credentials != nil {
		if spec.Credentials.BotToken != "" && spec.Credentials.BotToken != saved.Secrets.BotToken {
			saved.BotUsername = ""
		}
		merge := func(dst *string, src string) {
			if src != "" {
				*dst = src
			}
		}
		merge(&saved.Secrets.BotToken, spec.Credentials.BotToken)
		merge(&saved.Secrets.AppToken, spec.Credentials.AppToken)
		merge(&saved.Secrets.SigningSecret, spec.Credentials.SigningSecret)
		merge(&saved.Secrets.WebhookSecret, spec.Credentials.WebhookSecret)
		merge(&saved.Secrets.PublicKey, spec.Credentials.PublicKey)
		merge(&saved.Secrets.ClientSecret, spec.Credentials.ClientSecret)
	}
	secrets := saved.Secrets
	if spec.Provider != "teams" && secrets.BotToken == "" {
		return Connection{}, invalid("bot token is required")
	}
	if spec.Provider == "slack" && (spec.WorkspaceID == "" || spec.Transport == "webhook" && secrets.SigningSecret == "" || spec.Transport == "outbound" && secrets.AppToken == "") {
		return Connection{}, invalid("Slack team ID and a signing secret (webhook) or app token (outbound) are required")
	}
	if spec.Provider == "telegram" && spec.Transport == "webhook" {
		if len(secrets.WebhookSecret) < 16 || len(secrets.WebhookSecret) > 256 || strings.IndexFunc(secrets.WebhookSecret, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
		}) >= 0 {
			return Connection{}, invalid("Telegram webhook secret must contain 16–256 letters, digits, underscores or hyphens")
		}
	}
	if spec.Provider == "discord" {
		key, err := hex.DecodeString(secrets.PublicKey)
		if spec.Transport == "webhook" && (err != nil || len(key) != 32) || spec.ApplicationID == "" || spec.WorkspaceID == "" {
			return Connection{}, invalid("Discord application ID, guild ID and Ed25519 public key are required")
		}
	}
	// One receiver per bot in a profile. Multiple pollers/sockets can steal events
	// from one another; channels belong on the same connection instead.
	{
		rows, err := s.db.SurfaceList(configKind)
		if err != nil {
			return Connection{}, err
		}
		for _, raw := range rows {
			var other savedConnection
			if err := json.Unmarshal(raw, &other); err != nil {
				return Connection{}, err
			}
			if other.ID != id && other.Provider == spec.Provider && other.Secrets.BotToken == secrets.BotToken && (spec.Transport == "outbound" || other.Transport == "outbound" || len(spec.Commands) > 0 || len(other.Commands) > 0) {
				return Connection{}, invalid("this bot already has a connection; add channels to that connection or use a separate bot")
			}
		}
	}
	if spec.Provider == "teams" {
		if spec.ApplicationID == "" || spec.WorkspaceID == "" || secrets.ClientSecret == "" {
			return Connection{}, invalid("Teams application ID, tenant ID and client secret are required")
		}
		if err := validateServiceURL(spec.ServiceURL); err != nil {
			return Connection{}, err
		}
	}
	if spec.Destination == "" {
		spec.Destination = spec.ChannelIDs[0]
	}
	if !slices.Contains(spec.ChannelIDs, spec.Destination) {
		return Connection{}, invalid("destination must be an allowed channel")
	}
	spec.Credentials = nil
	spec.ExpectedRevision = 0
	saved.Connection = Connection{ID: id, Revision: saved.Revision + 1, Spec: spec, HasCredentials: true, WebhookPath: "/integrations/" + id + "/webhook"}
	if spec.Transport == "outbound" {
		saved.WebhookPath = ""
	}
	if err := s.db.SurfacePut(configKind, id, saved); err != nil {
		return Connection{}, err
	}
	s.teamsMu.Lock()
	delete(s.tokens, id)
	s.teamsMu.Unlock()
	s.signal()
	return saved.Connection, nil
}

func validateServiceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return invalid("a public Teams connector HTTPS serviceUrl is required")
	}
	host := strings.ToLower(u.Hostname())
	if host != "smba.trafficmanager.net" && host != "smba.infra.teams.microsoft.com" && !strings.HasSuffix(host, ".smba.trafficmanager.net") && !strings.HasSuffix(host, ".smba.infra.teams.microsoft.com") {
		return invalid("serviceUrl must be a Microsoft Teams connector")
	}
	return nil
}

func (s *Service) load(id string) (savedConnection, error) {
	var c savedConnection
	err := s.db.SurfaceGet(configKind, id, &c)
	return c, err
}
func (s *Service) Get(id string) (Connection, error) {
	c, err := s.load(id)
	return s.withReceiver(c.Connection), err
}
func (s *Service) List() ([]Connection, error) {
	rows, err := s.db.SurfaceList(configKind)
	if err != nil {
		return nil, err
	}
	out := []Connection{}
	for _, row := range rows {
		var c savedConnection
		if err := json.Unmarshal(row, &c); err != nil {
			return nil, err
		}
		out = append(out, s.withReceiver(c.Connection))
	}
	return out, nil
}
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Workers re-read configuration before every send, so revocation takes effect
	// even while a bot is running. Conversation transcripts remain native records.
	defer s.signal()
	if err := s.db.SurfaceDelete(configKind, id); err != nil {
		return err
	}
	return s.db.SurfaceDelete(cursorKind, id)
}
func (s *Service) Events(id string) ([]Receipt, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	rows, err := s.db.IntegrationEvents(id, false)
	if err != nil {
		return nil, err
	}
	out := []Receipt{}
	for i := 0; i < len(rows); i++ {
		var e envelope
		if err := json.Unmarshal(rows[i], &e); err != nil {
			return nil, err
		}
		if e.IntegrationID == id {
			out = append(out, e.Receipt)
		}
	}
	return out, nil
}
func (s *Service) Retry(id, eventID string) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.Get(id); err != nil {
		return Receipt{}, err
	}
	var e envelope
	if err := s.db.SurfaceGet(eventKind, eventID, &e); err != nil {
		return Receipt{}, err
	}
	if e.IntegrationID != id {
		return Receipt{}, registry.ErrNotFound
	}
	if e.State != "failed" {
		return Receipt{}, registry.ErrConflict
	}
	e.State, e.Error = "queued", ""
	if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
		return Receipt{}, err
	}
	s.signal()
	return e.Receipt, nil
}

func (s *Service) accept(c savedConnection, e envelope) error {
	if e.ID == "" || e.ChannelID == "" || e.UserID == "" || len(e.Text) > 65536 {
		return invalid("invalid incoming event")
	}
	if !slices.Contains(c.ChannelIDs, e.ChannelID) || len(c.UserIDs) > 0 && !slices.Contains(c.UserIDs, e.UserID) || c.WorkspaceID != "" && c.Provider != "telegram" && c.WorkspaceID != e.Workspace {
		return errForbidden
	}
	if c.Mode == "notifications" || strings.TrimSpace(e.Text) == "" && !e.Command {
		return nil
	}
	// Telegram addresses commands to a bot in group chats (e.g. /openship@MyBot).
	if c.Provider == "telegram" {
		words := strings.Fields(e.Text)
		if len(words) > 0 && strings.HasPrefix(words[0], c.Command+"@") && len(words[0]) > len(c.Command)+1 {
			e.Command = true
		}
	}
	if c.Mode == "commands" && !e.Command && !strings.HasPrefix(e.Text, c.Command+" ") && e.Text != c.Command {
		return nil
	}
	e.IntegrationID = c.ID
	e.ID = "evt-" + registry.SyncKey(c.ID, e.ID)[:48]
	e.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	e.State = "queued"
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("integrations are closed")
	}
	current, err := s.load(c.ID)
	if err != nil {
		return err
	}
	if !current.Enabled || current.Revision != c.Revision || !s.allowed(current, e) {
		return errForbidden
	}
	var prior envelope
	err = s.db.SurfaceGet(eventKind, e.ID, &prior)
	if err == nil {
		return nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return err
	}
	// Cap queued work before acknowledging a provider; a full daemon asks it to retry.
	rows, err := s.db.IntegrationEvents("", true)
	if err != nil {
		return err
	}
	pending := 0
	for _, row := range rows {
		var item envelope
		if err := json.Unmarshal(row, &item); err != nil {
			return err
		}
		if item.State == "queued" || item.State == "running" {
			pending++
		}
	}
	if pending >= 256 {
		return errors.New("integration queue is full; retry later")
	}
	if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
		return err
	}
	return nil
}

func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) dispatch() {
	defer s.wg.Done()
	// Completed receipts expire after 30 days; pending deliveries are never pruned.
	_ = s.db.PruneIntegrationEvents()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.reconcileReceivers()
		rows, err := s.db.IntegrationEvents("", true)
		if err == nil {
			for _, row := range rows {
				if len(s.active) >= 12 {
					break
				}
				var e envelope
				if json.Unmarshal(row, &e) != nil || e.AwaitingAck || s.active[e.ID] || e.State != "queued" && e.State != "running" {
					continue
				}
				key := registry.SyncKey(e.IntegrationID, e.Workspace, e.ChannelID, e.ThreadID, e.UserID)
				c, _ := s.load(e.IntegrationID)
				if c.Dispatch == "host" && e.HostReply == nil {
					continue
				}
				control := controlCommand(c, e)
				if !control && (s.activeThreads[key] || s.activeStreams >= 8) {
					continue
				}
				if !control {
					s.activeThreads[key] = true
					s.activeStreams++
				}
				s.active[e.ID] = true
				s.wg.Add(1)
				go func(e envelope) {
					defer s.wg.Done()
					s.process(e)
					s.mu.Lock()
					delete(s.active, e.ID)
					if !control {
						delete(s.activeThreads, key)
						s.activeStreams--
					}
					s.mu.Unlock()
					s.signal()
				}(e)
			}
		}
		s.mu.Unlock()
	}
}

func (s *Service) process(e envelope) {
	key := registry.SyncKey(e.IntegrationID, e.Workspace, e.ChannelID, e.ThreadID, e.UserID)
	if s.ctx.Err() != nil {
		return
	}
	c, err := s.load(e.IntegrationID)
	if err == nil && !c.Enabled {
		err = errors.New("integration is disabled")
	}
	if err == nil && (!s.allowed(c, e) || c.Mode == "notifications") {
		err = errForbidden
	}
	if err == nil {
		if c.Dispatch == "host" {
			if e.HostReply == nil || c.Revision != e.HostRevision {
				err = errForbidden
			} else {
				err = s.reply(s.ctx, c, &e, *e.HostReply)
			}
		} else {
			err = s.run(c, &e, key)
		}
	}
	if s.ctx.Err() != nil {
		return
	} // Leave the durable receipt resumable after restart.
	e.State = "delivered"
	if err != nil {
		e.State = "failed"
		e.Error = publicError(err, c.Secrets)
		// Surface failures in the originating conversation without exposing runtime
		// error bodies (which can contain local paths or credentials).
		_ = s.reply(s.ctx, c, &e, "This request could not be completed. Check this integration's delivery activity in Mindwire.")
	}
	_ = s.db.SurfacePut(eventKind, e.ID, e)
}

func controlCommand(c savedConnection, e envelope) bool {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(e.Text), c.Command))
	return (e.Command || strings.HasPrefix(strings.TrimSpace(e.Text), c.Command+" ") || e.Text == c.Command) && slices.Contains([]string{"", "status", "stop", "reset", "help"}, text)
}

func (s *Service) run(c savedConnection, e *envelope, key string) error {
	var thread conversation
	if err := s.db.SurfaceGet(threadKind, key, &thread); err != nil && !errors.Is(err, registry.ErrNotFound) {
		return err
	}
	text := strings.TrimSpace(e.Text)
	if text == c.Command || strings.HasPrefix(text, c.Command+" ") {
		text = strings.TrimSpace(strings.TrimPrefix(text, c.Command))
	}
	if e.Command || strings.HasPrefix(e.Text, c.Command+" ") || e.Text == c.Command {
		switch text {
		case "", "help":
			return s.reply(s.ctx, c, e, "Use ask <message>, status, stop, or reset. Approvals stay in the Mindwire app.")
		case "reset":
			thread = conversation{Generation: thread.Generation + 1}
			if err := s.db.SurfacePut(threadKind, key, thread); err != nil {
				return err
			}
			return s.reply(s.ctx, c, e, "New conversation ready.")
		case "stop":
			if thread.RunID == "" {
				return s.reply(s.ctx, c, e, "No active task in this conversation.")
			}
			if _, err := s.bots.CancelTask(registry.Bot{}, c.BotID, thread.RunID); err != nil {
				return err
			}
			return s.reply(s.ctx, c, e, "Stop requested.")
		case "status":
			if thread.RunID == "" {
				return s.reply(s.ctx, c, e, "No task in this conversation.")
			}
			task, err := s.bots.Task(registry.Bot{}, c.BotID, thread.RunID)
			if err != nil {
				return err
			}
			return s.reply(s.ctx, c, e, "Task status: "+task.Run.Status)
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, "ask "))
	}
	if c.Mode == "trigger" {
		key = registry.SyncKey(key, e.ID)
	}
	chatID := e.ChatID
	if chatID == "" {
		chatID = thread.ChatID
	}
	if chatID == "" || c.Mode == "trigger" {
		chatID = "chat-" + registry.SyncKey("integration", key, fmt.Sprint(thread.Generation))[:40]
		if _, err := s.db.CreateBotChat(c.BotID, chatID, "Conversation from "+c.Name, ""); err != nil {
			return err
		}
	}
	if e.RunID == "" {
		if s.ctx.Err() != nil {
			return s.ctx.Err()
		}
		e.ChatID = chatID
		if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
			return err
		}
		task, err := s.bots.Delegate(registry.Bot{}, c.BotID, bots.TaskRequest{RequestID: e.ID, ChatID: chatID, Message: text})
		if err != nil {
			return err
		}
		e.ChatID, e.RunID, e.State = task.ChatID, task.Run.ID, "running"
		s.mu.Lock()
		s.ownedRuns[e.RunID] = c.BotID
		s.mu.Unlock()
		if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
			return err
		}
		if c.Mode != "trigger" {
			thread.ChatID, thread.RunID = e.ChatID, e.RunID
			if err := s.db.SurfacePut(threadKind, key, thread); err != nil {
				return err
			}
		}
	}
	err := s.streamReply(c, e)
	if err == nil {
		s.mu.Lock()
		delete(s.ownedRuns, e.RunID)
		s.mu.Unlock()
	}
	return err
}

func (s *Service) allowed(c savedConnection, e envelope) bool {
	return slices.Contains(c.ChannelIDs, e.ChannelID) && (len(c.UserIDs) == 0 || slices.Contains(c.UserIDs, e.UserID)) && (c.Provider == "telegram" || c.WorkspaceID == e.Workspace)
}

func (s *Service) Send(ctx context.Context, id, text string) error {
	c, err := s.load(id)
	if err != nil {
		return err
	}
	if !c.Enabled {
		return invalid("integration is disabled")
	}
	if strings.TrimSpace(text) == "" || len(text) > 65536 {
		return invalid("message must contain 1–65536 bytes")
	}
	return s.reply(ctx, c, &envelope{Receipt: Receipt{IntegrationID: id, ChannelID: c.Destination}}, text)
}

// Notify routes bot lifecycle notifications using the same transport and credentials
// as chat replies. App notifications select integration:<id> explicitly instead.
func (s *Service) Notify(ctx context.Context, n agent.Notification) error {
	connections, err := s.List()
	if err != nil {
		return err
	}
	var failures []error
	var mu sync.Mutex
	var wg sync.WaitGroup
	workers := make(chan struct{}, 8)
	for _, c := range connections {
		if !c.Enabled || !c.Notify || n.BotID != c.BotID || len(c.Conditions) > 0 && !slices.Contains(c.Conditions, n.Condition) {
			continue
		}
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-workers }()
			if err := s.Send(ctx, c.ID, strings.TrimSpace(n.Title+"\n"+n.Body)); err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(failures...)
}

func publicError(err error, secrets Credentials) string {
	message := err.Error()
	for _, secret := range []string{secrets.BotToken, secrets.AppToken, secrets.SigningSecret, secrets.WebhookSecret, secrets.ClientSecret} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}
