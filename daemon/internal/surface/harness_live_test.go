package surface

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	_ "github.com/oblien/mindwire/daemon/internal/agent/claude"
	_ "github.com/oblien/mindwire/daemon/internal/agent/codex"
)

// Opt-in native CLI check against a synthetic desktop. Uses the developer's
// existing CLI login without reading or exporting its credentials.
func TestHarnessDesktopMCP(t *testing.T) {
	testHarnessDesktop(t, false)
}

func TestHarnessProjectDesktopMCP(t *testing.T) {
	testHarnessDesktop(t, true)
}

func testHarnessDesktop(t *testing.T, project bool) {
	t.Helper()
	harness := os.Getenv("MINDWIRE_DESKTOP_HARNESS")
	if harness == "" {
		t.Skip("opt in with an authenticated codex or claude-code CLI")
	}
	var adapter agent.Adapter
	for _, candidate := range agent.All() {
		if candidate.ID() == harness {
			adapter = candidate
			break
		}
	}
	if adapter == nil {
		t.Fatal("unknown test harness")
	}
	var s *Service
	var fixture *catalogFixture
	actor := Actor{Kind: "agent", Name: harness, RunID: "native-cli", ChatID: "native-cli"}
	if project {
		s, fixture, _ = catalogService(t)
		actor.ProjectID = "project-a"
	} else {
		var provider *testProvider
		s, provider, _ = testService(t)
		provider.size = Geometry{128, 96, 1}
	}
	var approvals atomic.Int32
	s.SetApproval(func(context.Context, Actor, string) error { approvals.Add(1); return nil })
	ctx, cancel := context.WithTimeout(t.Context(), 110*time.Second)
	defer cancel()
	overlay, env, cleanup, err := s.BindRun(ctx, actor, harness, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if harness == "claude-code" && project && os.Getenv("MINDWIRE_DESKTOP_MODEL_FIXTURE") == "1" {
		localClaudeDesktopModel(t, s, env)
	}
	config := map[string]string{"model": "haiku", "permission-mode": "dontAsk", "max-turns": "8",
		"disallowed-tools": "Bash,Read,Write,Edit,Glob,Grep,WebFetch,WebSearch,Agent,Skill"}
	if harness == "codex" {
		config = map[string]string{"permission-mode": "never", "sandbox": "read-only"}
	}
	settings, err := PermissionSettings(harness, nil)
	if err != nil {
		t.Fatal(err)
	}
	observed := map[string]bool{}
	prompt := "Verify the synthetic mindwire_desktop MCP service. Use only its tools. First surface_status; then surface_open in control mode with requestId cli-open; then surface_capture. Use that session ID, controlGeneration and frameId to surface_action with requestId cli-escape, action kind key and keys [Escape]. Finally surface_release. Do not use any other tool, inspect files, or run commands. Briefly describe the image and report completion. These are synthetic test pixels, not a real desktop."
	if project {
		prompt = strings.ReplaceAll(prompt, "First surface_status; then surface_open", "First surface_desktops; then surface_project_desktop")
	}
	result, err := adapter.RunStream(ctx, agent.TurnInput{Message: prompt, CWD: t.TempDir(), Config: config, Env: env, Options: agent.TurnOptions{MCPServers: overlay, ClaudeSettings: settings}}, func(ev agent.Event) {
		if ev.Type == agent.EventResult || ev.Type == agent.EventError {
			t.Logf("NATIVE_DESKTOP terminal=%s", ev.Type)
		}
		if ev.Tool == nil {
			return
		}
		agent.NormalizeSurfaceTool(ev.Tool)
		if ev.Type == agent.EventToolResult && ev.Tool.Action != nil && ev.Tool.Action.Surface != nil {
			action := ev.Tool.Action.Surface
			if project && action.Operation != "list" && action.DesktopID != "ds_0000000000000001" {
				t.Errorf("native desktop card lost its saved desktop ID: %s", action.Operation)
			}
			if action.Operation == "capture" && action.Capture == nil {
				t.Error("native CLI result lost its screenshot artifact")
			}
			observed[action.Operation] = true
			t.Logf("NATIVE_DESKTOP operation=%s error=%v", action.Operation, ev.Tool.IsError)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("native CLI ended with an error: %s", strings.TrimSpace(result.Text))
	}
	operations := []string{"status", "open", "capture", "key", "release"}
	if project {
		operations[0] = "list"
	}
	for _, operation := range operations {
		if !observed[operation] {
			t.Errorf("native CLI did not complete %s", operation)
		}
	}
	if approvals.Load() != 1 {
		t.Errorf("want one desktop permission, got %d", approvals.Load())
	}
	if fixture != nil {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		if fixture.posts != 1 || len(fixture.providers) != 1 {
			t.Errorf("native project desktop was duplicated: creates=%d providers=%d", fixture.posts, len(fixture.providers))
		}
	}
}
