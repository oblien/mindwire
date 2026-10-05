package codex

import (
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestNativeAccessPreservesWorkspaceBoundaryAndExplicitPermissions(t *testing.T) {
	for _, placement := range []string{"", "direct", "container", "unknown"} {
		t.Run(placement, func(t *testing.T) {
			t.Setenv("MINDWIRE_ISOLATION", placement)
			want := "danger-full-access"
			for _, sessionID := range []string{"", "existing-thread"} {
				input := agent.TurnInput{Message: "continue", SessionID: sessionID, Config: map[string]string{keyApproval: "on-request"},
					Env: map[string]string{"MINDWIRE_ISOLATION": "container"}}
				server := newAppServer(input, materialized{})
				if server.sandbox != want || server.approval != "on-request" {
					t.Fatalf("placement changed approvals or ignored sandbox: %s, %s", server.sandbox, server.approval)
				}
				for _, params := range []map[string]any{server.startParams(), server.resumeParams()} {
					if params["sandbox"] != want || params["approvalPolicy"] != "on-request" {
						t.Fatalf("thread start/resume lost native access or approvals: %+v", params)
					}
				}
				flag := "-s '" + want + "'"
				if sessionID != "" {
					flag = "-c 'sandbox_mode=" + want + "'"
				}
				if command := buildExecCommand(input, materialized{}); !strings.Contains(command, flag) ||
					!strings.Contains(command, "approval_policy=on-request") {
					t.Fatalf("exec/resume must agree with app-server: %s", command)
				}
				for _, explicit := range []string{"read-only", "workspace-write"} {
					input.Config[keySandbox] = explicit
					if sandbox(input) != explicit || newAppServer(input, materialized{}).sandbox != explicit {
						t.Fatal("placement replaced an explicit user restriction")
					}
				}
			}
		})
	}
}
