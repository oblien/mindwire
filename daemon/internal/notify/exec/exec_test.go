package exec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The exec channel delivers the notification as JSON on the command's stdin (never interpolated into
// the command string). A `cat > file` hook lets us assert the payload the command actually received.
func TestExecNotifierPipesJSONToStdin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "captured.json")
	e := &Notifier{Command: "cat > " + out, Timeout: 5 * time.Second}

	n := agent.Notification{Condition: agent.Finished, Title: "hello", ChatID: "c1", RunID: "r1"}
	if err := e.Notify(context.Background(), n); err != nil {
		t.Fatalf("notify: %v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	var got agent.Notification
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("stdin was not the notification JSON: %v\n%s", err, body)
	}
	if got.Title != "hello" || got.ChatID != "c1" || got.RunID != "r1" {
		t.Errorf("hook received %+v, want the emitted notification", got)
	}
}

// A non-zero exit is surfaced as an error (with a stderr snippet), never silently swallowed.
func TestExecNotifierNonZeroExitErrors(t *testing.T) {
	e := &Notifier{Command: "echo boom >&2; exit 3", Timeout: 5 * time.Second}
	err := e.Notify(context.Background(), agent.Notification{Condition: agent.Finished})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected an error carrying the stderr snippet, got %v", err)
	}
}

// A hook that outruns the timeout is killed and reported, so a hung command can't wedge a turn.
func TestExecNotifierTimeout(t *testing.T) {
	e := &Notifier{Command: "sleep 5", Timeout: 100 * time.Millisecond}
	start := time.Now()
	err := e.Notify(context.Background(), agent.Notification{Condition: agent.Finished})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("timeout not enforced: took %v", elapsed)
	}
}

// factory enables the channel only when NOTIFY_EXEC is set.
func TestFactoryEnvGated(t *testing.T) {
	t.Setenv("NOTIFY_EXEC", "")
	if _, ok := factory(nil); ok {
		t.Error("factory should be disabled without NOTIFY_EXEC")
	}
	t.Setenv("NOTIFY_EXEC", "true")
	n, ok := factory(nil)
	if !ok || n == nil {
		t.Fatal("factory should be enabled with NOTIFY_EXEC set")
	}
	if en, _ := n.(*Notifier); en == nil || en.Command != "true" || en.Timeout <= 0 {
		t.Errorf("factory should carry the command + a bounded timeout, got %+v", n)
	}
}
