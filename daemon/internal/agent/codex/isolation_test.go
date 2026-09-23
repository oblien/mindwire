package codex

import (
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestPlacementControlsOnlyDefaultSandbox(t *testing.T) {
	for _, placement := range []string{"", "direct", "container", "unknown"} {
		t.Run(placement, func(t *testing.T) {
			t.Setenv("MINDWIRE_ISOLATION", placement)
			want := "workspace-write"
			if placement == "container" {
				want = "danger-full-access"
			}
			for _, sessionID := range []string{"", "existing-thread"} {
				input := agent.TurnInput{Message: "continue", SessionID: sessionID, Config: map[string]string{keyApproval: "on-request"},
					Env: map[string]string{"MINDWIRE_ISOLATION": "container"}}
				server := newAppServer(input, materialized{})
				if server.sandbox != want || server.approval != "on-request" {
					t.Fatalf("placement changed approvals or ignored sandbox: %s, %s", server.sandbox, server.approval)
				}
				flag := "-s '" + want + "'"
				if sessionID != "" {
					flag = "-c 'sandbox_mode=" + want + "'"
				}
				if command := buildExecCommand(input, materialized{}); !strings.Contains(command, flag) ||
					!strings.Contains(command, "approval_policy=on-request") {
					t.Fatalf("exec/resume must agree with app-server: %s", command)
				}
				input.Config[keySandbox] = "read-only"
				if sandbox(input) != "read-only" {
					t.Fatal("placement replaced an explicit user restriction")
				}
			}
		})
	}
}
