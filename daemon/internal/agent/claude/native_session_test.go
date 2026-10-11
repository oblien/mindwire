package claude

import (
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestNativeClaudeActivityOwnsIdleTerminalAndNeverAdvertisesControls(t *testing.T) {
	q := agent.NativeSessionQuery{SessionID: "claude-session", CWD: t.TempDir()}
	for _, status := range []string{"busy", "waiting", "idle"} {
		activity, err := claudeSessionActivity([]nativeAgentRow{{SessionID: q.SessionID, CWD: q.CWD, PID: 21, Status: status, StartedAt: 1720000000000}}, q)
		if err != nil {
			t.Fatal(err)
		}
		if activity.Owner != "native" || !activity.Capabilities.Observe || activity.Capabilities.Input || activity.Capabilities.Interrupt || activity.Capabilities.Respond || activity.Reason != "terminal_handoff_required" {
			t.Fatalf("unsafe Claude capabilities: %+v", activity)
		}
	}
	activity, err := claudeSessionActivity(nil, q)
	if err != nil || activity.Owner != "none" || activity.Capabilities.Observe {
		t.Fatalf("closed terminal: %+v %v", activity, err)
	}
	_, err = claudeSessionActivity([]nativeAgentRow{{SessionID: q.SessionID, CWD: t.TempDir(), PID: 21}}, q)
	if err == nil {
		t.Fatal("different project accepted")
	}
}
