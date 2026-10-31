package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func outboundSpec(f *fixture, provider string) Spec {
	spec := f.spec(provider)
	spec.Transport, spec.Dispatch, spec.BotID = "outbound", "host", ""
	spec.Credentials.SigningSecret, spec.Credentials.WebhookSecret, spec.Credentials.PublicKey = "", "", ""
	spec.Credentials.AppToken = "fixture-app-token"
	return spec
}

func socketTransport(server *httptest.Server, api transportFunc) transportFunc {
	return func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Upgrade") != "websocket" {
			return api(r)
		}
		forward := r.Clone(r.Context())
		address, _ := url.Parse(server.URL)
		forward.URL.Scheme, forward.URL.Host = address.Scheme, address.Host
		return http.DefaultTransport.RoundTrip(forward)
	}
}

func TestOutboundTelegramPersistsCursorAndUsesHostQueue(t *testing.T) {
	f := newFixture(t)
	var largestOffset atomic.Int64
	var polls, deliveries atomic.Int32
	f.s.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getWebhookInfo"):
			return response(200, `{"ok":true,"result":{"url":""}}`), nil
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			var input struct {
				Offset  int64 `json:"offset"`
				Timeout int   `json:"timeout"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			if input.Timeout != 10 {
				t.Error("not using long polling")
			}
			largestOffset.Store(input.Offset)
			polls.Add(1)
			if input.Offset < 3 {
				return response(200, `{"ok":true,"result":[{"update_id":1,"message":{"text":"private","from":{"id":200},"chat":{"id":999}}},{"update_id":2,"message":{"text":"/mindwire projects","from":{"id":200},"chat":{"id":100}}}]}`), nil
			}
			return response(200, `{"ok":true,"result":[]}`), nil
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			deliveries.Add(1)
			return response(200, `{"ok":true,"result":{"message_id":44}}`), nil
		default:
			t.Error("unexpected Telegram mutation", r.URL.Path)
			return response(400, `{}`), nil
		}
	})
	c, err := f.s.Save("", outboundSpec(f, "telegram"))
	if err != nil {
		t.Fatal(err)
	}
	f.s.Start()
	waitFor(t, func() bool { return largestOffset.Load() == 3 })
	rows, _ := f.s.Events(c.ID)
	if len(rows) != 1 {
		t.Fatal("allowlist or dedup failed", rows)
	}
	e, err := f.s.Claim(c.ID)
	if err != nil || e == nil || e.Text != "projects" {
		t.Fatal(e, err)
	}
	if _, err := f.s.Complete(c.ID, e.ID, e.Lease, "Project fixture"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return deliveries.Load() == 1 })
	client := f.s.http
	f.s.Close()
	f.s = New(f.db, f.runtime, f.runtime.hub, Options{HTTPClient: client})
	before := polls.Load()
	f.s.Start()
	waitFor(t, func() bool { return polls.Load() > before })
	if largestOffset.Load() != 3 {
		t.Fatal("lost durable offset")
	}
	if e, err := f.s.Claim(c.ID); err != nil || e != nil {
		t.Fatal("replayed a completed command", e, err)
	}
	if f.runtime.count() != 0 {
		t.Fatal("host transport invoked an agent")
	}
	spec := c.Spec
	spec.Enabled, spec.ExpectedRevision = false, c.Revision
	if _, err := f.s.Save(c.ID, spec); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { f.s.mu.Lock(); defer f.s.mu.Unlock(); return len(f.s.receivers) == 0 })
	view, _ := f.s.Get(c.ID)
	if view.Receiver.State != "paused" {
		t.Fatal(view.Receiver)
	}
}

func TestOutboundTelegramNeverDeletesAnExistingWebhook(t *testing.T) {
	f := newFixture(t)
	f.s.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/getWebhookInfo") {
			t.Error("attempted to change another receiver")
		}
		return response(200, `{"ok":true,"result":{"url":"https://existing.example/bot"}}`), nil
	})
	c, _ := f.s.Save("", outboundSpec(f, "telegram"))
	f.s.Start()
	waitFor(t, func() bool { view, _ := f.s.Get(c.ID); return view.Receiver.State == "error" })
	view, _ := f.s.Get(c.ID)
	if !strings.Contains(view.Receiver.Error, "deleteWebhook") || strings.Contains(view.Receiver.Error, "existing.example") {
		t.Fatal(view.Receiver)
	}
}

func TestOutboundSlackSocketAcknowledgesDurablePrivateCommands(t *testing.T) {
	f := newFixture(t)
	var acknowledged atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		for i := 0; i < 2; i++ {
			packet := map[string]any{"type": "slash_commands", "envelope_id": fmt.Sprint(i), "payload": map[string]string{"command": "/mindwire", "trigger_id": "duplicate-event", "channel_id": "100", "user_id": "200", "team_id": "workspace", "text": "projects"}}
			if wsjson.Write(ctx, ws, packet) != nil {
				return
			}
			var ack map[string]string
			if wsjson.Read(ctx, ws, &ack) != nil {
				return
			}
			if ack["envelope_id"] != fmt.Sprint(i) {
				t.Error("incorrect Slack ack")
			}
			rows, _ := f.db.IntegrationEvents("", true)
			if len(rows) != 1 {
				t.Error("ack happened without a durable unique event", len(rows))
			}
			acknowledged.Add(1)
		}
		_, _, _ = ws.Read(ctx)
	}))
	defer server.Close()
	f.s.http.Transport = socketTransport(server, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/apps.connections.open" {
			if r.Header.Get("Authorization") != "Bearer fixture-app-token" {
				t.Error("wrong Socket Mode credential")
			}
			return response(200, `{"ok":true,"url":"wss://wss.slack.com/link?ticket=private-ticket"}`), nil
		}
		if r.URL.Path != "/api/chat.postEphemeral" {
			t.Error("host result was not private", r.URL.Path)
		}
		f.sent.Add(1)
		return response(200, `{"ok":true,"message_ts":"44.1"}`), nil
	})
	c, err := f.s.Save("", outboundSpec(f, "slack"))
	if err != nil {
		t.Fatal(err)
	}
	f.s.Start()
	defer f.s.Close()
	waitFor(t, func() bool { return acknowledged.Load() == 2 })
	e, err := f.s.Claim(c.ID)
	if err != nil || e == nil || e.UserID != "200" || e.WorkspaceID != "workspace" {
		t.Fatal(e, err)
	}
	if _, err := f.s.Complete(c.ID, e.ID, e.Lease, "Your project"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.sent.Load() == 1 })
	view, _ := f.s.Get(c.ID)
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "fixture-app-token") || strings.Contains(string(raw), "private-ticket") || view.WebhookPath != "" {
		t.Fatal("secret or webhook leaked", string(raw))
	}
}

func TestOutboundDiscordGatewayHeartbeatAndResume(t *testing.T) {
	f := newFixture(t)
	var sessions, acknowledgements, heartbeats, delivered atomic.Int32
	var connectionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		wsjson.Write(ctx, ws, map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 200}})
		var auth struct {
			Op   int `json:"op"`
			Data struct {
				Token    string `json:"token"`
				Session  string `json:"session_id"`
				Sequence int    `json:"seq"`
				Intents  int    `json:"intents"`
			} `json:"d"`
		}
		if wsjson.Read(ctx, ws, &auth) != nil {
			return
		}
		n := sessions.Add(1)
		if auth.Data.Token != "test-bot-token" || auth.Data.Intents != 0 {
			t.Error("wrong Gateway authentication")
		}
		if n == 1 {
			if auth.Op != 2 {
				t.Error("first connection did not identify")
			}
			wsjson.Write(ctx, ws, map[string]any{"op": 0, "s": 1, "t": "READY", "d": map[string]any{"session_id": "private-session", "resume_gateway_url": "wss://gateway.discord.gg", "application": map[string]string{"id": "application"}}})
			body := json.RawMessage(`{"id":"42","type":2,"token":"private-interaction","application_id":"application","guild_id":"workspace","channel_id":"100","member":{"user":{"id":"200"}},"data":{"name":"mindwire","options":[{"name":"message","value":"projects"}]}}`)
			wsjson.Write(ctx, ws, map[string]any{"op": 0, "s": 2, "t": "INTERACTION_CREATE", "d": body})
		} else {
			if auth.Op != 6 || auth.Data.Session != "private-session" || auth.Data.Sequence != 2 {
				t.Error("lost resume cursor", auth.Op, auth.Data.Sequence)
			}
			wsjson.Write(ctx, ws, map[string]any{"op": 0, "s": 3, "t": "RESUMED", "d": map[string]any{}})
		}
		for {
			var packet gatewayPacket
			if wsjson.Read(ctx, ws, &packet) != nil {
				return
			}
			if packet.Op == 1 {
				heartbeats.Add(1)
				wsjson.Write(ctx, ws, map[string]int{"op": 11})
			}
		}
	}))
	defer server.Close()
	f.s.http.Transport = socketTransport(server, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v10/oauth2/applications/@me":
			return response(200, `{"id":"application","interactions_endpoint_url":null}`), nil
		case "/api/v10/gateway/bot":
			return response(200, `{"url":"wss://gateway.discord.gg","session_start_limit":{"remaining":10,"reset_after":5000}}`), nil
		case "/api/v10/interactions/42/private-interaction/callback":
			var ack struct {
				Type int `json:"type"`
				Data struct {
					Flags int `json:"flags"`
				} `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&ack)
			if ack.Type != 5 || ack.Data.Flags != 64 {
				t.Error("interaction must be deferred privately")
			}
			if event, _ := f.s.Claim(connectionID); event != nil {
				t.Error("dispatched before provider acknowledgement")
			}
			acknowledgements.Add(1)
			return response(204, ""), nil
		case "/api/v10/webhooks/application/private-interaction/messages/@original":
			delivered.Add(1)
			return response(200, `{"id":"response"}`), nil
		default:
			t.Error("unexpected provider endpoint", r.URL.Path)
			return response(404, `{}`), nil
		}
	})
	c, err := f.s.Save("", outboundSpec(f, "discord"))
	if err != nil {
		t.Fatal(err)
	}
	connectionID = c.ID
	f.s.Start()
	waitFor(t, func() bool { return acknowledgements.Load() == 1 && heartbeats.Load() >= 2 })
	e, err := f.s.Claim(c.ID)
	if err != nil || e == nil {
		t.Fatal(e, err)
	}
	if _, err := f.s.Complete(c.ID, e.ID, e.Lease, "Your projects"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return delivered.Load() == 1 })
	client := f.s.http
	f.s.Close()
	f.s = New(f.db, f.runtime, f.runtime.hub, Options{HTTPClient: client})
	f.s.Start()
	defer f.s.Close()
	waitFor(t, func() bool { return sessions.Load() == 2 })
	if e, err := f.s.Claim(c.ID); err != nil || e != nil {
		t.Fatal("duplicate Gateway execution", e, err)
	}
}

func TestOutboundCannotBeForgedByWebhookOrStaleConfiguration(t *testing.T) {
	f := newFixture(t)
	spec := outboundSpec(f, "telegram")
	c, err := f.s.Save("", spec)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/ignored", strings.NewReader(`{"update_id":1}`))
	r.SetPathValue("id", c.ID)
	w := httptest.NewRecorder()
	f.s.Webhook(w, r)
	if w.Code != 404 {
		t.Fatal("outbound connector accepted an unsigned webhook", w.Code)
	}
	saved, _ := f.s.load(c.ID)
	spec.ExpectedRevision, spec.Enabled = c.Revision, false
	if _, err := f.s.Save(c.ID, spec); err != nil {
		t.Fatal(err)
	}
	if err := f.s.accept(saved, envelope{Receipt: Receipt{ID: "1", ChannelID: "100", UserID: "200"}, Text: "projects"}); err == nil {
		t.Fatal("stale receiver admitted work")
	}
	if _, err := f.s.Save("", outboundSpec(f, "telegram")); err == nil {
		t.Fatal("two receivers accepted for one bot")
	}
	if _, err := f.s.Save("", outboundSpec(f, "teams")); err == nil {
		t.Fatal("unsupported Teams outbound accepted")
	}
	for _, address := range []string{"ws://gateway.discord.gg", "wss://gateway.discord.gg.attacker.example", "wss://localhost", "wss://user:pass@wss.slack.com"} {
		if _, err := f.s.dialProvider(context.Background(), address, "discord"); err == nil {
			t.Fatal("untrusted provider URL allowed", address)
		}
	}
}

func TestOutboundDiscordRecoversAcknowledgementWithoutReexecution(t *testing.T) {
	f := newFixture(t)
	c, err := f.s.Save("", outboundSpec(f, "discord"))
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := f.s.load(c.ID)
	raw := []byte(`{"id":"42","type":2,"token":"private-interaction","application_id":"application","guild_id":"workspace","channel_id":"100","member":{"user":{"id":"200"}},"data":{"name":"mindwire","options":[{"name":"message","value":"projects"}]}}`)
	if _, _, err := f.s.receiveEvent(saved, raw, "application/json"); err != nil {
		t.Fatal(err)
	}
	if e, err := f.s.Claim(c.ID); err != nil || e != nil {
		t.Fatal("claimed unacknowledged event", e, err)
	}
	f.s.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "/42/private-interaction/callback") {
			t.Error("lost original interaction ID")
		}
		return response(400, `{"code":40060,"message":"already acknowledged"}`), nil
	})
	if err := f.s.recoverDiscordAcks(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	e, err := f.s.Claim(c.ID)
	if err != nil || e == nil {
		t.Fatal(e, err)
	}
	if err := f.s.recoverDiscordAcks(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.s.Claim(c.ID); again != nil {
		t.Fatal("reclaimed active work")
	}
}

func TestOutboundDiscordRetiresExpiredAndRemovedAcknowledgements(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(fmt.Sprint("removed=", removed), func(t *testing.T) {
			f := newFixture(t)
			c, err := f.s.Save("", outboundSpec(f, "discord"))
			if err != nil {
				t.Fatal(err)
			}
			saved, _ := f.s.load(c.ID)
			e := envelope{Receipt: Receipt{ID: "42", ChannelID: "100", UserID: "200"}, Workspace: "workspace", Text: "projects", Command: true, AwaitingAck: true, ProviderEventID: "42", InteractionToken: "fixture-token"}
			if err := f.s.accept(saved, e); err != nil {
				t.Fatal(err)
			}
			f.s.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if removed {
					t.Error("contacted a removed destination")
				}
				return response(404, `{"code":10062,"message":"unknown interaction"}`), nil
			})
			if removed {
				spec := c.Spec
				spec.ChannelIDs, spec.Destination, spec.ExpectedRevision = []string{"101"}, "101", c.Revision
				if _, err := f.s.Save(c.ID, spec); err != nil {
					t.Fatal(err)
				}
				saved, _ = f.s.load(c.ID)
			}
			if err := f.s.recoverDiscordAcks(context.Background(), saved); err != nil {
				t.Fatal(err)
			}
			if event, err := f.s.Claim(c.ID); err != nil || event != nil {
				t.Fatal("expired or removed work executed", event, err)
			}
			rows, _ := f.s.Events(c.ID)
			if len(rows) != 1 || rows[0].State != "failed" {
				t.Fatal("acknowledgement still pending", rows)
			}
		})
	}
}
