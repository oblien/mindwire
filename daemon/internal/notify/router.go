package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// router.go is the daemon-driven notification fan-out. Where the built-in webhook (webhook.go) is a
// single client-provisioned target, the Router owns a LIST of named channels and a set of routing
// RULES; on each notification it evaluates every rule against the notification's agent / chatId /
// condition, unions the matched channels, and POSTs to each with a type-appropriate payload. It plugs
// into the same registry as every other channel — one more Notifier behind notify.Fanout — so the
// orchestrator send path is unchanged. It is additive: the legacy webhook and the file/exec channels
// keep firing independently.

// RouterStore is the slice of daemon state the Router reads: the configured channels and rules. It is
// deliberately SEPARATE from notify.Store (which only exposes NotifyConfig) so the existing Store
// interface — and its test fakes — never have to grow these methods. The session store satisfies both
// structurally; the factory type-asserts to discover whether routing is available.
type RouterStore interface {
	NotifyChannels() []agent.NotifyChannel
	NotifyRules() []agent.NotifyRule
}

// The Router self-registers as a channel. It is enabled only when the store also satisfies RouterStore
// (the real session store does; a bare notify.Store fake does not → routing is simply absent there).
// Like the webhook, it reads channels/rules LIVE on each send, so runtime CRUD takes effect with no
// restart; with no matching rule it is a silent no-op.
func init() { Register(routerFactory) }

func routerFactory(store Store) (Notifier, bool) {
	rs, ok := store.(RouterStore)
	if !ok {
		return nil, false
	}
	return &Router{Store: rs, HTTP: &http.Client{Timeout: 10 * time.Second}}, true
}

// Router delivers a notification to every channel selected by a matching, enabled rule.
type Router struct {
	Store RouterStore
	HTTP  *http.Client
}

var _ Notifier = (*Router)(nil)

func (r *Router) Notify(ctx context.Context, n agent.Notification) error {
	rules := r.Store.NotifyRules()
	if len(rules) == 0 {
		return nil
	}
	// Union the channel ids selected by every matching rule (a channel referenced by two rules is
	// delivered to once).
	want := map[string]bool{}
	for _, rule := range rules {
		if !rule.Matches(n) {
			continue
		}
		for _, id := range rule.ChannelIDs {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	// Resolve to enabled channels.
	var targets []agent.NotifyChannel
	for _, ch := range r.Store.NotifyChannels() {
		if want[ch.ID] && ch.Enabled && ch.URL != "" {
			targets = append(targets, ch)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	// Deliver concurrently — one slow or failing channel never blocks the others. Errors are joined.
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	for _, ch := range targets {
		wg.Add(1)
		go func(ch agent.NotifyChannel) {
			defer wg.Done()
			if err := DeliverOne(ctx, r.HTTP, ch, n); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("channel %s: %w", ch.ID, err))
				mu.Unlock()
			}
		}(ch)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// DeliverOne POSTs a single notification to one channel, framed for the channel's type. It is exported
// so the API /notify/channels/{id}/test handler and the Go SDK can reuse the exact delivery path a live
// notification would take. A non-2xx response is an error (a failed send must not look successful).
func DeliverOne(ctx context.Context, hc *http.Client, ch agent.NotifyChannel, n agent.Notification) error {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	if ch.URL == "" {
		return errors.New("channel has no url")
	}
	body, err := shape(ch.Type, n)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range ch.Headers {
		req.Header.Set(k, v)
	}
	if ch.Token != "" {
		req.Header.Set("Authorization", "Bearer "+ch.Token)
	}
	// HMAC signing is meaningful only for the raw-JSON webhook type (a receiver you control verifies the
	// signature). Slack/Discord/Telegram won't read it, so we don't add it there.
	if ch.Type == agent.ChannelWebhook && ch.Secret != "" {
		mac := hmac.New(sha256.New, []byte(ch.Secret))
		mac.Write(body)
		req.Header.Set("X-Mindwire-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("notify/router: POST %s type=%s cond=%s FAILED — %v", ch.URL, ch.Type, n.Condition, err)
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	log.Printf("notify/router: POST %s type=%s cond=%s → HTTP %d", ch.URL, ch.Type, n.Condition, resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}
	return nil
}

// shape frames the notification as the request body for a channel type. webhook = the raw unified
// Notification JSON (a receiver you own maps it however it likes). slack/discord/telegram = a single
// text field in each provider's own key so a channel URL can point straight at an incoming webhook /
// bot sendMessage endpoint with no relay in between.
func shape(t agent.NotifyChannelType, n agent.Notification) ([]byte, error) {
	switch t {
	case agent.ChannelSlack, agent.ChannelTelegram:
		return json.Marshal(map[string]string{"text": messageText(n)})
	case agent.ChannelDiscord:
		return json.Marshal(map[string]string{"content": messageText(n)})
	case agent.ChannelWebhook, "":
		// "" (unset) defaults to the raw webhook shape — the most general, receiver-defined mapping.
		return json.Marshal(n)
	default:
		return nil, fmt.Errorf("unknown channel type %q", t)
	}
}

// messageText renders a notification as one human-readable string for the chat providers.
func messageText(n agent.Notification) string {
	title := n.Title
	if title == "" {
		title = string(n.Condition)
	}
	if n.Body == "" {
		return title
	}
	return title + "\n" + n.Body
}
