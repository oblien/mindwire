package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// namespacedCreds mimics the orchestrator's CredView: it returns the agent's settings with
// the "<type>:" prefix already stripped (bare keys), plus a secret the schema doesn't declare.
type namespacedCreds struct{ vals map[string]string }

func (c namespacedCreds) Get(k string) string    { return c.vals[k] }
func (c namespacedCreds) Set(k, v string) error  { c.vals[k] = v; return nil }
func (c namespacedCreds) All() map[string]string { return c.vals }

type stubAuth struct{}

func (stubAuth) Methods() []agent.AuthMethod { return nil }
func (stubAuth) Begin(context.Context, string) (agent.AuthState, error) {
	return agent.AuthState{}, nil
}
func (stubAuth) Step(context.Context, map[string]string) (agent.AuthState, error) {
	return agent.AuthState{}, nil
}
func (stubAuth) Status(context.Context) agent.AuthStatus { return agent.AuthStatus{} }
func (stubAuth) EnvForRun() map[string]string            { return map[string]string{"SECRET_ENV": "x"} }

// stubAdapter declares one non-secret setting ("model") and one secret ("apiKey"), and
// captures the TurnInput it's run with.
type stubAdapter struct{ got agent.TurnInput }

func (*stubAdapter) ID() string                       { return "stub" }
func (*stubAdapter) Meta() agent.CatalogEntry         { return agent.CatalogEntry{ID: "stub", Name: "Stub"} }
func (*stubAdapter) Capabilities() agent.Capabilities { return agent.Capabilities{} }
func (*stubAdapter) Settings() agent.SettingsSchema {
	return agent.SettingsSchema{Sections: []agent.Section{{Title: "S", Fields: []agent.Field{
		{Key: "model", Type: agent.FieldText},
		{Key: "apiKey", Type: agent.FieldSecret},
	}}}}
}
func (*stubAdapter) InstallSteps() []agent.Step                          { return nil }
func (*stubAdapter) VersionCommand() string                              { return "true" }
func (*stubAdapter) ConfigPath() string                                  { return "" }
func (*stubAdapter) Auth(agent.CredStore) agent.AuthModule               { return stubAuth{} }
func (*stubAdapter) History(agent.HistoryQuery) ([]agent.Message, error) { return nil, nil }
func (*stubAdapter) Notifications() agent.NotificationSpec               { return agent.NotificationSpec{} }
func (*stubAdapter) Doctor(context.Context) []agent.Check                { return nil }
func (a *stubAdapter) RunStream(_ context.Context, in agent.TurnInput, _ agent.Emit) (agent.TurnResult, error) {
	a.got = in
	return agent.TurnResult{Text: "ok", SessionID: "sid-1"}, nil
}

// actionAdapter emits a tool_use carrying an input-only action, then a tool_result carrying the
// completed action (with stdout + exit code) for the same id.
type actionAdapter struct{ *stubAdapter }

func (actionAdapter) RunStream(_ context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	emit(agent.Event{Type: agent.EventToolUse, Tool: &agent.ToolEvent{ID: "t1", Name: "Bash",
		Action: &agent.ToolAction{Kind: agent.KindShell, Shell: &agent.ShellCommand{Command: "ls"}}}})
	ec := 0
	emit(agent.Event{Type: agent.EventToolResult, Tool: &agent.ToolEvent{ID: "t1", Output: "file.txt",
		Action: &agent.ToolAction{Kind: agent.KindShell, Shell: &agent.ShellCommand{Command: "ls", Stdout: "file.txt", ExitCode: &ec}}}})
	return agent.TurnResult{Text: "ok", SessionID: "sid-a"}, nil
}

// The accumulator must fold the completed action (with stdout/exit) onto the tool part, replacing the
// input-only one carried by the tool_use.
func TestRunTurnFoldsToolAction(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ad := actionAdapter{&stubAdapter{}}
	r := New(store, ad, stubAuth{}, namespacedCreds{vals: map[string]string{}}, stream.New(), "/work")

	_, parts := r.RunTurn(context.Background(), Turn{ChatID: "c", Message: "hi", RunID: "run-a"})

	var tp *agent.ToolPart
	for i := range parts {
		if parts[i].Tool != nil && parts[i].Tool.ID == "t1" {
			tp = parts[i].Tool
		}
	}
	if tp == nil || tp.Action == nil || tp.Action.Shell == nil {
		t.Fatalf("tool part action = %+v", tp)
	}
	if tp.Output != "file.txt" || tp.Action.Shell.Stdout != "file.txt" {
		t.Errorf("completed action not folded onto the part: %+v", tp.Action.Shell)
	}
	if tp.Action.Shell.ExitCode == nil || *tp.Action.Shell.ExitCode != 0 {
		t.Errorf("folded action should carry the exit code, got %v", tp.Action.Shell.ExitCode)
	}
}

// TestRunTurnAppliesNamespacedSettings is the regression test for the multi-adapter config
// bug: the runner must pass the agent's OWN settings (bare keys, prefix stripped), filtered
// to declared non-secret keys — never the raw namespaced store, and never secrets in Config.
func TestRunTurnAppliesNamespacedSettings(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ad := &stubAdapter{}
	creds := namespacedCreds{vals: map[string]string{
		"model":   "opus",   // declared non-secret setting → should reach Config
		"apiKey":  "sk-xxx", // declared secret → must NOT reach Config (env only)
		"unknown": "leak",   // not in the schema → must be filtered out
	}}
	r := New(store, ad, stubAuth{}, creds, stream.New(), "/work")

	res, _ := r.RunTurn(context.Background(), Turn{ChatID: "chat-1", Message: "hi", RunID: "run-1", CWD: "/proj"})
	if res.Text != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}

	if got := ad.got.Config["model"]; got != "opus" {
		t.Errorf("Config[model] = %q, want opus (namespaced setting not applied)", got)
	}
	if _, ok := ad.got.Config["apiKey"]; ok {
		t.Errorf("secret apiKey leaked into TurnInput.Config: %v", ad.got.Config)
	}
	if _, ok := ad.got.Config["unknown"]; ok {
		t.Errorf("undeclared key leaked into TurnInput.Config: %v", ad.got.Config)
	}
	if ad.got.CWD != "/proj" {
		t.Errorf("CWD = %q, want /proj", ad.got.CWD)
	}
	// Session id must be persisted per (agent, chat) and round-trip.
	if sid := store.Session("stub", "chat-1"); sid != "sid-1" {
		t.Errorf("session not persisted per-agent: got %q", sid)
	}
	// The chat's cwd is recorded so native history can find the transcript later.
	if cwd := store.ChatCWD("chat-1"); cwd != "/proj" {
		t.Errorf("chat cwd = %q, want /proj", cwd)
	}
}

// TestRunTurnConsumesForkPending: a chat with a fork-pending marker (seeded by ForkChat) must have its
// FIRST turn forced to ForkOnResume — so a forked chat branches the native session instead of continuing
// (and polluting) the source's — and the marker must be consumed so the SECOND turn does not fork.
func TestRunTurnConsumesForkPending(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Seed a stub session on a source chat, then fork it: ForkChat records the pending marker for
	// ("stub", "dst") and seeds its session from the source.
	if err := store.SetSession("stub", "src", "sid-src"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := store.ForkChat("src", "dst"); err != nil {
		t.Fatalf("fork: %v", err)
	}

	ad := &stubAdapter{}
	r := New(store, ad, stubAuth{}, namespacedCreds{vals: map[string]string{}}, stream.New(), "/work")

	// First turn: the runner must force ForkOnResume (client passed none).
	r.RunTurn(context.Background(), Turn{ChatID: "dst", Message: "hi", RunID: "run-1"})
	if !ad.got.Options.ForkOnResume {
		t.Fatalf("first turn on a forked chat should force ForkOnResume")
	}

	// Second turn: the marker was consumed, so no forced fork.
	r.RunTurn(context.Background(), Turn{ChatID: "dst", Message: "again", RunID: "run-2"})
	if ad.got.Options.ForkOnResume {
		t.Fatalf("second turn should NOT fork — the pending marker must be consumed exactly once")
	}
}

// TestRunTurnPinsResolvedHistoryCWD asserts the history anchor is the FIRST turn's symlink-resolved
// directory (matching the slug Claude writes under) and that a later turn in a different directory does
// NOT move it (first-turn-wins) — while each turn still RUNS in its own requested cwd.
func TestRunTurnPinsResolvedHistoryCWD(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(t.TempDir()) // canonical (macOS /var → /private/var)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}
	ad := &stubAdapter{}
	r := New(store, ad, stubAuth{}, namespacedCreds{vals: map[string]string{}}, stream.New(), "/work")

	// First turn runs under the symlinked cwd; the anchor is recorded resolved (not the symlink path).
	r.RunTurn(context.Background(), Turn{ChatID: "c", Message: "hi", RunID: "r1", CWD: link})
	if got := store.ChatCWD("c"); got != real {
		t.Fatalf("history anchor = %q, want resolved %q", got, real)
	}
	if ad.got.CWD != link {
		t.Fatalf("first turn ran in %q, want the requested cwd %q", ad.got.CWD, link)
	}

	// Second turn runs elsewhere; the anchor must NOT move.
	other, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.RunTurn(context.Background(), Turn{ChatID: "c", Message: "again", RunID: "r2", CWD: other})
	if got := store.ChatCWD("c"); got != real {
		t.Fatalf("history anchor moved on second turn to %q, want stable %q", got, real)
	}
	if ad.got.CWD != other {
		t.Fatalf("second turn ran in %q, want %q", ad.got.CWD, other)
	}
}

// overrideSchema declares one unified field (canon "reasoningEffort" → key "effort") and one custom
// field (canon == key). No secret is declared — secrets never appear in a settings schema, which is
// exactly why a per-turn override can't reach one.
func overrideSchema() agent.SettingsSchema {
	return agent.SettingsSchema{Sections: []agent.Section{{
		Title: "General",
		Fields: []agent.Field{
			{Key: "effort", Label: "Effort", Type: agent.FieldSelect, Scope: agent.ScopeUnified, Canon: agent.CanonReasoningEffort},
			{Key: "flavor", Label: "Flavor", Type: agent.FieldText, Scope: agent.ScopeCustom, Canon: "flavor"},
		},
	}}}
}

func TestResolveSettingsOverridesWinAndFilter(t *testing.T) {
	sticky := map[string]string{"effort": "low", "flavor": "vanilla", "bogus": "x"}
	opts := agent.TurnOptions{Settings: map[string]string{
		agent.CanonReasoningEffort: "high", // canon → key "effort", overrides sticky
		"flavor":                   "mint", // custom canon == key
	}}
	got := resolveSettings(overrideSchema(), sticky, opts)

	if got["effort"] != "high" {
		t.Errorf("per-turn override should win: effort = %q, want high", got["effort"])
	}
	if got["flavor"] != "mint" {
		t.Errorf("custom override should apply: flavor = %q, want mint", got["flavor"])
	}
	if _, ok := got["bogus"]; ok {
		t.Errorf("undeclared sticky key must be filtered out, got %q", got["bogus"])
	}
}

// The security invariant: a per-turn override cannot introduce an undeclared key and cannot reach a
// secret. Secrets never appear in the settings schema, so a canon that doesn't resolve to a declared
// non-secret key is dropped — a client cannot smuggle e.g. an API key through options.settings.
func TestResolveSettingsDropsSecretsAndUnknowns(t *testing.T) {
	opts := agent.TurnOptions{Settings: map[string]string{
		"apiKey":         "sk-leak",       // a secret: not in the schema → dropped
		"oauthToken":     "oauth-leak",    // ditto
		"totallyMadeUp":  "x",             // unregistered canon → dropped
		agent.CanonModel: "not-declared",  // valid canon, but this schema has no model field → dropped
		"effort":         "raw-key-notok", // the raw KEY, not its canon → dropped (overrides are canon-addressed)
	}}
	got := resolveSettings(overrideSchema(), map[string]string{}, opts)

	if len(got) != 0 {
		t.Fatalf("no override should have survived, got %v", got)
	}
}
