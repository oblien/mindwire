package integrations

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/bots"
	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/session"
	"github.com/oblien/mindwire/packages/daemon/internal/stream"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

type runtimeFixture struct {
	mu    sync.Mutex
	hub   *stream.Hub
	tasks map[string]bots.Task
	calls []bots.TaskRequest
	hold  bool
}

func (f *runtimeFixture) Delegate(_ registry.Bot, bot string, in bots.TaskRequest) (bots.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if task, ok := f.tasks[in.RequestID]; ok {
		return task, nil
	}
	f.calls = append(f.calls, in)
	task := bots.Task{BotID: bot, ChatID: in.ChatID, Run: session.Run{ID: in.RequestID, ChatID: in.ChatID, Status: "done"}, Output: "Hello from the bot: " + in.Message}
	if f.hold {
		task.Run.Status = "running"
	}
	f.tasks[in.RequestID] = task
	f.hub.Publish(task.Run.ID, agent.Event{Type: agent.EventText, Text: task.Output, Delta: true})
	if !f.hold {
		f.hub.Publish(task.Run.ID, agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: task.Output}})
		f.hub.Close(task.Run.ID)
	}
	return task, nil
}
func (f *runtimeFixture) Task(_ registry.Bot, _ string, id string) (bots.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tasks[id], nil
}
func (f *runtimeFixture) CancelTask(_ registry.Bot, _ string, id string) (bots.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task := f.tasks[id]
	task.Run.Status = "cancelled"
	f.tasks[id] = task
	f.hub.Close(id)
	return task, nil
}
func (f *runtimeFixture) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

type fixture struct {
	s       *Service
	db      *registry.Store
	bot     registry.Bot
	runtime *runtimeFixture
	private ed25519.PrivateKey
	rsa     *rsa.PrivateKey
	sent    atomic.Int32
	fail    atomic.Bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := registry.Open(filepath.Join(dir, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetBotEngines([]string{"integration-fixture"})
	if err := db.Import(registry.Import{Agents: []registry.Agent{{Record: registry.Record{ID: "agent"}, Name: "Agent", AgentType: "integration-fixture"}}, Projects: []registry.Project{{Record: registry.Record{ID: "project"}, Name: "Project", Path: dir}}}); err != nil {
		t.Fatal(err)
	}
	bot, err := db.CreateBot(registry.Bot{AgentID: "agent", ProjectID: "project", Name: "My bot"})
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.New()
	runtime := &runtimeFixture{hub: hub, tasks: map[string]bots.Task{}}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, bot: bot, runtime: runtime, private: private, rsa: rsaKey}
	f.s = New(db, runtime, hub)
	f.s.teamsKeys = map[string]teamKey{"fixture": {&rsaKey.PublicKey, []string{"msteams"}}}
	f.s.teamsKeysUntil = time.Now().Add(time.Hour)
	f.s.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "login.microsoftonline.com" {
			return response(200, `{"access_token":"teams-access","expires_in":3600}`), nil
		}
		f.sent.Add(1)
		if f.fail.Load() {
			return response(503, `{"error":"secret provider response"}`), nil
		}
		if r.URL.Host == "api.telegram.org" {
			return response(200, `{"ok":true,"result":{"message_id":44}}`), nil
		}
		if r.URL.Host == "slack.com" {
			if r.URL.Path == "/api/chat.postEphemeral" {
				return response(200, `{"ok":true,"message_ts":"44.1"}`), nil
			}
			return response(200, `{"ok":true,"ts":"44.1"}`), nil
		}
		return response(200, `{"id":"44"}`), nil
	})
	t.Cleanup(func() { f.s.Close(); db.Close() })
	return f
}
func (f *fixture) spec(provider string) Spec {
	c := Spec{Name: "My " + provider, Provider: provider, BotID: f.bot.ID, Mode: "chat", Enabled: true, ChannelIDs: []string{"100"}, WorkspaceID: "workspace", ApplicationID: "application", ServiceURL: "https://smba.trafficmanager.net/emea/", Credentials: &Credentials{BotToken: "test-bot-token", SigningSecret: "test-signing-secret", WebhookSecret: "test-webhook-secret", PublicKey: hex.EncodeToString(f.private.Public().(ed25519.PublicKey)), ClientSecret: "test-client-secret"}}
	if provider == "telegram" {
		c.WorkspaceID = ""
	}
	return c
}
func (f *fixture) create(t *testing.T, provider string) Connection {
	t.Helper()
	c, err := f.s.Save("", f.spec(provider))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (f *fixture) incoming(t *testing.T, c Connection, id, text, user, channel string) *http.Request {
	t.Helper()
	var body string
	switch c.Provider {
	case "slack":
		body = fmt.Sprintf(`{"type":"event_callback","team_id":"workspace","event_id":%q,"event":{"type":"message","user":%q,"channel":%q,"text":%q,"ts":%q,"thread_ts":"1.0"}}`, id, user, channel, text, id)
	case "telegram":
		body = fmt.Sprintf(`{"update_id":%s,"message":{"message_id":1,"text":%q,"from":{"id":%s},"chat":{"id":%s}}}`, id, text, user, channel)
	case "discord":
		body = fmt.Sprintf(`{"id":%q,"type":2,"token":"interaction-secret","application_id":"application","guild_id":"workspace","channel_id":%q,"member":{"user":{"id":%q}},"data":{"name":"mindwire","options":[{"name":"message","value":%q}]}}`, id, channel, user, text)
	case "teams":
		body = fmt.Sprintf(`{"id":%q,"type":"message","text":%q,"channelId":"msteams","serviceUrl":"https://smba.trafficmanager.net/emea/","from":{"id":%q},"conversation":{"id":%q},"channelData":{"tenant":{"id":"workspace"}}}`, id, text, user, channel)
	}
	r := httptest.NewRequest("POST", c.WebhookPath, strings.NewReader(body))
	r.SetPathValue("id", c.ID)
	r.Header.Set("Content-Type", "application/json")
	ts := fmt.Sprint(time.Now().Unix())
	switch c.Provider {
	case "slack":
		mac := hmac.New(sha256.New, []byte("test-signing-secret"))
		mac.Write([]byte("v0:" + ts + ":" + body))
		r.Header.Set("X-Slack-Request-Timestamp", ts)
		r.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	case "telegram":
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-webhook-secret")
	case "discord":
		r.Header.Set("X-Signature-Timestamp", ts)
		r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(f.private, []byte(ts+body))))
	case "teams":
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"iss":"https://api.botframework.com","aud":"application","exp":%d,"nbf":%d,"serviceurl":"https://smba.trafficmanager.net/emea/"}`, time.Now().Add(time.Hour).Unix(), time.Now().Add(-time.Minute).Unix())))
		digest := sha256.Sum256([]byte(header + "." + payload))
		sig, err := rsa.SignPKCS1v15(rand.Reader, f.rsa, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+header+"."+payload+"."+base64.RawURLEncoding.EncodeToString(sig))
	}
	return r
}
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for integration delivery")
}
func (f *fixture) post(t *testing.T, c Connection, id, text, user, channel string) int {
	t.Helper()
	w := httptest.NewRecorder()
	f.s.Webhook(w, f.incoming(t, c, id, text, user, channel))
	return w.Code
}
func (f *fixture) waitState(t *testing.T, id, state string, n int) []Receipt {
	t.Helper()
	var receipts []Receipt
	waitFor(t, func() bool {
		var err error
		receipts, err = f.s.Events(id)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, e := range receipts {
			if e.State == state {
				count++
			}
		}
		return count == n
	})
	return receipts
}

func TestProviderWebhookToBotAndDelivery(t *testing.T) {
	for _, provider := range []string{"slack", "telegram", "discord", "teams"} {
		t.Run(provider, func(t *testing.T) {
			f := newFixture(t)
			c := f.create(t, provider)
			// Acknowledgement is durable even before the dispatcher is started.
			if code := f.post(t, c, "1", "hello", "200", "100"); code != 200 {
				t.Fatalf("webhook: %d", code)
			}
			if f.runtime.count() != 0 {
				t.Fatal("webhook ran a bot synchronously")
			}
			f.s.Start()
			receipts := f.waitState(t, c.ID, "delivered", 1)
			if receipts[0].ChatID == "" || receipts[0].RunID == "" || f.sent.Load() == 0 {
				t.Fatalf("missing native task or delivery: %+v", receipts)
			}
			if code := f.post(t, c, "1", "hello", "200", "100"); code != 200 {
				t.Fatal(code)
			}
			if f.runtime.count() != 1 {
				t.Fatal("provider retry executed twice")
			}
			if code := f.post(t, c, "2", "follow up", "200", "100"); code != 200 {
				t.Fatal(code)
			}
			if receipts = f.waitState(t, c.ID, "delivered", 2); receipts[0].ChatID != receipts[1].ChatID {
				t.Fatal("thread did not retain conversation")
			}
			if code := f.post(t, c, "3", "other user", "201", "100"); code != 200 {
				t.Fatal(code)
			}
			receipts = f.waitState(t, c.ID, "delivered", 3)
			if receipts[0].ChatID == receipts[1].ChatID {
				t.Fatal("different users shared bot context")
			}
			data, _ := json.Marshal(receipts)
			if strings.Contains(string(data), "interaction-secret") || strings.Contains(string(data), "hello") {
				t.Fatal("receipt exposed private envelope")
			}
		})
	}
}

func TestRejectsForgeryAndUnauthorizedChannels(t *testing.T) {
	for _, provider := range []string{"slack", "telegram", "discord", "teams"} {
		t.Run(provider, func(t *testing.T) {
			f := newFixture(t)
			c := f.create(t, provider)
			r := f.incoming(t, c, "1", "hello", "200", "100")
			r.Header = make(http.Header)
			w := httptest.NewRecorder()
			f.s.Webhook(w, r)
			if w.Code != 401 {
				t.Fatalf("unsigned event: %d", w.Code)
			}
			if code := f.post(t, c, "2", "hello", "200", "999"); code != 403 {
				t.Fatalf("unknown channel: %d", code)
			}
			r = f.incoming(t, c, "3", "hello", "200", "100")
			r.Header.Set("X-Slack-Request-Timestamp", "1")
			r.Header.Set("X-Signature-Timestamp", "1")
			if provider == "slack" || provider == "discord" {
				w = httptest.NewRecorder()
				f.s.Webhook(w, r)
				if w.Code != 401 {
					t.Fatal("accepted stale signed request")
				}
			}
			if f.runtime.count() != 0 {
				t.Fatal("unauthorized event started a bot")
			}
		})
	}
}

func TestModesCredentialsAndRevision(t *testing.T) {
	f := newFixture(t)
	spec := f.spec("telegram")
	spec.Mode = "commands"
	spec.UserIDs = []string{"200"}
	c, err := f.s.Save("", spec)
	if err != nil {
		t.Fatal(err)
	}
	if code := f.post(t, c, "1", "ordinary message", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	if events, _ := f.s.Events(c.ID); len(events) != 0 {
		t.Fatal("commands mode queued plain text")
	}
	if code := f.post(t, c, "2", "/mindwire ask hello", "201", "100"); code != 403 {
		t.Fatal("user allowlist ignored")
	}
	if code := f.post(t, c, "3", "/mindwire ask hello", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	f.s.Start()
	f.waitState(t, c.ID, "delivered", 1)
	spec = c.Spec
	spec.ExpectedRevision = c.Revision
	spec.Mode = "notifications"
	spec.Notify = true
	updated, err := f.s.Save(c.ID, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Save(c.ID, spec); err != registry.ErrConflict {
		t.Fatal("stale update accepted", err)
	}
	saved, _ := f.s.load(c.ID)
	if saved.Secrets.BotToken != "test-bot-token" {
		t.Fatal("omitted credentials were erased")
	}
	data, _ := json.Marshal(updated)
	if strings.Contains(string(data), "test-bot-token") || strings.Contains(string(data), "secrets") {
		t.Fatal("credentials leaked in public config")
	}
	if code := f.post(t, updated, "4", "hello", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	if events, _ := f.s.Events(c.ID); len(events) != 1 {
		t.Fatal("notifications mode started a task")
	}
	if err := f.s.Notify(context.Background(), agent.Notification{BotID: f.bot.ID, Condition: agent.Finished, Title: "Done"}); err != nil {
		t.Fatal(err)
	}
	if f.runtime.count() != 1 {
		t.Fatal("notification ran a bot")
	}
}

func TestFailedDeliveryRetryReusesTaskAndRestartKeepsQueue(t *testing.T) {
	f := newFixture(t)
	c := f.create(t, "telegram")
	f.fail.Store(true)
	if code := f.post(t, c, "1", "hello", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	f.s.Start()
	events := f.waitState(t, c.ID, "failed", 1)
	if strings.Contains(events[0].Error, "secret provider response") {
		t.Fatal("raw provider error leaked")
	}
	f.fail.Store(false)
	if _, err := f.s.Retry(c.ID, events[0].ID); err != nil {
		t.Fatal(err)
	}
	f.waitState(t, c.ID, "delivered", 1)
	if f.runtime.count() != 1 {
		t.Fatal("retry created a second bot task")
	}
	f.s.Close()
	restored := New(f.db, f.runtime, f.runtime.hub)
	restored.http = f.s.http
	f.s = restored
	if code := f.post(t, c, "2", "after restart", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	f.s.Start()
	f.waitState(t, c.ID, "delivered", 2)
	if f.runtime.count() != 2 {
		t.Fatal("restart lost pending task")
	}
	if code := f.post(t, c, "1", "hello", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	if f.runtime.count() != 2 {
		t.Fatal("restart lost replay receipt")
	}
}

func TestTeamsClaimsAndDestinationCannotExfiltrateTokens(t *testing.T) {
	f := newFixture(t)
	spec := f.spec("teams")
	for _, bad := range []string{"http://smba.trafficmanager.net/", "https://smba.trafficmanager.net.evil.example/", "https://127.0.0.1/", "https://smba.trafficmanager.net:443/", "https://user@smba.trafficmanager.net/"} {
		spec.ServiceURL = bad
		if _, err := f.s.Save("", spec); err == nil {
			t.Fatal("accepted unsafe Teams URL", bad)
		}
	}
	c := f.create(t, "teams")
	r := f.incoming(t, c, "1", "hi", "200", "100")
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(string(body), "/emea/", "/different/")))
	w := httptest.NewRecorder()
	f.s.Webhook(w, r)
	if w.Code != 401 {
		t.Fatal("accepted mismatched signed service URL")
	}
	f.s.teamsKeys["fixture"] = teamKey{&f.rsa.PublicKey, []string{"other-channel"}}
	if code := f.post(t, c, "2", "hi", "200", "100"); code != 401 {
		t.Fatal("accepted key without Teams endorsement")
	}
}

func TestChunkingRateLimitsAndAPILevelErrors(t *testing.T) {
	f := newFixture(t)
	text := strings.Repeat("😀é", 2500)
	parts := chunks(text, 1900)
	if strings.Join(parts, "") != text {
		t.Fatal("chunking corrupted unicode")
	}
	for _, part := range parts {
		if len(utf16.Encode([]rune(part))) > 1900 {
			t.Fatal("oversize chunk")
		}
	}
	var calls atomic.Int32
	f.s.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			res := response(429, `{"retry_after":0.001}`)
			return res, nil
		}
		return response(200, `{"ok":false,"error":"missing_scope"}`), nil
	})
	_, err := f.s.request(context.Background(), "POST", "https://slack.com/api/chat.postMessage", "Bearer secret", map[string]string{"text": "hello"})
	if err == nil || calls.Load() != 2 || strings.Contains(err.Error(), "secret") {
		t.Fatal("API rejection or rate limit ignored", err, calls.Load())
	}
}

func TestStreamEditsAndControlCommandsDoNotWaitForCompletion(t *testing.T) {
	f := newFixture(t)
	f.runtime.hold = true
	c := f.create(t, "telegram")
	var mu sync.Mutex
	methods := []string{}
	f.s.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		methods = append(methods, r.URL.Path)
		mu.Unlock()
		return response(200, `{"ok":true,"result":{"message_id":44}}`), nil
	})
	f.s.Start()
	if code := f.post(t, c, "1", "hello", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	waitFor(t, func() bool { events, _ := f.s.Events(c.ID); return len(events) == 1 && events[0].RunID != "" })
	events, _ := f.s.Events(c.ID)
	runID := events[0].RunID
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(methods) > 0 })
	for i := 0; i < 200; i++ {
		f.runtime.hub.Publish(runID, agent.Event{Type: agent.EventText, Text: " token", Delta: true})
	}
	if code := f.post(t, c, "2", "/mindwire status", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	f.waitState(t, c.ID, "delivered", 1)
	if f.runtime.count() != 1 {
		t.Fatal("status created a new bot turn")
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, method := range methods {
			if strings.HasSuffix(method, "/editMessageText") {
				return true
			}
		}
		return false
	})
	mu.Lock()
	count := len(methods)
	mu.Unlock()
	if count > 4 {
		t.Fatalf("token bursts were not coalesced: %d requests", count)
	}
	if code := f.post(t, c, "3", "/mindwire stop", "200", "100"); code != 200 {
		t.Fatal(code)
	}
	f.waitState(t, c.ID, "delivered", 3)
	task, _ := f.runtime.Task(registry.Bot{}, c.BotID, runID)
	if task.Run.Status != "cancelled" {
		t.Fatal("stop did not cancel active work")
	}
}

func TestTriggerModeStartsSeparateChatsAndDisableRevokesOutput(t *testing.T) {
	f := newFixture(t)
	spec := f.spec("telegram")
	spec.Mode = "trigger"
	c, err := f.s.Save("", spec)
	if err != nil {
		t.Fatal(err)
	}
	f.s.Start()
	for _, id := range []string{"1", "2"} {
		if code := f.post(t, c, id, "run a task", "200", "100"); code != 200 {
			t.Fatal(code)
		}
	}
	events := f.waitState(t, c.ID, "delivered", 2)
	if events[0].ChatID == events[1].ChatID {
		t.Fatal("triggers reused a conversation")
	}
	spec = c.Spec
	spec.ExpectedRevision = c.Revision
	spec.Enabled = false
	if _, err := f.s.Save(c.ID, spec); err != nil {
		t.Fatal(err)
	}
	before := f.sent.Load()
	if err := f.s.Send(context.Background(), c.ID, "should not send"); err == nil {
		t.Fatal("disabled connection sent a message")
	}
	if code := f.post(t, c, "3", "hello", "200", "100"); code != 404 {
		t.Fatal("disabled webhook accepted input")
	}
	if f.sent.Load() != before {
		t.Fatal("revocation sent output")
	}
}
