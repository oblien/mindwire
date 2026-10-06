package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// A context cancellation kills the CLI process group, so exec.Wait may report
// "signal: killed" for both Stop and a configured deadline. The context is the
// authority for those cases; a raw process exit must not disguise either one.
// Apply the same normalization to streamed errors/results and the saved result.
func (r *Runner) normalizeResult(ctx context.Context, result agent.TurnResult) agent.TurnResult {
	switch ctx.Err() {
	case context.Canceled:
		if result.IsError {
			result.Text = ""
		}
		result.IsError, result.Cancelled = false, true
	case context.DeadlineExceeded:
		result.IsError, result.Cancelled = true, false
		cause := context.Cause(ctx)
		if cause != nil && cause != context.DeadlineExceeded {
			result.Text = cause.Error()
		} else {
			result.Text = "Mindwire stopped this turn because its configured execution deadline expired. You can continue this chat."
		}
	default:
		if result.IsError && strings.HasPrefix(strings.TrimSpace(result.Text), "signal: ") {
			name := agent.FirstNonEmpty(r.adapter.Meta().Name, r.adapter.ID())
			result.Text = fmt.Sprintf("%s was terminated on the workspace (%s). You can continue this chat. If it happens again, check the computer's memory and process logs.", name, strings.TrimSpace(result.Text))
		}
	}
	return result
}
