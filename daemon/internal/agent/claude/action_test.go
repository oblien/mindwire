package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestClaudeEditAction(t *testing.T) {
	a := claudeToolAction("Edit", json.RawMessage(`{"file_path":"main.go","old_string":"foo","new_string":"bar"}`), "", false)
	if a == nil || a.Kind != agent.KindFileEdit || len(a.Files) != 1 {
		t.Fatalf("Edit action = %+v", a)
	}
	f := a.Files[0]
	if f.Path != "main.go" || f.Op != "edit" || f.OldText != "foo" || f.NewText != "bar" {
		t.Errorf("file change = %+v", f)
	}
	if !strings.Contains(f.Diff, "-foo") || !strings.Contains(f.Diff, "+bar") {
		t.Errorf("edit diff should show the fragment change:\n%s", f.Diff)
	}
}

func TestClaudeWriteAction(t *testing.T) {
	a := claudeToolAction("Write", json.RawMessage(`{"file_path":"new.txt","content":"hi\nthere\n"}`), "", false)
	if a == nil || len(a.Files) != 1 || a.Files[0].Op != "create" {
		t.Fatalf("Write action = %+v", a)
	}
	if a.Files[0].NewText != "hi\nthere\n" {
		t.Errorf("Write newText = %q", a.Files[0].NewText)
	}
	if !strings.Contains(a.Files[0].Diff, "+hi") {
		t.Errorf("Write diff should be all-add:\n%s", a.Files[0].Diff)
	}
}

func TestClaudeMultiEditAction(t *testing.T) {
	a := claudeToolAction("MultiEdit", json.RawMessage(`{"file_path":"m.go","edits":[{"old_string":"a","new_string":"A"},{"old_string":"b","new_string":"B"}]}`), "", false)
	if a == nil || len(a.Files) != 1 {
		t.Fatalf("MultiEdit action = %+v", a)
	}
	d := a.Files[0].Diff
	if !strings.Contains(d, "--- a/m.go") || !strings.Contains(d, "-a") || !strings.Contains(d, "+A") || !strings.Contains(d, "-b") || !strings.Contains(d, "+B") {
		t.Errorf("MultiEdit diff should carry both hunks under one header:\n%s", d)
	}
}

// The pointer-field correctness trap: Claude's Bash reports no exit code and merges stderr into stdout,
// so ExitCode and Stderr MUST stay nil (≠ 0 / "") while Stdout carries the combined output.
func TestClaudeBashActionNilExitAndStderr(t *testing.T) {
	a := claudeToolAction("Bash", json.RawMessage(`{"command":"ls -la"}`), "total 0\n", false)
	if a == nil || a.Kind != agent.KindShell || a.Shell == nil {
		t.Fatalf("Bash action = %+v", a)
	}
	if a.Shell.Command != "ls -la" || a.Shell.Stdout != "total 0\n" {
		t.Errorf("shell = %+v", a.Shell)
	}
	if a.Shell.ExitCode != nil {
		t.Errorf("Claude Bash must leave ExitCode nil (not reported), got %d", *a.Shell.ExitCode)
	}
	if a.Shell.Stderr != "" {
		t.Errorf("Claude Bash merges stderr into stdout; Stderr must be empty, got %q", a.Shell.Stderr)
	}
}

func TestClaudeReadAction(t *testing.T) {
	a := claudeToolAction("Read", json.RawMessage(`{"file_path":"/x/y.go"}`), "", false)
	if a == nil || a.Kind != agent.KindFileRead || a.Title != "/x/y.go" {
		t.Fatalf("Read action = %+v", a)
	}
	if len(a.Files) != 0 {
		t.Errorf("Read is Title-only, should carry no Files, got %+v", a.Files)
	}
}

func TestClaudeSearchActions(t *testing.T) {
	g := claudeToolAction("Grep", json.RawMessage(`{"pattern":"TODO","path":"src","glob":"*.go"}`), "", false)
	if g == nil || g.Kind != agent.KindSearch || g.Search == nil || g.Search.Query != "TODO" || g.Search.Glob != "*.go" {
		t.Errorf("Grep action = %+v", g)
	}
	// Glob's `pattern` is the glob itself.
	gl := claudeToolAction("Glob", json.RawMessage(`{"pattern":"**/*.ts"}`), "", false)
	if gl == nil || gl.Search == nil || gl.Search.Glob != "**/*.ts" {
		t.Errorf("Glob action = %+v", gl)
	}
}

func TestClaudeWebActions(t *testing.T) {
	f := claudeToolAction("WebFetch", json.RawMessage(`{"url":"https://x.dev"}`), "", false)
	if f == nil || f.Kind != agent.KindWebFetch || f.Web == nil || f.Web.URL != "https://x.dev" {
		t.Errorf("WebFetch action = %+v", f)
	}
	s := claudeToolAction("WebSearch", json.RawMessage(`{"query":"golang lsp"}`), "", false)
	if s == nil || s.Kind != agent.KindWebSearch || s.Web == nil || s.Web.Query != "golang lsp" {
		t.Errorf("WebSearch action = %+v", s)
	}
}

func TestClaudeMCPAction(t *testing.T) {
	a := claudeToolAction("mcp__github__create_issue", nil, "", false)
	if a == nil || a.Kind != agent.KindMCP || a.MCP == nil || a.MCP.Server != "github" || a.MCP.Tool != "create_issue" {
		t.Errorf("mcp action = %+v", a)
	}
}

func TestClaudeUnknownAction(t *testing.T) {
	a := claudeToolAction("SomeFutureTool", json.RawMessage(`{}`), "", false)
	if a == nil || a.Kind != agent.KindOther || a.Title != "SomeFutureTool" {
		t.Errorf("unknown tool action = %+v", a)
	}
}

// A malformed input must degrade to a Title-only action of the right Kind, never panic.
func TestClaudeMalformedInputGraceful(t *testing.T) {
	a := claudeToolAction("Edit", json.RawMessage(`not json`), "", false)
	if a == nil || a.Kind != agent.KindFileEdit {
		t.Errorf("malformed Edit should still classify as file_edit, got %+v", a)
	}
}
