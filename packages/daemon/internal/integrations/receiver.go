package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/oblien/mindwire/packages/daemon/internal/registry"
)

const cursorKind = "integration-cursor"

type receiver struct {
	revision int64
	cancel   context.CancelFunc
}

// Cursors are private daemon records, like delivery receipts. Persist only after
// accepting an event durably; a crash can replay an event, never execute it twice.
type receiverCursor struct {
	TokenKey string `json:"tokenKey"`
	Offset   int64  `json:"offset,omitempty"`
	Session  string `json:"session,omitempty"`
	Gateway  string `json:"gateway,omitempty"`
	Sequence *int64 `json:"sequence,omitempty"`
}

type receiveError struct {
	message string
	after   time.Duration
	fatal   bool
}

func (e receiveError) Error() string { return e.message }

func (s *Service) withReceiver(c Connection) Connection {
	if c.Transport != "outbound" {
		return c
	}
	s.receiverMu.Lock()
	status, ok := s.receiverStatus[c.ID]
	s.receiverMu.Unlock()
	if !c.Enabled {
		status = ReceiverStatus{State: "paused"}
	} else if !ok {
		status = ReceiverStatus{State: "connecting"}
	}
	c.Receiver = &status
	return c
}

func (s *Service) setReceiver(c savedConnection, state, message string) {
	s.receiverMu.Lock()
	s.receiverStatus[c.ID] = ReceiverStatus{State: state, Error: message}
	s.receiverMu.Unlock()
}

// Called only while holding s.mu. A replacement waits for its old receiver to
// finish, including an outstanding poll, so there are never competing receivers.
func (s *Service) reconcileReceivers() {
	rows, err := s.db.SurfaceList(configKind)
	if err != nil {
		return
	}
	wanted := map[string]savedConnection{}
	for _, raw := range rows {
		var c savedConnection
		if json.Unmarshal(raw, &c) == nil && c.Enabled && c.Transport == "outbound" {
			wanted[c.ID] = c
		}
	}
	for id, worker := range s.receivers {
		c, ok := wanted[id]
		if !ok || c.Revision != worker.revision {
			worker.cancel()
		}
	}
	for id, c := range wanted {
		if s.receivers[id] != nil {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		worker := &receiver{revision: c.Revision, cancel: cancel}
		s.receivers[id] = worker
		s.setReceiver(c, "connecting", "")
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer cancel()
			s.receive(ctx, c)
			s.mu.Lock()
			delete(s.receivers, id)
			s.receiverMu.Lock()
			delete(s.receiverStatus, id)
			s.receiverMu.Unlock()
			s.mu.Unlock()
			s.signal()
		}()
	}
}

func (s *Service) receive(ctx context.Context, c savedConnection) {
	cursor := receiverCursor{}
	err := s.db.SurfaceGet(cursorKind, c.ID, &cursor)
	if err != nil && !errors.Is(err, registry.ErrNotFound) {
		s.setReceiver(c, "error", "Could not read the saved connection cursor. Restart the connector.")
		<-ctx.Done()
		return
	}
	tokenKey := registry.SyncKey(c.Secrets.BotToken, c.ApplicationID)
	if cursor.TokenKey != tokenKey {
		cursor = receiverCursor{TokenKey: tokenKey}
	}
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		started := time.Now()
		switch c.Provider {
		case "slack":
			err = s.receiveSlack(ctx, c)
		case "telegram":
			err = s.receiveTelegram(ctx, c, &cursor)
		case "discord":
			err = s.receiveDiscord(ctx, c, &cursor)
		}
		if ctx.Err() != nil {
			return
		}
		message := "Connection interrupted. Reconnecting; check the bot credentials and provider settings if this persists."
		delay := backoff + time.Duration(rand.Int64N(int64(time.Second)))
		var failure receiveError
		if errors.As(err, &failure) {
			message = failure.message
			if failure.fatal {
				s.setReceiver(c, "error", message)
				<-ctx.Done()
				return
			}
			delay = max(delay, failure.after)
		}
		var limited rateLimit
		if errors.As(err, &limited) {
			delay = max(delay, limited.after)
		}
		var providerErr providerHTTPError
		if errors.As(err, &providerErr) {
			if providerErr.status == 401 || providerErr.status == 403 {
				message = "The provider rejected this bot's credentials or permissions. Check its token and app settings."
			}
			if c.Provider == "telegram" && providerErr.status == 409 {
				message = "Another Telegram receiver or webhook is using this bot. Stop that receiver or connect a separate bot."
			}
		}
		s.setReceiver(c, "retrying", message)
		if !receiveWait(ctx, delay) {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = 5 * time.Second
		} else {
			backoff = min(backoff*2, time.Minute)
		}
	}
}

func receiveWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Service) saveCursor(c savedConnection, cursor *receiverCursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.load(c.ID)
	if err != nil {
		return err
	}
	if s.closed || !current.Enabled || current.Revision != c.Revision {
		return errForbidden
	}
	return s.db.SurfacePut(cursorKind, c.ID, cursor)
}

// Inbound webhooks cannot opt into this path. Only authenticated provider
// sockets/polls call it, then share the same decoder, allowlists and durable queue.
func (s *Service) receiveEvent(c savedConnection, body []byte, contentType string) (*envelope, any, error) {
	r := &http.Request{Header: http.Header{"Content-Type": []string{contentType}}}
	e, ack, err := decodeEvent(c, r, body)
	if err == nil && e != nil {
		e.AwaitingAck = c.Provider == "discord"
		if e.AwaitingAck {
			e.ProviderEventID = e.ID
		}
		err = s.accept(c, *e)
	}
	if errors.Is(err, errForbidden) || errors.Is(err, registry.ErrInvalid) {
		// A disallowed/irrelevant message is consumed without sending information
		// or letting a sender outside the allowlist poison a provider's queue.
		return nil, nil, nil
	}
	return e, ack, err
}

func (s *Service) dialProvider(ctx context.Context, raw, provider string) (*websocket.Conn, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "wss" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return nil, errors.New("invalid provider socket URL")
	}
	host := strings.ToLower(u.Hostname())
	allowed := provider == "slack" && (host == "wss.slack.com" || strings.HasSuffix(host, ".wss.slack.com") || strings.HasPrefix(host, "wss-") && strings.HasSuffix(host, ".slack.com")) || provider == "discord" && (host == "gateway.discord.gg" || strings.HasSuffix(host, ".discord.gg") && strings.HasPrefix(host, "gateway"))
	if !allowed {
		return nil, errors.New("untrusted provider socket host")
	}
	// Never follow redirects with provider socket tickets or credentials.
	client := *s.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	conn, _, err := websocket.Dial(ctx, raw, &websocket.DialOptions{HTTPClient: &client})
	if err != nil {
		return nil, errors.New("provider socket connection failed")
	}
	conn.SetReadLimit(1 << 20)
	return conn, nil
}
