package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"github.com/oblien/mindwire/packages/daemon/internal/integrations"
	"github.com/oblien/mindwire/packages/daemon/internal/notify"
	"github.com/oblien/mindwire/packages/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/packages/daemon/internal/registry"
	"github.com/oblien/mindwire/packages/daemon/internal/session"
	"github.com/oblien/mindwire/packages/daemon/internal/stream"
)

type messagingHarness struct {
	resolveFakeAdapter
	turns *atomic.Int32
}

func (f messagingHarness) Capabilities() agent.Capabilities {
	return agent.Capabilities{BotProfiles: true}
}
func (f messagingHarness) RunStream(_ context.Context, input agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	f.turns.Add(1)
	text := "A real supervised response"
	emit(agent.Event{Type: agent.EventText, Text: text, Delta: true})
	emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: text}})
	return agent.TurnResult{Text: text}, nil
}

// This crosses SDK -> authenticated HTTP -> registry -> signed webhook -> bot
// binding -> real supervisor/runner -> event hub -> provider HTTP -> SDK receipt.
// Only the external harness and Slack service are deterministic fixtures.
func TestIntegrationsTypeScriptSDKEndToEnd(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("Bun is needed for the cross-SDK end-to-end test")
	}
	dir := t.TempDir()
	var turns, deliveries atomic.Int32
	harness := messagingHarness{resolveFakeAdapter{id: "messaging-e2e"}, &turns}
	agent.Register(harness)
	store, err := session.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := registry.Open(filepath.Join(dir, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	hub := stream.New()
	sup := orchestrator.New(store, hub, notify.Noop{}, dir, harness.ID())
	a := New(store, hub, sup, db)
	if a.InitError() != nil {
		t.Fatal(a.InitError())
	}
	defer a.Close()
	if err := db.Import(registry.Import{Agents: []registry.Agent{{Record: registry.Record{ID: "profile"}, Name: "Agent", AgentType: harness.ID()}}, Projects: []registry.Project{{Record: registry.Record{ID: "project"}, Name: "Project", Path: dir}}}); err != nil {
		t.Fatal(err)
	}
	bot, err := db.CreateBot(registry.Bot{AgentID: "profile", ProjectID: "project", Name: "Chat bot"})
	if err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer slack-fixture-token" {
			t.Error("provider lost bot credential")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["channel"] != "C100" {
			t.Error("provider destination changed", payload)
		}
		deliveries.Add(1)
		writeJSON(w, 200, map[string]any{"ok": true, "ts": "123.456"})
	}))
	defer provider.Close()
	a.integrations.Close()
	client := provider.Client()
	client.Transport = messagingTransport{target: provider.URL}
	a.integrations = integrations.New(db, sup.BotService(), hub, integrations.Options{HTTPClient: client})
	a.apps.SetIntegrationChannels(a.integrations)
	a.integrations.Start()
	mux := http.NewServeMux()
	a.Register(mux)
	root := http.NewServeMux()
	root.HandleFunc("POST /integrations/{id}/webhook", a.IntegrationWebhook)
	root.HandleFunc("POST /apps/{id}/notify", a.AppNotify)
	root.Handle("/", Auth("owner-fixture-token", mux))
	server := httptest.NewServer(root)
	defer server.Close()
	if err := a.SetAppsAddress(server.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	sdk, err := filepath.Abs("../../../sdk/src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { createHmac } from "node:crypto";
const { Mindwire, remote } = await import(process.env.MINDWIRE_TEST_SDK);
const mw = new Mindwire({ target: remote(process.env.MINDWIRE_TEST_URL, { token: "owner-fixture-token" }) });
const assert = (ok, message) => { if (!ok) throw new Error(message); };
const c = await mw.integrations.create({ name: "Slack E2E", provider: "slack", botId: process.env.MINDWIRE_TEST_BOT, mode: "chat", enabled: true, notify: false, channelIds: ["C100"], workspaceId: "T100", credentials: { botToken: "slack-fixture-token", signingSecret: "slack-fixture-signing" } });
assert(c.hasCredentials && !JSON.stringify(c).includes("slack-fixture-token"), "write-only credentials leaked");
assert((await mw.integrations.list()).length === 1, "connection not listed");
await mw.integrations.send(c.id, "Connection test");
const post = async (id) => {
  const body = JSON.stringify({ type: "event_callback", team_id: "T100", event_id: id, event: { type: "message", channel: "C100", user: "U100", ts: "1", thread_ts: "1", text: "Hello from Slack" } });
  const ts = String(Math.floor(Date.now() / 1000));
  const signature = "v0=" + createHmac("sha256", "slack-fixture-signing").update("v0:" + ts + ":" + body).digest("hex");
  const res = await fetch(process.env.MINDWIRE_TEST_URL + c.webhookPath, { method: "POST", headers: { "Content-Type": "application/json", "X-Slack-Request-Timestamp": ts, "X-Slack-Signature": signature }, body });
  assert(res.status === 200, "webhook rejected: " + await res.text());
};
await post("event-1"); await post("event-1");
let receipts = [];
for (let i = 0; i < 200; i++) { receipts = await mw.integrations.events(c.id); if (receipts[0]?.state === "delivered") break; await Bun.sleep(25); }
assert(receipts.length === 1 && receipts[0].state === "delivered", "delivery did not finish: " + JSON.stringify(receipts));
const task = await mw.bots.task(c.botId, receipts[0].runId);
assert(task.run.status === "done" && task.chatId === receipts[0].chatId, "native task not visible through SDK");
const channels = await mw.apps.channels();
assert(channels.some(x => x.id === "integration:" + c.id), "connection absent from app notification destinations");
const app = await mw.apps.register({ name: "Report automation", projectId: "project", automation: true, url: "", icon: "mint-pebble" });
const grant = await mw.apps.notifications(app.app.id, { channelIds: ["integration:" + c.id] });
const credential = await Bun.file(grant.credentialFile).json();
assert(!JSON.stringify(credential).includes("slack-fixture-token"), "app obtained provider credentials");
const notify = () => fetch(credential.endpoint, { method: "POST", headers: { "Content-Type": "application/json", Authorization: "Bearer " + credential.token }, body: JSON.stringify({ eventId: "report-1", title: "Report ready", body: "Your report is available" }) });
assert((await notify()).status === 200 && (await notify()).status === 200, "scoped app notification failed");
const updated = await mw.integrations.update(c.id, { ...c, enabled: false, expectedRevision: c.revision });
assert(updated.revision > c.revision && updated.hasCredentials, "update did not preserve credentials");
await mw.integrations.delete(c.id);
assert((await mw.integrations.list()).length === 0, "disconnect failed");
const host = await mw.integrations.create({ name:"OpenShip commands",provider:"slack",botId:"",dispatch:"host",mode:"commands",enabled:true,channelIds:["C100"],workspaceId:"T100",command:"/openship",notify:false,credentials:{botToken:"slack-fixture-token",signingSecret:"slack-fixture-signing"} });
const raw=JSON.stringify({type:"event_callback",team_id:"T100",event_id:"host-1",event:{type:"message",channel:"C100",user:"U100",text:"/openship projects",ts:"22"}});
const timestamp=String(Math.floor(Date.now()/1000));
const headers={"Content-Type":"application/json","X-Slack-Request-Timestamp":timestamp,"X-Slack-Signature":"v0="+createHmac("sha256","slack-fixture-signing").update("v0:"+timestamp+":"+raw).digest("hex")};
assert((await mw.integrations.forwardWebhook(host.id,raw,{})).status===401,"relay trusted an unsigned event");
assert((await mw.integrations.forwardWebhook(host.id,raw,headers)).status===200,"relay rejected signed raw bytes");
assert((await mw.integrations.forwardWebhook(host.id,raw,headers)).status===200,"duplicate webhook failed");
const claimed=await mw.integrations.claim(host.id);
assert(claimed?.userId==="U100"&&claimed.text==="/openship projects","host did not receive verified event");
assert(await mw.integrations.claim(host.id)===null,"leased event was claimed twice");
await mw.integrations.complete(host.id,claimed.id,claimed.lease,"Your projects: Example");
await mw.integrations.complete(host.id,claimed.id,claimed.lease,"Your projects: Example");
for(let i=0;i<200;i++){ const rows=await mw.integrations.events(host.id); if(rows[0]?.state==="delivered")break;await Bun.sleep(25);}
assert((await mw.integrations.events(host.id))[0]?.state==="delivered","host reply did not reach provider");
await mw.integrations.delete(host.id);

`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bun, "-e", script)
	cmd.Env = append(os.Environ(), "MINDWIRE_TEST_SDK="+sdk, "MINDWIRE_TEST_URL="+server.URL, "MINDWIRE_TEST_BOT="+bot.ID)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SDK end-to-end: %v\n%s", err, output)
	}
	if turns.Load() != 1 || deliveries.Load() != 4 {
		t.Fatalf("expected one deduplicated turn and provider deliveries: turns=%d deliveries=%d", turns.Load(), deliveries.Load())
	}
	for _, endpoint := range []string{"/integrations/id/claim", "/integrations/id/forward", "/integrations/id/events/event/complete"} {
		if got := serve(t, root, "POST", endpoint, "{}"); got.Code != 401 {
			t.Fatal("host endpoint bypassed auth", endpoint)
		}
	}
	if got := serve(t, root, "GET", "/integrations", ""); got.Code != 401 {
		t.Fatal("management endpoint bypassed owner auth")
	}
}

type messagingTransport struct{ target string }

func (t messagingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	copy.URL = &u
	parts := strings.SplitN(t.target, "://", 2)
	copy.URL.Scheme = parts[0]
	copy.URL.Host = parts[1]
	return http.DefaultTransport.RoundTrip(copy)
}
