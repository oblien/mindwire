package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// WebhookNotifier delivers a notification by POSTing it to a user-configured webhook URL.
// It is provider-agnostic: the daemon just fires the agent.Notification as JSON at whatever
// endpoint the client provisioned via PUT /notify/config — a serverless function, a Slack/ntfy
// relay, a push gateway (APNs/FCM), your own backend, anything that speaks HTTP. The daemon holds
// no device tokens or push credentials; shaping/fan-out is the receiver's job.
//
// It reads its target LIVE from a provider on every send, so the client can set or rotate the
// webhook at runtime. With no URL configured it is a silent no-op — notifications are optional and
// off by default.
type WebhookNotifier struct {
	// Config returns (url, channel, token): the webhook endpoint, an optional routing tag sent as
	// the X-Mindwire-Channel header, and an optional bearer token sent as Authorization.
	Config func() (url, channel, token string)
	HTTP   *http.Client
}

// NewWebhook builds a notifier that reads its target from config on each send.
func NewWebhook(config func() (url, channel, token string)) *WebhookNotifier {
	return &WebhookNotifier{Config: config, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// The webhook is the built-in core channel and self-registers with the notify registry. It is always
// ENABLED: it reads its target LIVE from the store on each send, so it is included even when currently
// unconfigured (a no-op then) and honors a later PUT /notify/config without a restart.
func init() { Register(webhookFactory) }

func webhookFactory(store Store) (Notifier, bool) { return NewWebhook(store.NotifyConfig), true }

func (w *WebhookNotifier) Notify(ctx context.Context, n agent.Notification) error {
	url, channel, token := w.Config()
	// Not configured → notifications are simply disabled: a silent no-op, not an error, so a daemon
	// with no webhook set (the default) never fails on every turn.
	if url == "" {
		return nil
	}
	// The body IS the unified notification — condition, title, body, agent, chatId, runId, actions.
	// A receiver maps that onto whatever it delivers (APNs alert, Slack message, email, …).
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if channel != "" {
		req.Header.Set("X-Mindwire-Channel", channel) // optional routing hint for the receiver
	}

	resp, err := w.HTTP.Do(req)
	if err != nil {
		log.Printf("notify: POST %s cond=%s FAILED — %v", url, n.Condition, err)
		return err
	}
	defer resp.Body.Close()
	// Drain (bounded) so the connection can be reused; keep a snippet for error reporting.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	log.Printf("notify: POST %s cond=%s → HTTP %d", url, n.Condition, resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// A non-2xx would otherwise look like a successful send.
		return fmt.Errorf("webhook HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}
	return nil
}
