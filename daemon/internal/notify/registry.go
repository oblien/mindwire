package notify

import (
	"context"
	"errors"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// registry.go turns notification delivery into a pluggable, N-way fan-out — the same self-registration
// pattern the agent adapters use. A channel is one Factory registered from its package's init(); adding a
// channel is one new file + (for a sub-package channel) one blank import in main, with ZERO changes to the
// send path or the orchestrator. Fanout composes the enabled channels behind the single notify.Notifier
// the orchestrator already calls.

// Store is the subset of daemon state a notifier factory may read to configure its channel. The session
// store satisfies it structurally (no import cycle). The webhook channel reads its runtime-configurable
// target from it; env-configured channels (notify/file, notify/exec) ignore it and read their own
// os.Getenv config — deliberately keeping local-trust hooks OUT of API-mutable state.
type Store interface {
	NotifyConfig() (url, channel, token string)
}

// Factory builds a Notifier from daemon state, reporting whether the channel is ENABLED. ok=false means
// "not configured — skip" (e.g. no NOTIFY_FILE set), so All can omit it rather than fan out to a no-op.
type Factory func(Store) (Notifier, bool)

// factories is the global channel registry, appended to by Register from each channel's init().
var factories []Factory

// Register adds a notifier factory. The webhook registers from this package; sub-package channels
// (notify/file, notify/exec) register when blank-imported by main.
func Register(f Factory) { factories = append(factories, f) }

// All instantiates every registered channel that is enabled for the given store, in registration order
// (deterministic: webhook first, then blank-import order). Disabled channels (ok=false) and nil notifiers
// are omitted, so the result is safe to hand straight to Fanout.
func All(store Store) []Notifier {
	out := make([]Notifier, 0, len(factories))
	for _, f := range factories {
		if n, ok := f(store); ok && n != nil {
			out = append(out, n)
		}
	}
	return out
}

// Fanout delivers one notification to N channels behind a single Notifier, so the send path never grows
// a branch per channel. It calls EVERY channel — one channel's failure never blocks the others — and
// joins their errors (nil when all succeed). An empty fan-out is a silent no-op.
type Fanout []Notifier

var _ Notifier = Fanout(nil)

func (f Fanout) Notify(ctx context.Context, n agent.Notification) error {
	var errs []error
	for _, ch := range f {
		if ch == nil {
			continue
		}
		if err := ch.Notify(ctx, n); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
