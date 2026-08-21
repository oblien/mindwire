// Package exec is a notification channel that runs a local command (named by NOTIFY_EXEC) with the
// notification JSON on stdin — a true local hook (desktop toast, log shipper, pager, …). It is a
// self-registering plug-in for the notify registry: importing it for its init() adds exec delivery with
// no change to the send path.
//
// TRUST BOUNDARY: the command comes from NOTIFY_EXEC in the daemon's own environment, NOT from any API
// client. Running it is the same trust as choosing the daemon binary itself; it is never settable over
// HTTP (unlike the webhook target, which is API-configurable). Each invocation is bounded by a timeout so
// a hung or slow hook can never wedge a turn, and stdout/stderr are discarded (a bounded stderr snippet
// is kept only for error reporting). The notification is delivered on stdin, never interpolated into the
// command string — no shell-injection surface from notification content.
package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
)

func init() { notify.Register(factory) }

// defaultTimeout bounds each hook invocation. Generous enough for a real hook, short enough that a hung
// command can't hold a turn's notify path open indefinitely.
const defaultTimeout = 10 * time.Second

// factory enables the exec channel only when NOTIFY_EXEC names a command (else ok=false → skipped). The
// command is daemon-owner env config, not API-mutable, so it is read directly (the Store is ignored).
func factory(notify.Store) (notify.Notifier, bool) {
	cmd := strings.TrimSpace(os.Getenv("NOTIFY_EXEC"))
	if cmd == "" {
		return nil, false
	}
	return &Notifier{Command: cmd, Timeout: defaultTimeout}, true
}

// Notifier runs Command via `bash -lc` (matching the daemon's driver convention) with the notification
// JSON piped to stdin. Timeout <= 0 means no bound (not used by the factory).
type Notifier struct {
	Command string
	Timeout time.Duration
}

var _ notify.Notifier = (*Notifier)(nil)

func (e *Notifier) Notify(ctx context.Context, n agent.Notification) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "bash", "-lc", e.Command)
	cmd.Stdin = bytes.NewReader(body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if snippet := strings.TrimSpace(stderr.String()); snippet != "" {
			return fmt.Errorf("notify exec: %w: %s", err, snippet)
		}
		return fmt.Errorf("notify exec: %w", err)
	}
	return nil
}
