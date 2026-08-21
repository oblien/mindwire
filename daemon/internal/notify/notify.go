// Package notify is the delivery layer for agent notifications. The daemon is a dumb, provider-
// agnostic EMITTER: it POSTs an agent.Notification to a webhook URL the client provisioned via
// PUT /notify/config. Where that webhook points — a serverless function, a Slack/ntfy relay, a
// push gateway (APNs/FCM), your own backend — is entirely up to you; the daemon holds no device
// tokens or push credentials. With no webhook configured, notifications are a silent no-op.
package notify

import (
	"context"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Notifier delivers a Notification. Implementations: WebhookNotifier (the built-in HTTP emitter),
// Stream (local SSE broadcaster), and Noop (disabled).
type Notifier interface {
	Notify(ctx context.Context, n agent.Notification) error
}

// Noop drops notifications (used in tests / when delivery is intentionally off).
type Noop struct{}

func (Noop) Notify(context.Context, agent.Notification) error { return nil }
