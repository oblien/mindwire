package mindwire

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/api"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// The SDK is exercised against a scripted, test-only adapter registered here. Its behavior is driven
// ENTIRELY by the turn message (never instance state), so the single globally-registered instance is
// race-free even though every test shares it. register.go additionally pulls in the real claude/codex
// adapters, so the supervisor hosts all three — the fake is just an extra, deterministic agent.
func init() { agent.Register(fakeAdapter{}) }

type fakeAdapter struct{}

func (fakeAdapter) ID() string                            { return "fake" }
func (fakeAdapter) Meta() CatalogEntry                    { return CatalogEntry{ID: "fake", Name: "Fake"} }
func (fakeAdapter) Settings() SettingsSchema              { return SettingsSchema{} }
func (fakeAdapter) InstallSteps() []agent.Step            { return nil }
func (fakeAdapter) VersionCommand() string                { return "true" }
func (fakeAdapter) ConfigPath() string                    { return "" }
func (fakeAdapter) Notifications() NotificationSpec       { return NotificationSpec{} }
func (fakeAdapter) Doctor(context.Context) []Check        { return nil }
func (fakeAdapter) Auth(agent.CredStore) agent.AuthModule { return fakeAuth{} }

// Capabilities: history is native (to exercise the native-history path); cancel is on; the turn-option
// flags are all off so SystemPrompt exercises the 400 gate. Ingress caps stay off — the tests that
// need a run to stay in flight use the "hang" script, which needs no ingress.
func (fakeAdapter) Capabilities() Capabilities {
	return Capabilities{Protocol: ProtocolCLI, Output: OutputStructuredJSON, History: SupportNative, Cancel: true}
}

// History returns a canned native transcript only for "native-chat"; every other chat returns empty so
// Messages falls back to the recorded log — letting one adapter exercise both Messages paths.
func (fakeAdapter) History(q HistoryQuery) ([]Message, error) {
	if q.ChatID == "native-chat" {
		return []Message{{ID: "n1", ChatID: "native-chat", Role: "user", Text: "native hello"}}, nil
	}
	return nil, nil
}

// RunStream is scripted by the message: "hang" blocks until cancelled, "fail" errors, anything else
// emits a text→tool_use→tool_result→result sequence and completes cleanly.
func (fakeAdapter) RunStream(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	switch in.Message {
	case "hang":
		<-ctx.Done()
		return agent.TurnResult{IsError: true, Text: "cancelled"}, nil
	case "fail":
		return agent.TurnResult{IsError: true, Text: "boom"}, nil
	default:
		emit(Event{Type: EventText, Text: "hello"})
		emit(Event{Type: EventToolUse, Tool: &ToolEvent{ID: "t1", Name: "read"}})
		emit(Event{Type: EventToolResult, Tool: &ToolEvent{ID: "t1", Output: "data"}})
		emit(Event{Type: EventResult, Result: &ResultInfo{Text: "hello"}})
		return agent.TurnResult{Text: "hello", SessionID: "sess-1"}, nil
	}
}

type fakeAuth struct{}

func (fakeAuth) Methods() []AuthMethod { return []AuthMethod{{ID: "apiKey", Label: "API key"}} }
func (fakeAuth) Begin(context.Context, string) (AuthState, error) {
	return AuthState{Method: "apiKey", Status: "complete"}, nil
}
func (fakeAuth) Step(context.Context, map[string]string) (AuthState, error) {
	return AuthState{Status: "complete"}, nil
}
func (fakeAuth) Status(context.Context) AuthStatus {
	return AuthStatus{Configured: true, Method: "apiKey"}
}
func (fakeAuth) EnvForRun() map[string]string { return nil }

// capturingNotifier records every notification the fan-out delivers to it.
type capturingNotifier struct {
	mu  sync.Mutex
	got []Notification
}

func (c *capturingNotifier) Notify(_ context.Context, n Notification) error {
	c.mu.Lock()
	c.got = append(c.got, n)
	c.mu.Unlock()
	return nil
}
func (c *capturingNotifier) all() []Notification {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Notification(nil), c.got...)
}

func newFakeClient(t *testing.T, n Notifier) *Client {
	t.Helper()
	dir := t.TempDir()
	c, err := New(Options{Agent: "fake", CWD: dir, StatePath: filepath.Join(dir, "state.json"), Notifier: n})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func containsSubsequence[T comparable](hay, needle []T) bool {
	i := 0
	for _, h := range hay {
		if i < len(needle) && h == needle[i] {
			i++
		}
	}
	return i == len(needle)
}

// TestStreamOrderAndWait asserts Stream yields the adapter's events in order (replay+live coalesced by
// the hub) and Wait reports the terminal record + captured result.
func TestStreamOrderAndWait(t *testing.T) {
	c := newFakeClient(t, nil)
	ctx := context.Background()

	run, err := c.Turn(ctx, TurnRequest{ChatID: "c1", Message: "script"})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}

	var types []EventType
	for ev := range run.Stream(ctx) {
		types = append(types, ev.Type)
	}
	want := []EventType{EventText, EventToolUse, EventToolResult, EventResult}
	if !containsSubsequence(types, want) {
		t.Fatalf("stream order: got %v, want subsequence %v", types, want)
	}

	res, err := run.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Run.Status != "done" {
		t.Fatalf("status = %q, want done", res.Run.Status)
	}
	if res.Result == nil || res.Result.Text != "hello" {
		t.Fatalf("result = %+v, want text=hello", res.Result)
	}
}

// TestWithOpenSentinel asserts the opt-in synthetic open frame is prepended.
func TestWithOpenSentinel(t *testing.T) {
	c := newFakeClient(t, nil)
	ctx := context.Background()
	run, err := c.Turn(ctx, TurnRequest{ChatID: "c1", Message: "script"})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	first := true
	for ev := range run.Stream(ctx, WithOpenSentinel()) {
		if first {
			if ev.Type != EventStatus || ev.Meta["stream"] != "open" {
				t.Fatalf("first event = %+v, want status/open sentinel", ev)
			}
			first = false
		}
	}
	if first {
		t.Fatal("stream yielded no events")
	}
}

// TestTurnConflict asserts the one-turn-per-chat gate returns APIError{409}. active[chatID] is set
// synchronously in StartTurn before Turn returns, so this is race-free.
func TestTurnConflict(t *testing.T) {
	c := newFakeClient(t, nil)
	ctx := context.Background()

	run1, err := c.Turn(ctx, TurnRequest{ChatID: "c1", Message: "hang"})
	if err != nil {
		t.Fatalf("first Turn: %v", err)
	}
	defer func() { _ = run1.Cancel() }()

	_, err = c.Turn(ctx, TurnRequest{ChatID: "c1", Message: "script"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("second Turn: want APIError 409, got %v", err)
	}
}

// TestCloseDrainsInFlight asserts Close cancels an in-flight turn AND blocks until that turn's
// terminal persist has landed — so an embedder can tear down the state dir/working directory the
// instant Close returns without racing a late SaveRun. Regression guard for the drain race: before
// Close drained, a just-cancelled turn's async SaveRun could outlive Close and write into a dir being
// removed. The deterministic signal is that the run is already persisted non-"running" post-Close.
func TestCloseDrainsInFlight(t *testing.T) {
	c := newFakeClient(t, nil)

	run, err := c.Turn(context.Background(), TurnRequest{ChatID: "c1", Message: "hang"})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close has returned; the drain guarantees the run goroutine (and its SaveRun) finished first.
	rec, ok := c.core.store.GetRun(run.ID())
	if !ok {
		t.Fatalf("run %s missing after Close", run.ID())
	}
	if rec.Status == "running" {
		t.Fatal("run still 'running' after Close — drain did not wait for the terminal persist")
	}
	if rec.Status != "cancelled" {
		t.Fatalf("run status = %q after Close, want cancelled", rec.Status)
	}

	// Close is idempotent: a second call is a cheap no-op and never blocks.
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestTurnOptionGate asserts an unsupported per-turn option returns APIError{400}.
func TestTurnOptionGate(t *testing.T) {
	c := newFakeClient(t, nil)
	_, err := c.Turn(context.Background(), TurnRequest{
		ChatID: "c1", Message: "script", Options: TurnOptions{SystemPrompt: "you are a bot"},
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("want APIError 400, got %v", err)
	}
}

// TestWaitRunFailed asserts a non-"done" terminal state yields *RunFailedError, and NoErrorOnFailure
// suppresses it while still returning the record.
func TestWaitRunFailed(t *testing.T) {
	c := newFakeClient(t, nil)
	ctx := context.Background()

	run, err := c.Turn(ctx, TurnRequest{ChatID: "c1", Message: "fail"})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	res, err := run.Wait(ctx)
	var rfe *RunFailedError
	if !errors.As(err, &rfe) {
		t.Fatalf("want RunFailedError, got %v", err)
	}
	if rfe.Status != "error" || rfe.Detail != "boom" {
		t.Fatalf("RunFailedError = %+v, want status=error detail=boom", rfe)
	}
	if res.Run.Status != "error" {
		t.Fatalf("res status = %q, want error", res.Run.Status)
	}

	run2, err := c.Turn(ctx, TurnRequest{ChatID: "c2", Message: "fail"})
	if err != nil {
		t.Fatalf("second Turn: %v", err)
	}
	res2, err := run2.Wait(ctx, NoErrorOnFailure())
	if err != nil {
		t.Fatalf("NoErrorOnFailure: want nil error, got %v", err)
	}
	if res2.Run.Status != "error" {
		t.Fatalf("res2 status = %q, want error", res2.Run.Status)
	}
}

// TestMessages asserts native history is served when present and the recorded log otherwise.
func TestMessages(t *testing.T) {
	c := newFakeClient(t, nil)
	ctx := context.Background()

	native, err := c.Messages("native-chat", MessagesOptions{})
	if err != nil {
		t.Fatalf("Messages(native): %v", err)
	}
	if len(native) != 1 || native[0].Text != "native hello" {
		t.Fatalf("native messages = %+v", native)
	}

	run, err := c.Turn(ctx, TurnRequest{ChatID: "rec-chat", Message: "script"})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if _, err := run.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	rec, err := c.Messages("rec-chat", MessagesOptions{})
	if err != nil {
		t.Fatalf("Messages(rec): %v", err)
	}
	if len(rec) != 2 || rec[0].Role != "user" || rec[1].Role != "assistant" {
		t.Fatalf("recorded messages = %+v, want [user, assistant]", rec)
	}
}

// TestChatLifecycleSDK exercises RenameChat/ForkChat/DeleteChat through the Go SDK: rename precedence
// (user title wins, trimmed), fork seeds the session mapping and maps errors to the right statuses, and
// delete purges bookkeeping (the fake agent implements no HistoryDeleter, so nothing is natively purged).
func TestChatLifecycleSDK(t *testing.T) {
	c := newFakeClient(t, nil)
	st := c.core.store

	// Rename: the trimmed user title wins over the derived first-message snippet in the listing.
	if err := st.AddMessage(session.Message{ID: "m1", ChatID: "c1", Role: "user", Text: "derived snippet", CreatedAt: "t1"}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	sum, err := c.RenameChat("c1", "  Renamed  ")
	if err != nil {
		t.Fatalf("RenameChat: %v", err)
	}
	if sum.Title != "Renamed" {
		t.Fatalf("rename title = %q, want trimmed 'Renamed'", sum.Title)
	}
	if chats := c.Chats(); len(chats) != 1 || chats[0].Title != "Renamed" {
		t.Fatalf("Chats title = %+v, want user title to win", chats)
	}

	// Fork: seeds the source's session mapping into the new id (shared native history until it branches).
	if err := st.SetSession("fake", "src", "sess-src"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	fsum, err := c.ForkChat("src", "dst")
	if err != nil {
		t.Fatalf("ForkChat: %v", err)
	}
	if fsum.ChatID != "dst" {
		t.Fatalf("fork summary chatId = %q, want dst", fsum.ChatID)
	}
	if st.Session("fake", "dst") != "sess-src" {
		t.Fatalf("fork did not seed session mapping: %q", st.Session("fake", "dst"))
	}

	// Fork error mapping: unknown source → 404, existing target → 400.
	if _, err := c.ForkChat("ghost", "x"); !isAPIErr(err, http.StatusNotFound) {
		t.Fatalf("fork unknown source err = %v, want APIError 404", err)
	}
	if _, err := c.ForkChat("src", "dst"); !isAPIErr(err, http.StatusBadRequest) {
		t.Fatalf("fork existing target err = %v, want APIError 400", err)
	}

	// Delete: purges the bookkeeping; the fake agent has no HistoryDeleter, so no native purge.
	res, err := c.DeleteChat("dst")
	if err != nil {
		t.Fatalf("DeleteChat: %v", err)
	}
	if !res.Deleted || res.Sessions != 1 {
		t.Fatalf("delete result = %+v, want deleted=true sessions=1", res)
	}
	if len(res.NativePurged) != 0 || len(res.NativeFailed) != 0 {
		t.Fatalf("delete native fields = %+v/%+v, want none (fake keeps no transcript)", res.NativePurged, res.NativeFailed)
	}
	if st.Session("fake", "dst") != "" {
		t.Fatalf("session mapping survived delete")
	}
}

func isAPIErr(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == status
}

// TestMCPSDK exercises the Client.MCP sub-API against the real codex adapter (user-scope config.toml in a
// temp CODEX_HOME): Set→List→Get→Delete round-trips, an empty scope defaults to user, a missing server is
// APIError{404}, and the fake agent — which implements no MCPServerModule — is APIError{400}. This is the
// SDK analogue of the /mcp HTTP test; the on-disk TOML fidelity is covered by codex/mcp_test.go.
func TestMCPSDK(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	c := newFakeClient(t, nil)
	cx := c.WithAgent("codex")

	// Set a server (empty scope → user default), then it appears in List keyed under the user scope.
	if _, err := cx.MCP.Set("", "", "local", MCPServer{Command: "srv", Args: []string{"--x"}}); err != nil {
		t.Fatalf("MCP.Set: %v", err)
	}
	all, err := cx.MCP.List("")
	if err != nil {
		t.Fatalf("MCP.List: %v", err)
	}
	if all[MemoryUser]["local"].Command != "srv" {
		t.Fatalf("MCP.List user scope = %+v, want local server", all[MemoryUser])
	}

	// Get one (empty scope → user); a missing server is APIError{404}.
	got, err := cx.MCP.Get("", "", "local")
	if err != nil {
		t.Fatalf("MCP.Get: %v", err)
	}
	if got.Command != "srv" || len(got.Args) != 1 {
		t.Fatalf("MCP.Get = %+v", got)
	}
	if _, err := cx.MCP.Get("", "", "nope"); !isAPIErr(err, http.StatusNotFound) {
		t.Fatalf("MCP.Get(nope) err = %v, want APIError 404", err)
	}

	// Delete removes it (idempotent), and Get afterward is 404.
	if err := cx.MCP.Delete("", "", "local"); err != nil {
		t.Fatalf("MCP.Delete: %v", err)
	}
	if err := cx.MCP.Delete("", "", "local"); err != nil {
		t.Fatalf("MCP.Delete (idempotent): %v", err)
	}
	if _, err := cx.MCP.Get("", "", "local"); !isAPIErr(err, http.StatusNotFound) {
		t.Fatalf("MCP.Get after delete = %v, want APIError 404", err)
	}

	// The fake agent implements no MCPServerModule → the capability gate is APIError{400}.
	if _, err := c.MCP.List(""); !isAPIErr(err, http.StatusBadRequest) {
		t.Fatalf("MCP.List(fake) err = %v, want APIError 400", err)
	}
}

// TestProvidersSDK exercises the Client.Providers sub-API against the real codex adapter (user-scope
// config.toml in a temp CODEX_HOME): Set→List→Get→Delete round-trips, the key is stored but never
// echoed (only HasKey), an empty scope defaults to user, a missing provider is APIError{404}, and the
// fake agent — which implements no CustomProvidersModule — is APIError{400}. The on-disk TOML fidelity
// is covered by codex/providers_test.go.
func TestProvidersSDK(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	c := newFakeClient(t, nil)
	cx := c.WithAgent("codex")

	// Register a provider WITH a key (empty scope → user default). The echo carries HasKey but never the
	// key itself, and the derived EnvVar is present.
	set, err := cx.Providers.Set("", "", "my-llm", CustomProvider{
		Name: "My LLM", BaseURL: "https://llm.example/v1", Models: []string{"m-large", "m-small"},
	}, "sk-secret-123", nil)
	if err != nil {
		t.Fatalf("Providers.Set: %v", err)
	}
	if !set.HasKey || set.EnvVar != "MY_LLM_API_KEY" || set.BaseURL != "https://llm.example/v1" {
		t.Fatalf("Providers.Set echo = %+v, want hasKey + derived env var", set)
	}

	all, err := cx.Providers.List("")
	if err != nil {
		t.Fatalf("Providers.List: %v", err)
	}
	got := all[MemoryUser]["my-llm"]
	if got.BaseURL != "https://llm.example/v1" || len(got.Models) != 2 || !got.HasKey {
		t.Fatalf("Providers.List user scope = %+v, want the registered provider", got)
	}

	// Get one (empty scope → user); a missing provider is APIError{404}.
	one, err := cx.Providers.Get("", "", "my-llm")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}
	if one.Name != "My LLM" || one.EnvVar != "MY_LLM_API_KEY" {
		t.Fatalf("Providers.Get = %+v", one)
	}
	if _, err := cx.Providers.Get("", "", "nope"); !isAPIErr(err, http.StatusNotFound) {
		t.Fatalf("Providers.Get(nope) err = %v, want APIError 404", err)
	}

	// A metadata-only update (empty apiKey) keeps the stored key.
	if _, err := cx.Providers.Set("", "", "my-llm", CustomProvider{BaseURL: "https://llm.example/v2"}, "", nil); err != nil {
		t.Fatalf("Providers.Set (update): %v", err)
	}
	if upd, _ := cx.Providers.Get("", "", "my-llm"); !upd.HasKey || upd.BaseURL != "https://llm.example/v2" {
		t.Fatalf("Providers.Set metadata-only lost the key or URL: %+v", upd)
	}

	// Delete removes it (idempotent), and Get afterward is 404.
	if err := cx.Providers.Delete("", "", "my-llm"); err != nil {
		t.Fatalf("Providers.Delete: %v", err)
	}
	if err := cx.Providers.Delete("", "", "my-llm"); err != nil {
		t.Fatalf("Providers.Delete (idempotent): %v", err)
	}
	if _, err := cx.Providers.Get("", "", "my-llm"); !isAPIErr(err, http.StatusNotFound) {
		t.Fatalf("Providers.Get after delete = %v, want APIError 404", err)
	}

	// The fake agent implements no CustomProvidersModule → the capability gate is APIError{400}.
	if _, err := c.Providers.List(""); !isAPIErr(err, http.StatusBadRequest) {
		t.Fatalf("Providers.List(fake) err = %v, want APIError 400", err)
	}
}

// TestSetConfigCanon asserts the sticky config path accepts a CANONICAL key (reasoningEffort) and
// persists it under the real Codex adapter's raw key (reasoning-effort), mirroring the canon-addressed
// per-turn path — while a raw key still works (back-compat) and an unknown key is dropped.
func TestSetConfigCanon(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	c := newFakeClient(t, nil)
	cx := c.WithAgent("codex")

	// A canonical key resolves to the agent's raw key on write.
	if err := cx.SetConfig(map[string]string{"reasoningEffort": "high"}); err != nil {
		t.Fatalf("SetConfig(canon): %v", err)
	}
	got, err := cx.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got["reasoning-effort"] != "high" {
		t.Fatalf("canon setConfig persisted as %+v; want reasoning-effort=high", got)
	}

	// The raw key still writes directly (back-compat), and an unknown key is silently ignored.
	if err := cx.SetConfig(map[string]string{"reasoning-effort": "low", "bogusKey": "x"}); err != nil {
		t.Fatalf("SetConfig(raw): %v", err)
	}
	got, _ = cx.GetConfig()
	if got["reasoning-effort"] != "low" {
		t.Fatalf("raw setConfig persisted as %+v; want reasoning-effort=low", got)
	}
	if _, present := got["bogusKey"]; present {
		t.Fatalf("unknown key leaked into config: %+v", got)
	}
}

// TestNotifyFanout asserts a turn's terminal notification reaches an Options.Notifier.
func TestNotifyFanout(t *testing.T) {
	cap := &capturingNotifier{}
	c := newFakeClient(t, cap)
	ctx := context.Background()

	run, err := c.Turn(ctx, TurnRequest{ChatID: "c1", Message: "script"})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if _, err := run.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	found := false
	for _, n := range cap.all() {
		if n.Condition == Finished {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Finished notification captured, got %+v", cap.all())
	}
}

// TestUnknownAgent asserts a bad ForAgent yields APIError{400}.
func TestUnknownAgent(t *testing.T) {
	c := newFakeClient(t, nil)
	_, err := c.Doctor(context.Background(), ForAgent("nope"))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("want APIError 400, got %v", err)
	}
}

// TestSDKRouteParity asserts every HTTP route the daemon serves has a corresponding SDK method (and no
// coverage entry is stale) — the Go analogue of the OpenAPI parity test, so the SDK surface can't
// silently drift from the wire surface.
func TestSDKRouteParity(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sup := orchestrator.New(store, stream.New(), notify.Noop{}, dir, "fake")
	routes := api.New(store, stream.New(), sup).Routes()

	// Each HTTP route → the SDK method covering it. A new route with no entry (or an entry naming a
	// route that no longer exists) fails the test.
	coverage := map[string]string{
		"POST /turns":                         "Client.Turn",
		"GET /runs/{id}":                      "Client.Run",
		"GET /runs/{id}/children":             "Client.Children",
		"POST /runs/{id}/cancel":              "Run.Cancel",
		"POST /runs/{id}/respond":             "Run.Respond",
		"POST /runs/{id}/input":               "Run.SendInput",
		"POST /runs/{id}/interrupt":           "Run.Interrupt",
		"POST /runs/{id}/set-model":           "Run.SetModel",
		"POST /runs/{id}/set-permission-mode": "Run.SetPermissionMode",
		"GET /runs/{id}/stream":               "Run.Stream",
		"GET /doctor":                         "Client.Doctor",
		"GET /chats":                          "Client.Chats",
		"PUT /chats/{id}":                     "Client.RenameChat",
		"DELETE /chats/{id}":                  "Client.DeleteChat",
		"POST /chats/{id}/fork":               "Client.ForkChat",
		"POST /chats/{id}/compact":            "Client.Compact",
		"GET /chats/{id}/messages":            "Client.Messages",
		"GET /chats/{id}/run":                 "Client.LatestRun",
		"GET /catalog":                        "Client.Catalog",
		"GET /agent":                          "Client.Agent",
		"GET /models":                         "Client.Models",
		"POST /setup":                         "Client.Setup",
		"POST /update":                        "Client.Update",
		"GET /setup":                          "Client.SetupStatus",
		"GET /config":                         "Client.GetConfig",
		"PUT /config":                         "Client.SetConfig",
		"GET /memory":                         "Prompts.Memory",
		"PUT /memory":                         "Prompts.SetMemory",
		"DELETE /memory":                      "Prompts.DeleteMemory",
		"GET /prompts":                        "Prompts.List",
		"GET /prompts/{name}":                 "Prompts.Get",
		"PUT /prompts/{name}":                 "Prompts.Set",
		"DELETE /prompts/{name}":              "Prompts.Delete",
		"GET /subagents":                      "Prompts.Subagents",
		"GET /subagents/{name}":               "Prompts.Subagent",
		"PUT /subagents/{name}":               "Prompts.SetSubagent",
		"DELETE /subagents/{name}":            "Prompts.DeleteSubagent",
		"GET /mcp":                            "MCP.List",
		"GET /mcp/{name}":                     "MCP.Get",
		"PUT /mcp/{name}":                     "MCP.Set",
		"DELETE /mcp/{name}":                  "MCP.Delete",
		"GET /providers":                      "Providers.List",
		"GET /providers/{id}":                 "Providers.Get",
		"PUT /providers/{id}":                 "Providers.Set",
		"DELETE /providers/{id}":              "Providers.Delete",
		"GET /auth/methods":                   "Auth.Methods",
		"POST /auth/begin":                    "Auth.Begin",
		"POST /auth/step":                     "Auth.Step",
		"GET /auth/status":                    "Auth.Status",
		"PUT /notify/config":                  "Client.SetNotifyConfig",
		"GET /notify/config":                  "Client.GetNotifyConfig",
		"GET /notify/stream":                  "Client.Notifications",
		"GET /notify/channels":                "Client.NotifyChannels",
		"POST /notify/channels":               "Client.SetNotifyChannel",
		"PUT /notify/channels/{id}":           "Client.SetNotifyChannel",
		"DELETE /notify/channels/{id}":        "Client.DeleteNotifyChannel",
		"POST /notify/channels/{id}/test":     "Client.TestNotifyChannel",
		"GET /notify/rules":                   "Client.NotifyRules",
		"POST /notify/rules":                  "Client.SetNotifyRule",
		"PUT /notify/rules/{id}":              "Client.SetNotifyRule",
		"DELETE /notify/rules/{id}":           "Client.DeleteNotifyRule",
		"GET /healthz":                        "Client.Health",
		"GET /stats":                          "Client.Stats",
		"GET /processes/stream":               "Client.Processes",
	}

	actual := map[string]bool{}
	for _, rt := range routes {
		actual[rt.Method+" "+rt.Pattern] = true
	}
	for _, rt := range api.PublicRoutes {
		actual[rt.Method+" "+rt.Pattern] = true
	}

	for key := range actual {
		if _, ok := coverage[key]; !ok {
			t.Errorf("HTTP route %q has no SDK coverage entry — add the SDK method + coverage note", key)
		}
	}
	for key := range coverage {
		if !actual[key] {
			t.Errorf("coverage lists %q but no such HTTP route exists — stale entry", key)
		}
	}
}

// TestLiveClaude runs a real claude-code turn end-to-end, but only when claude is authenticated on the
// host — otherwise it skips (like the repo's other live-gated tests). Never runs in unconfigured CI.
func TestLiveClaude(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Options{Agent: "claude-code", CWD: dir, StatePath: filepath.Join(dir, "state.json")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()

	st, err := c.Auth.Status(ctx)
	if err != nil {
		t.Fatalf("Auth.Status: %v", err)
	}
	if !st.Configured {
		t.Skip("claude-code not authenticated; skipping live test")
	}

	run, err := c.Turn(ctx, TurnRequest{ChatID: "live-1", Message: "Reply with exactly: pong"})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	res, err := run.Wait(ctx)
	if err != nil {
		t.Fatalf("live turn failed: %v", err)
	}
	if res.Run.Status != "done" {
		t.Fatalf("status = %q, want done", res.Run.Status)
	}
}
