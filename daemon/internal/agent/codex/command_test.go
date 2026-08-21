package codex

import (
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// fullConfig exercises every mappable setting so both branches assert the whole surface.
var fullConfig = map[string]string{
	keyModel:    "gpt-5.5",
	keyEffort:   "high",
	keyApproval: "on-request",
	keySandbox:  "read-only",
	keyWorkdir:  "/repo",
	keyAddDir:   "/extra",
}

// A fresh (non-resume) turn uses the flags exec accepts directly: -s for sandbox, -C for the working
// dir, --add-dir for the extra dir. Reasoning effort and approval policy are always -c overrides.
func TestBuildExecCommandFresh(t *testing.T) {
	cmd := buildExecCommand(agent.TurnInput{Message: "hi there", Config: fullConfig}, materialized{})
	for _, want := range []string{
		"codex exec --json --skip-git-repo-check",
		"-m 'gpt-5.5'",
		"-c 'model_reasoning_effort=high'",
		"-c 'approval_policy=on-request'",
		"-s 'read-only'",
		"-C '/repo'",
		"--add-dir '/extra'",
		"'hi there'",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("fresh command missing %q\n got: %s", want, cmd)
		}
	}
	// Fresh must NOT use the resume-only config overrides.
	for _, bad := range []string{"resume", "sandbox_mode=", "writable_roots"} {
		if strings.Contains(cmd, bad) {
			t.Errorf("fresh command should not contain %q\n got: %s", bad, cmd)
		}
	}
}

// A resumed turn rejects -s/-C/--add-dir, so sandbox and the extra writable dir become -c overrides
// and the working dir is dropped (a resumed session keeps its recorded root). Model, effort, and
// approval still apply.
func TestBuildExecCommandResume(t *testing.T) {
	cmd := buildExecCommand(agent.TurnInput{Message: "keep going", SessionID: "sess-1", Config: fullConfig}, materialized{})
	for _, want := range []string{
		"codex exec resume 'sess-1'",
		"-m 'gpt-5.5'",
		"-c 'model_reasoning_effort=high'",
		"-c 'approval_policy=on-request'",
		"-c 'sandbox_mode=read-only'",
		`-c 'sandbox_workspace_write.writable_roots=["/extra"]'`,
		"'keep going'",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("resume command missing %q\n got: %s", want, cmd)
		}
	}
	// Resume must NOT use the fresh-only flags.
	if strings.Contains(cmd, " -s '") {
		t.Errorf("resume must not pass -s (rejected on resume)\n got: %s", cmd)
	}
	if strings.Contains(cmd, " -C '") || strings.Contains(cmd, "--add-dir") {
		t.Errorf("resume must not pass -C/--add-dir (rejected on resume)\n got: %s", cmd)
	}
}

// Session-control precedence: an explicit per-turn id > ContinueLatest > the chat's stored id.
func TestBuildExecCommandSessionPrecedence(t *testing.T) {
	pin := buildExecCommand(agent.TurnInput{Message: "x", SessionID: "stored", Options: agent.TurnOptions{SessionID: "explicit"}}, materialized{})
	if !strings.Contains(pin, "resume 'explicit'") {
		t.Errorf("explicit session id should win, got: %s", pin)
	}

	cont := buildExecCommand(agent.TurnInput{Message: "x", SessionID: "stored", Options: agent.TurnOptions{ContinueLatest: true}}, materialized{})
	if !strings.Contains(cont, "resume --last") || strings.Contains(cont, "'stored'") {
		t.Errorf("ContinueLatest should resume --last, got: %s", cont)
	}

	stored := buildExecCommand(agent.TurnInput{Message: "x", SessionID: "stored"}, materialized{})
	if !strings.Contains(stored, "resume 'stored'") {
		t.Errorf("stored session id should resume, got: %s", stored)
	}
}

// Defaults: an unconfigured turn is autonomous — approval never, sandbox workspace-write — and unset
// settings never appear as flags.
func TestBuildExecCommandDefaultsAndOmits(t *testing.T) {
	cmd := buildExecCommand(agent.TurnInput{Message: "x"}, materialized{})
	if !strings.Contains(cmd, "-c 'approval_policy=never'") {
		t.Errorf("expected default approval_policy=never, got: %s", cmd)
	}
	if !strings.Contains(cmd, "-s 'workspace-write'") {
		t.Errorf("expected default sandbox workspace-write, got: %s", cmd)
	}
	for _, bad := range []string{"-m ", "-C ", "--add-dir", "model_reasoning_effort", "resume"} {
		if strings.Contains(cmd, bad) {
			t.Errorf("unset setting leaked as %q, got: %s", bad, cmd)
		}
	}
}

// A resume with no new prompt is valid and must not append an empty ” argument.
func TestBuildExecCommandEmptyMessageResume(t *testing.T) {
	cmd := buildExecCommand(agent.TurnInput{Message: "", SessionID: "sess-1"}, materialized{})
	if strings.HasSuffix(cmd, "''") {
		t.Errorf("empty message must not become a trailing '' arg, got: %s", cmd)
	}
	if !strings.Contains(cmd, "resume 'sess-1'") {
		t.Errorf("expected resume with the stored id, got: %s", cmd)
	}
}

// The message is POSIX single-quoted so shell metacharacters can't break out.
func TestBuildExecCommandQuotesInjection(t *testing.T) {
	cmd := buildExecCommand(agent.TurnInput{Message: "a'; rm -rf /"}, materialized{})
	if !strings.Contains(cmd, `'\''`) {
		t.Errorf("message not safely single-quoted: %s", cmd)
	}
}

// Auto-compact (F4a): the canon autoCompactTokens maps to a -c model_auto_compact_token_limit
// override. Codex takes an integer only, so a non-numeric value is ignored; unset emits nothing.
func TestBuildExecCommandAutoCompact(t *testing.T) {
	cmd := buildExecCommand(agent.TurnInput{Message: "x", Config: map[string]string{keyAutoCompact: "200000"}}, materialized{})
	if !strings.Contains(cmd, "-c 'model_auto_compact_token_limit=200000'") {
		t.Errorf("expected -c model_auto_compact_token_limit override, got: %s", cmd)
	}
	nonNumeric := buildExecCommand(agent.TurnInput{Message: "x", Config: map[string]string{keyAutoCompact: "auto"}}, materialized{})
	if strings.Contains(nonNumeric, "model_auto_compact_token_limit") {
		t.Errorf("non-numeric auto-compact must be ignored, got: %s", nonNumeric)
	}
	none := buildExecCommand(agent.TurnInput{Message: "x"}, materialized{})
	if strings.Contains(none, "model_auto_compact_token_limit") {
		t.Errorf("unset auto-compact must not appear, got: %s", none)
	}
}

// Materialized per-turn files (output schema, image attachments) become their flags.
func TestBuildExecCommandMaterializedFiles(t *testing.T) {
	cmd := buildExecCommand(agent.TurnInput{Message: "x"}, materialized{
		outputSchemaPath: "/tmp/s.json",
		imagePaths:       []string{"/img/a.png", "/img/b.jpg"},
	})
	for _, want := range []string{"--output-schema '/tmp/s.json'", "-i '/img/a.png'", "-i '/img/b.jpg'"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("materialized command missing %q\n got: %s", want, cmd)
		}
	}
}
