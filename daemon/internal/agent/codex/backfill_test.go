package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// History resolves a session id to its rollout file (via findRollout) and maps it to unified messages.
// It searches the live sessions tree first and the archived_sessions tree second; an empty id or a
// missing file returns nil so the API falls back to mindwire's own recorded stream.
func TestHistoryResolvesRollout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	// A minimal two-message rollout (user turn + agent reply) under the LIVE sessions tree.
	live := filepath.Join(home, "sessions", "2026", "07")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := "" +
		`{"timestamp":"t0","type":"event_msg","payload":{"type":"user_message","message":"hi there"}}` + "\n" +
		`{"timestamp":"t1","type":"event_msg","payload":{"type":"agent_message","message":"hello back"}}` + "\n"
	if err := os.WriteFile(filepath.Join(live, "rollout-2026-07-31T00-00-00-sess-live.jsonl"), []byte(rollout), 0o600); err != nil {
		t.Fatal(err)
	}

	msgs, err := adapter{}.History(agent.HistoryQuery{SessionID: "sess-live", ChatID: "c1"})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[0].Text != "hi there" || msgs[1].Role != "assistant" {
		t.Fatalf("history = %+v, want a user+assistant pair", msgs)
	}
	if msgs[0].ChatID != "c1" {
		t.Errorf("chatID not threaded through: %+v", msgs[0])
	}

	// A rollout only in the ARCHIVED tree is still found (second search root).
	arch := filepath.Join(home, "archived_sessions")
	if err := os.MkdirAll(arch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(arch, "rollout-2026-01-01T00-00-00-sess-arch.jsonl"),
		[]byte(`{"timestamp":"t0","type":"event_msg","payload":{"type":"user_message","message":"archived q"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archived, _ := adapter{}.History(agent.HistoryQuery{SessionID: "sess-arch"})
	if len(archived) != 1 || archived[0].Text != "archived q" {
		t.Errorf("archived rollout not resolved: %+v", archived)
	}

	// Empty session id and an unknown id both fall back to nil (no rollout on disk).
	empty, _ := adapter{}.History(agent.HistoryQuery{SessionID: ""})
	if empty != nil {
		t.Errorf("empty session id should return nil, got %+v", empty)
	}
	missing, _ := adapter{}.History(agent.HistoryQuery{SessionID: "does-not-exist"})
	if missing != nil {
		t.Errorf("missing rollout should return nil, got %+v", missing)
	}
	if p := findRollout(home, "does-not-exist"); p != "" {
		t.Errorf("findRollout should return \"\" for a missing id, got %q", p)
	}
}

// isImage classifies an attachment as a Codex `-i` image by mime prefix or by extension (on either the
// path or the name), and treats everything else as a non-image reference.
func TestIsImage(t *testing.T) {
	cases := []struct {
		name string
		at   agent.Attachment
		want bool
	}{
		{"mime prefix", agent.Attachment{Mime: "image/png"}, true},
		{"mime uppercased", agent.Attachment{Mime: "IMAGE/JPEG"}, true},
		{"png by path", agent.Attachment{Path: "/tmp/shot.PNG"}, true},
		{"svg by name", agent.Attachment{Name: "diagram.svg"}, true},
		{"webp by path", agent.Attachment{Path: "/x/y.webp"}, true},
		{"text file", agent.Attachment{Name: "notes.txt", Mime: "text/plain"}, false},
		{"no signal", agent.Attachment{}, false},
		{"pdf is not an image", agent.Attachment{Path: "/docs/spec.pdf"}, false},
	}
	for _, c := range cases {
		if got := isImage(c.at); got != c.want {
			t.Errorf("%s: isImage(%+v) = %v, want %v", c.name, c.at, got, c.want)
		}
	}
}

// materialize resolves attachments: images become -i paths, other files are referenced by path in an
// appended message block. Data-only attachments are written to a temp file first; cleanup removes them.
func TestMaterializeAttachments(t *testing.T) {
	img := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(img, []byte("\x89PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, msgAppend, cleanup, err := materialize(agent.TurnInput{Options: agent.TurnOptions{
		Attachments: []agent.Attachment{
			{Path: img, Mime: "image/png"},                // → imagePaths
			{Name: "log.txt", Data: []byte("some bytes")}, // data-only, non-image → temp file + ref
			{}, // no path, no data → skipped
		},
	}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	defer cleanup()

	if len(files.imagePaths) != 1 || files.imagePaths[0] != img {
		t.Errorf("imagePaths = %v, want [%s]", files.imagePaths, img)
	}
	if msgAppend == "" || !strings.Contains(msgAppend, "log.txt") {
		t.Errorf("msgAppend = %q, want it to reference the non-image attachment", msgAppend)
	}
}

// summarizeChanges renders a file_change item's edits as "<kind> <path>" lines, degrading to just the
// path when the kind is absent and to "" when there are no changes.
func TestSummarizeChanges(t *testing.T) {
	if got := summarizeChanges(nil); got != "" {
		t.Errorf("no changes → %q, want empty", got)
	}
	got := summarizeChanges([]normChange{
		{Kind: "modify", Path: "a.go"},
		{Path: "b.go"},   // no kind → path only
		{Kind: "delete"}, // no path → dropped
	})
	want := "modify a.go\nb.go"
	if got != want {
		t.Errorf("summarizeChanges = %q, want %q", got, want)
	}
}

// planInteraction maps an app-server turn/plan notification to a unified todos interaction, defaulting a
// blank status to "pending" and returning nil when the plan is empty or undecodable.
func TestPlanInteraction(t *testing.T) {
	params := json.RawMessage(`{"plan":[{"step":"read the code","status":"completed"},{"step":"write the test","status":""}]}`)
	it := planInteraction(params)
	if it == nil || it.Kind != "todos" || len(it.Items) != 2 {
		t.Fatalf("planInteraction = %+v, want a 2-item todos interaction", it)
	}
	if it.Items[0].Status != "completed" || it.Items[1].Status != "pending" {
		t.Errorf("statuses = %q/%q, want completed/pending (blank defaults to pending)", it.Items[0].Status, it.Items[1].Status)
	}
	if planInteraction(json.RawMessage(`{"plan":[]}`)) != nil {
		t.Error("empty plan should map to nil")
	}
	if planInteraction(json.RawMessage(`not json`)) != nil {
		t.Error("undecodable params should map to nil")
	}
}

// asPhase maps an app-server item notification suffix to a lifecycle phase (unknown → started).
func TestAsPhase(t *testing.T) {
	for suffix, want := range map[string]itemPhase{
		"started":   phaseStarted,
		"updated":   phaseUpdated,
		"completed": phaseCompleted,
		"weird":     phaseStarted,
	} {
		if got := asPhase(suffix); got != want {
			t.Errorf("asPhase(%q) = %v, want %v", suffix, got, want)
		}
	}
}

// The app-server request-param builders include the shared fields and omit optional ones (model/effort)
// when unset — so a default turn sends the minimal envelope and a configured turn carries the extras.
func TestAppServerParams(t *testing.T) {
	full := appServer{model: "gpt-5.5", effort: "high", sandbox: "workspace-write", approval: "on-request", cwd: "/repo", resumeID: "th-1", message: "go"}
	if p := full.startParams(); p["model"] != "gpt-5.5" || p["cwd"] != "/repo" || p["sandbox"] != "workspace-write" {
		t.Errorf("startParams = %+v, want model/cwd/sandbox populated", p)
	}
	rp := full.resumeParams()
	if rp["threadId"] != "th-1" || rp["model"] != "gpt-5.5" || rp["approvalPolicy"] != "on-request" {
		t.Errorf("resumeParams = %+v, want the resume envelope", rp)
	}
	tp := full.turnParams("th-9")
	if tp["threadId"] != "th-9" || tp["model"] != "gpt-5.5" || tp["effort"] != "high" {
		t.Errorf("turnParams = %+v, want threadId/model/effort", tp)
	}

	// A default (unconfigured) turn omits the optional keys entirely.
	bare := appServer{sandbox: "read-only", approval: "never"}
	if p := bare.startParams(); p["model"] != nil || p["cwd"] != nil {
		t.Errorf("bare startParams should omit model/cwd, got %+v", p)
	}
	if p := bare.turnParams("t"); p["model"] != nil || p["effort"] != nil {
		t.Errorf("bare turnParams should omit model/effort, got %+v", p)
	}
}

// turnIDOf pulls the turn id out of a turn envelope and returns "" on a decode miss.
func TestTurnIDOf(t *testing.T) {
	if got := turnIDOf(json.RawMessage(`{"turn":{"id":"turn-7","status":"completed"}}`)); got != "turn-7" {
		t.Errorf("turnIDOf = %q, want turn-7", got)
	}
	if got := turnIDOf(json.RawMessage(`}{`)); got != "" {
		t.Errorf("turnIDOf on junk = %q, want empty", got)
	}
}

// Adapter metadata methods are pure descriptors; this exercises them so the catalog surface is covered
// and a regression in the declared shape (id/name, config filename, notification conditions) is caught.
func TestAdapterMetadataSmoke(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	a := adapter{}

	if a.Meta().ID != "codex" || a.Meta().Name == "" {
		t.Errorf("Meta = %+v, want id=codex with a name", a.Meta())
	}
	caps := a.Capabilities()
	if !caps.SystemPrompt || !caps.MCPServers || caps.Subagents || caps.ClaudeSettings {
		t.Errorf("Capabilities option flags = %+v, want systemPrompt+mcpServers on, subagents+claudeSettings off", caps)
	}
	if steps := a.InstallSteps(); len(steps) == 0 || steps[0].Name == "" {
		t.Errorf("InstallSteps = %+v, want at least one named step", steps)
	}
	if a.VersionCommand() == "" {
		t.Error("VersionCommand should be non-empty")
	}
	if p := a.ConfigPath(); filepath.Base(p) != "config.toml" {
		t.Errorf("ConfigPath = %q, want it to end in config.toml", p)
	}
	if checks := a.Doctor(context.Background()); len(checks) == 0 {
		t.Error("Doctor should return at least one check")
	}
	if a.Auth(mapStore{}) == nil {
		t.Error("Auth should return a module")
	}
	conds := a.Notifications().Conditions
	if len(conds) == 0 {
		t.Fatal("Notifications should declare conditions")
	}
	sawApproval := false
	for _, c := range conds {
		if c.Condition == agent.WaitingApproval && len(c.Actions) == 2 {
			sawApproval = true
		}
	}
	if !sawApproval {
		t.Error("expected a waiting_approval condition carrying approve/reject actions")
	}
}

// RunStream's exec branch composes materialize → buildExecCommand → driver. Its materialize-error
// path is the one branch observable without spawning a process: a turn with undecodable mcpServers
// (and no inbound channel, so the autonomous exec transport is selected) surfaces the failure as an
// EventError plus an IsError result — never a silent drop, never a spawned CLI. The generic spawn/parse
// tail is covered by the driver and parseStream unit tests; a live `codex exec` turn is the integration
// check (see the plan's live smoke).
func TestRunStreamMaterializeError(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir()) // resolvable home so materialize reaches the decode (not a home error)

	var evs []agent.Event
	res, err := adapter{}.RunStream(context.Background(),
		agent.TurnInput{
			Message: "hi",
			Options: agent.TurnOptions{MCPServers: json.RawMessage(`{not valid json`)},
		},
		func(e agent.Event) { evs = append(evs, e) })
	if err != nil {
		t.Fatalf("RunStream should fold a materialize error into the result, not return err: %v", err)
	}
	if !res.IsError || res.Text == "" {
		t.Fatalf("result = %+v, want an error result carrying the materialize failure", res)
	}
	sawErr := false
	for _, e := range evs {
		if e.Type == agent.EventError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("expected an EventError for the materialize failure")
	}
}

// Begin returns a needs_input state for each known method and an error for an unknown one; Methods
// declares the two credential methods with their secret fields.
func TestAuthBeginAndMethods(t *testing.T) {
	m := newAuth(mapStore{})
	if len(m.Methods()) != 2 {
		t.Fatalf("Methods = %d, want 2 (apiKey + accessToken)", len(m.Methods()))
	}
	for _, id := range []string{"apiKey", "accessToken"} {
		st, err := m.Begin(context.Background(), id)
		if err != nil || st.Status != "needs_input" || st.Method != id || len(st.Fields) == 0 {
			t.Errorf("Begin(%q) = %+v, err=%v; want needs_input with fields", id, st, err)
		}
	}
	if _, err := m.Begin(context.Background(), "nope"); err == nil {
		t.Error("Begin on an unknown method should error")
	}
}
