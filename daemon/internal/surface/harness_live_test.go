package surface

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	_ "github.com/oblien/mindwire/daemon/internal/agent/claude"
	_ "github.com/oblien/mindwire/daemon/internal/agent/codex"
)

// Opt-in native CLI check against a synthetic desktop. Uses the developer's
// existing CLI login without reading or exporting its credentials.
func TestHarnessDesktopMCP(t *testing.T) {
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
	s, provider, _ := testService(t)
	provider.size = Geometry{128, 96, 1}
	approvals := 0
	s.SetApproval(func(context.Context, Actor, string) error { approvals++; return nil })
	ctx, cancel := context.WithTimeout(t.Context(), 110*time.Second)
	defer cancel()
	overlay, env, cleanup, err := s.BindRun(ctx, Actor{Kind: "agent", Name: harness, RunID: "native-cli", ChatID: "native-cli"}, harness, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
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
	for _, operation := range []string{"status", "open", "capture", "key", "release"} {
		if !observed[operation] {
			t.Errorf("native CLI did not complete %s", operation)
		}
	}
	if approvals != 1 {
		t.Errorf("want one desktop permission, got %d", approvals)
	}
}
