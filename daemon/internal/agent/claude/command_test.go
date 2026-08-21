package claude

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestBuildCommandAppliesSettings(t *testing.T) {
	cmd := buildCommand(agent.TurnInput{
		Message: "hi there",
		Config: map[string]string{
			"model":                "opus",
			"effort":               "max",
			"permission-mode":      "plan",
			"tools":                "Bash,Edit",
			"append-system-prompt": "be terse",
			"max-budget-usd":       "5",
			"max-turns":            "10",
		},
		SessionID: "abc-123",
	}, materialized{}, transportOneShot)
	for _, want := range []string{
		"claude -p 'hi there'",
		"--output-format stream-json",
		"--permission-mode 'plan'",
		"--model 'opus'",
		"--effort 'max'",
		"--tools 'Bash,Edit'",
		"--append-system-prompt 'be terse'",
		"--max-budget-usd '5'",
		"--max-turns '10'",
		"--resume 'abc-123'",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q\n got: %s", want, cmd)
		}
	}
}

func TestBuildCommandDefaultsAndOmits(t *testing.T) {
	cmd := buildCommand(agent.TurnInput{Message: "x"}, materialized{}, transportOneShot)
	if !strings.Contains(cmd, "--permission-mode 'bypassPermissions'") {
		t.Errorf("expected default bypassPermissions, got: %s", cmd)
	}
	if strings.Contains(cmd, "--model") || strings.Contains(cmd, "--resume") {
		t.Errorf("unset settings must not appear as flags, got: %s", cmd)
	}
}

func TestBuildCommandQuotesInjection(t *testing.T) {
	cmd := buildCommand(agent.TurnInput{Message: "a'; rm -rf /"}, materialized{}, transportOneShot)
	if strings.Contains(cmd, "rm -rf /'") == false && !strings.Contains(cmd, `'\''`) {
		t.Errorf("message not safely single-quoted: %s", cmd)
	}
}

// System prompt: the per-turn Options value fully overrides the default AND wins over the sticky
// system-prompt setting; it is distinct from --append-system-prompt.
func TestBuildCommandSystemPromptPrecedence(t *testing.T) {
	cmd := buildCommand(agent.TurnInput{
		Message: "x",
		Config:  map[string]string{"system-prompt": "sticky one"},
		Options: agent.TurnOptions{SystemPrompt: "per-turn wins"},
	}, materialized{}, transportOneShot)
	if !strings.Contains(cmd, "--system-prompt 'per-turn wins'") {
		t.Errorf("per-turn system prompt should win, got: %s", cmd)
	}
	if strings.Contains(cmd, "sticky one") {
		t.Errorf("sticky system prompt should have been overridden, got: %s", cmd)
	}

	sticky := buildCommand(agent.TurnInput{Message: "x", Config: map[string]string{"system-prompt": "sticky only"}}, materialized{}, transportOneShot)
	if !strings.Contains(sticky, "--system-prompt 'sticky only'") {
		t.Errorf("sticky system prompt should apply when no per-turn value, got: %s", sticky)
	}
}

// Session-control precedence: an explicit per-turn session id > ContinueLatest > the chat's stored
// resume id; --fork-session only appears when a base session is being resumed.
func TestBuildCommandSessionControls(t *testing.T) {
	pin := buildCommand(agent.TurnInput{Message: "x", SessionID: "stored", Options: agent.TurnOptions{SessionID: "explicit"}}, materialized{}, transportOneShot)
	if !strings.Contains(pin, "--session-id 'explicit'") || strings.Contains(pin, "--resume") {
		t.Errorf("explicit session id should win over resume, got: %s", pin)
	}

	cont := buildCommand(agent.TurnInput{Message: "x", SessionID: "stored", Options: agent.TurnOptions{ContinueLatest: true, ForkOnResume: true}}, materialized{}, transportOneShot)
	if !strings.Contains(cont, "--continue") || strings.Contains(cont, "--resume") {
		t.Errorf("ContinueLatest should win over resume, got: %s", cont)
	}
	if !strings.Contains(cont, "--fork-session") {
		t.Errorf("ForkOnResume should add --fork-session when resuming, got: %s", cont)
	}

	resume := buildCommand(agent.TurnInput{Message: "x", SessionID: "stored"}, materialized{}, transportOneShot)
	if !strings.Contains(resume, "--resume 'stored'") {
		t.Errorf("stored session id should resume when no options, got: %s", resume)
	}

	// Fork with no base session to fork from → no --fork-session.
	forkNoBase := buildCommand(agent.TurnInput{Message: "x", Options: agent.TurnOptions{ForkOnResume: true}}, materialized{}, transportOneShot)
	if strings.Contains(forkNoBase, "--fork-session") {
		t.Errorf("--fork-session must not appear without a base session, got: %s", forkNoBase)
	}
}

// Persistent transport: no `-p <msg>` argument (the message goes over stdin), and the stream-json
// input + permission-prompt tool that arm can_use_tool routing are present. Settings still apply.
func TestBuildCommandPersistent(t *testing.T) {
	cmd := buildCommand(agent.TurnInput{
		Message: "should not appear as an arg",
		Config:  map[string]string{"permission-mode": "plan", "model": "opus"},
	}, materialized{}, transportPersistent)
	for _, want := range []string{
		"--input-format stream-json",
		"--output-format stream-json",
		"--permission-prompt-tool stdio",
		"--permission-mode 'plan'",
		"--model 'opus'",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("persistent command missing %q\n got: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "should not appear as an arg") {
		t.Errorf("persistent transport must not pass the message as an argument, got: %s", cmd)
	}
}

// Auto-compact (F4a): the canon autoCompactTokens setting maps to Claude's --autocompact launch
// flag; an unset value emits no flag.
func TestBuildCommandAutoCompact(t *testing.T) {
	cmd := buildCommand(agent.TurnInput{Message: "x", Config: map[string]string{"autocompact": "200000"}}, materialized{}, transportOneShot)
	if !strings.Contains(cmd, "--autocompact '200000'") {
		t.Errorf("expected --autocompact flag, got: %s", cmd)
	}
	auto := buildCommand(agent.TurnInput{Message: "x", Config: map[string]string{"autocompact": "auto"}}, materialized{}, transportOneShot)
	if !strings.Contains(auto, "--autocompact 'auto'") {
		t.Errorf("expected --autocompact 'auto', got: %s", auto)
	}
	none := buildCommand(agent.TurnInput{Message: "x"}, materialized{}, transportOneShot)
	if strings.Contains(none, "--autocompact") {
		t.Errorf("unset autocompact must not appear as a flag, got: %s", none)
	}
}

// Structured per-turn file paths (materialized by RunStream) become their flags.
func TestBuildCommandMaterializedFiles(t *testing.T) {
	cmd := buildCommand(agent.TurnInput{Message: "x"}, materialized{jsonSchemaPath: "/tmp/s.json", mcpConfigPath: "/tmp/m.json", settingsPath: "/tmp/set.json"}, transportOneShot)
	if !strings.Contains(cmd, "--json-schema '/tmp/s.json'") {
		t.Errorf("expected --json-schema flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--mcp-config '/tmp/m.json'") {
		t.Errorf("expected --mcp-config flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--settings '/tmp/set.json'") {
		t.Errorf("expected --settings flag, got: %s", cmd)
	}
}

// Subagents ride INLINE on --agents (a file path is not loaded by the CLI), so the raw JSON object is
// single-quoted onto the command directly — no temp file. --settings, by contrast, is a materialized
// path. Absent options emit neither flag.
func TestBuildCommandSubagents(t *testing.T) {
	agents := `{"reviewer":{"description":"Reviews code","prompt":"You are a reviewer."}}`
	cmd := buildCommand(agent.TurnInput{
		Message: "x",
		Options: agent.TurnOptions{Subagents: json.RawMessage(agents)},
	}, materialized{}, transportOneShot)
	if !strings.Contains(cmd, "--agents "+agent.ShellQuote(agents)) {
		t.Errorf("expected inline --agents JSON, got: %s", cmd)
	}

	none := buildCommand(agent.TurnInput{Message: "x"}, materialized{}, transportOneShot)
	if strings.Contains(none, "--agents") || strings.Contains(none, "--settings") {
		t.Errorf("no subagents/settings options should emit neither flag, got: %s", none)
	}
}

// F3 true vision: an image attachment becomes a base64 image content block the model SEES (routed to
// imageBlocks, never into the path-reference msgAppend), while a non-image attachment stays a path
// reference. materialize routes unconditionally, so a single turn can mix both.
func TestMaterializeImageBlocks(t *testing.T) {
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake-image-bytes")
	_, msgAppend, imageBlocks, cleanup, err := materialize(agent.TurnOptions{
		Attachments: []agent.Attachment{
			{Name: "shot.png", Mime: "image/png", Data: pngBytes},
			{Name: "notes.txt", Path: "/tmp/notes.txt"},
		},
	})
	defer cleanup()
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(imageBlocks) != 1 {
		t.Fatalf("expected 1 image block, got %d", len(imageBlocks))
	}
	blk, ok := imageBlocks[0].(map[string]any)
	if !ok || blk["type"] != "image" {
		t.Fatalf("image block malformed: %#v", imageBlocks[0])
	}
	src, ok := blk["source"].(map[string]any)
	if !ok || src["type"] != "base64" || src["media_type"] != "image/png" {
		t.Fatalf("image source malformed: %#v", blk["source"])
	}
	if got := src["data"]; got != base64.StdEncoding.EncodeToString(pngBytes) {
		t.Errorf("image data not base64 of the bytes, got: %v", got)
	}
	// The image must NOT also appear as a path reference (no double-delivery); the text file must.
	if strings.Contains(msgAppend, "shot.png") {
		t.Errorf("image attachment leaked into path-reference append: %s", msgAppend)
	}
	if !strings.Contains(msgAppend, "/tmp/notes.txt") {
		t.Errorf("non-image attachment should be a path reference, got: %s", msgAppend)
	}

	// The stdin payload wraps the message text and the image block into one user message.
	payload := userMessageBlocks("describe this", imageBlocks)
	line, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal user message: %v", err)
	}
	var decoded struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &decoded); err != nil {
		t.Fatalf("unmarshal user message: %v", err)
	}
	if decoded.Type != "user" || decoded.Message.Role != "user" {
		t.Fatalf("unexpected envelope: %s", line)
	}
	if len(decoded.Message.Content) != 2 || decoded.Message.Content[0].Type != "text" || decoded.Message.Content[1].Type != "image" {
		t.Fatalf("content should be [text, image], got: %s", line)
	}
}

// A turn carrying image blocks runs on the stream-json input transport: no `-p <msg>` argument (the
// message rides on stdin), --input-format stream-json present, and NOT the persistent-only
// permission-prompt tool (a bypass image turn never pauses).
func TestBuildCommandStreamInput(t *testing.T) {
	cmd := buildCommand(agent.TurnInput{
		Message: "should not appear as an arg",
		Config:  map[string]string{"model": "opus"},
	}, materialized{}, transportStreamInput)
	if !strings.Contains(cmd, "--input-format stream-json") {
		t.Errorf("stream-input command missing --input-format stream-json, got: %s", cmd)
	}
	if strings.Contains(cmd, "should not appear as an arg") {
		t.Errorf("stream-input transport must not pass the message as an argument, got: %s", cmd)
	}
	if strings.Contains(cmd, "--permission-prompt-tool") {
		t.Errorf("one-shot stream-input must not arm the permission-prompt tool, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--model 'opus'") {
		t.Errorf("settings should still apply on the stream-input transport, got: %s", cmd)
	}
}

// materialize writes the settings/hooks bundle to a temp file (keeping a large bundle off argv) and
// cleanup removes it; subagents are inline so they never produce a file.
func TestMaterializeSettings(t *testing.T) {
	const body = `{"hooks":{"PreToolUse":[]},"permissions":{"allow":["Bash"]}}`
	files, _, _, cleanup, err := materialize(agent.TurnOptions{
		ClaudeSettings: json.RawMessage(body),
		Subagents:      json.RawMessage(`{"r":{"description":"d","prompt":"p"}}`),
	})
	defer cleanup()
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if files.settingsPath == "" {
		t.Fatal("expected a settings file path")
	}
	if got, err := os.ReadFile(files.settingsPath); err != nil || string(got) != body {
		t.Fatalf("settings file = %q err=%v, want %q", got, err, body)
	}
	cleanup()
	if _, err := os.Stat(files.settingsPath); !os.IsNotExist(err) {
		t.Errorf("cleanup should remove the settings file (stat err=%v)", err)
	}
}
