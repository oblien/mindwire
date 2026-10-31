package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/url"
	"runtime"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/oblien/mindwire/packages/daemon/internal/registry"
)

type gatewayPacket struct {
	Op       int             `json:"op"`
	Data     json.RawMessage `json:"d"`
	Sequence *int64          `json:"s"`
	Type     string          `json:"t"`
}

func (s *Service) receiveDiscord(ctx context.Context, c savedConnection, cursor *receiverCursor) error {
	// A configured interactions endpoint takes precedence over the Gateway. Do
	// not silently replace another deployment's endpoint or claim to be ready.
	app, err := s.request(ctx, "GET", "https://discord.com/api/v10/oauth2/applications/@me", "Bot "+c.Secrets.BotToken, nil)
	if err != nil {
		return err
	}
	var appID, endpoint string
	_ = json.Unmarshal(app["id"], &appID)
	_ = json.Unmarshal(app["interactions_endpoint_url"], &endpoint)
	if appID != c.ApplicationID {
		return receiveError{message: "The Discord bot token belongs to a different application.", fatal: true}
	}
	if endpoint != "" {
		return receiveError{message: "Clear this bot's Interactions Endpoint URL in Discord before using its outbound Gateway connection, or create a separate bot.", fatal: true}
	}
	if err := s.recoverDiscordAcks(ctx, c); err != nil {
		return err
	}
	address := cursor.Gateway
	if cursor.Session == "" || cursor.Sequence == nil {
		result, err := s.request(ctx, "GET", "https://discord.com/api/v10/gateway/bot", "Bot "+c.Secrets.BotToken, nil)
		if err != nil {
			return err
		}
		if json.Unmarshal(result["url"], &address) != nil {
			return errors.New("Discord did not return a gateway URL")
		}
		var limit struct {
			Remaining  int   `json:"remaining"`
			ResetAfter int64 `json:"reset_after"`
		}
		if json.Unmarshal(result["session_start_limit"], &limit) != nil {
			return errors.New("Discord did not return session limits")
		}
		if limit.Remaining <= 0 {
			return receiveError{message: "Discord's connection limit was reached. Waiting before reconnecting.", after: max(time.Minute, time.Duration(limit.ResetAfter)*time.Millisecond)}
		}
	}
	u, err := url.Parse(address)
	if err != nil {
		return errors.New("invalid Discord gateway")
	}
	q := u.Query()
	q.Set("v", "10")
	q.Set("encoding", "json")
	u.RawQuery = q.Encode()
	ws, err := s.dialProvider(ctx, u.String(), "discord")
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	helloCtx, helloCancel := context.WithTimeout(readCtx, 15*time.Second)
	var hello gatewayPacket
	err = wsjson.Read(helloCtx, ws, &hello)
	helloCancel()
	if err != nil {
		return err
	}
	var settings struct {
		Interval int64 `json:"heartbeat_interval"`
	}
	if hello.Op != 10 || json.Unmarshal(hello.Data, &settings) != nil || settings.Interval < 100 || settings.Interval > 300000 {
		return errors.New("invalid Discord heartbeat interval")
	}
	if cursor.Session != "" && cursor.Sequence != nil {
		err = wsjson.Write(ctx, ws, map[string]any{"op": 6, "d": map[string]any{"token": c.Secrets.BotToken, "session_id": cursor.Session, "seq": *cursor.Sequence}})
	} else {
		// Slash interactions need no privileged message-content intent. They
		// already carry the exact command text and verified invoking identity.
		err = wsjson.Write(ctx, ws, map[string]any{"op": 2, "d": map[string]any{"token": c.Secrets.BotToken, "intents": 0, "properties": map[string]string{"os": runtime.GOOS, "browser": "mindwire", "device": "mindwire"}}})
	}
	if err != nil {
		return err
	}
	type readResult struct {
		packet gatewayPacket
		err    error
	}
	incoming := make(chan readResult, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			var packet gatewayPacket
			err := wsjson.Read(readCtx, ws, &packet)
			select {
			case incoming <- readResult{packet, err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { cancel(); ws.CloseNow(); <-readDone }()
	interval := time.Duration(settings.Interval) * time.Millisecond
	timer := time.NewTimer(time.Duration(rand.Int64N(int64(interval))))
	defer timer.Stop()
	awaitingHeartbeat := false
	heartbeat := func() error {
		awaitingHeartbeat = true
		return wsjson.Write(ctx, ws, map[string]any{"op": 1, "d": cursor.Sequence})
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if awaitingHeartbeat {
				return errors.New("Discord missed a heartbeat acknowledgement")
			}
			if err := heartbeat(); err != nil {
				return err
			}
			timer.Reset(interval)
		case result := <-incoming:
			if result.err != nil {
				switch websocket.CloseStatus(result.err) {
				case 4004, 4010, 4011, 4012, 4013, 4014:
					return receiveError{message: "Discord rejected the connection. Check this bot's token and Gateway configuration.", fatal: true}
				case 4007, 4009:
					cursor.Session, cursor.Gateway, cursor.Sequence = "", "", nil
					if err := s.saveCursor(c, cursor); err != nil {
						return err
					}
				}
				return result.err
			}
			packet := result.packet
			switch packet.Op {
			case 11:
				awaitingHeartbeat = false
			case 1:
				if err := heartbeat(); err != nil {
					return err
				}
			case 7:
				return errors.New("Discord requested a reconnect")
			case 9:
				var resumable bool
				_ = json.Unmarshal(packet.Data, &resumable)
				if !resumable {
					cursor.Session, cursor.Gateway, cursor.Sequence = "", "", nil
					if err := s.saveCursor(c, cursor); err != nil {
						return err
					}
				}
				return errors.New("Discord requested a new session")
			case 0:
				switch packet.Type {
				case "READY":
					var ready struct {
						Session     string `json:"session_id"`
						Gateway     string `json:"resume_gateway_url"`
						Application struct {
							ID string `json:"id"`
						} `json:"application"`
					}
					if json.Unmarshal(packet.Data, &ready) != nil || ready.Session == "" || ready.Application.ID != c.ApplicationID {
						return errors.New("invalid Discord session identity")
					}
					cursor.Session, cursor.Gateway = ready.Session, ready.Gateway
					s.setReceiver(c, "connected", "")
				case "RESUMED":
					s.setReceiver(c, "connected", "")
				case "INTERACTION_CREATE":
					e, ack, err := s.receiveEvent(c, packet.Data, "application/json")
					if err != nil {
						return err
					}
					if e != nil {
						if err := s.ackDiscord(ctx, c, *e, ack); err != nil {
							return err
						}
					}
				}
				if packet.Sequence != nil {
					cursor.Sequence = packet.Sequence
					if err := s.saveCursor(c, cursor); err != nil {
						return err
					}
				}
			}
		}
	}
}

func (s *Service) recoverDiscordAcks(ctx context.Context, c savedConnection) error {
	rows, err := s.db.IntegrationEvents(c.ID, true)
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var e envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if e.AwaitingAck {
			if !s.allowed(c, e) {
				// Removed destinations must not strand an acknowledgement and
				// prevent all future Gateway connections for this bot.
				s.mu.Lock()
				e.AwaitingAck, e.ProviderEventID = false, ""
				e.State, e.Error = "failed", "conversation is no longer allowed"
				err := s.db.SurfacePut(eventKind, e.ID, e)
				s.mu.Unlock()
				if err != nil {
					return err
				}
				continue
			}
			// The stored ID is hashed; Discord's original snowflake is kept only
			// in the private receipt while an acknowledgement is outstanding.
			if err := s.ackDiscord(ctx, c, e, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) ackDiscord(ctx context.Context, c savedConnection, event envelope, ack any) error {
	id := event.ID
	if event.IntegrationID == "" {
		id = "evt-" + registry.SyncKey(c.ID, event.ID)[:48]
	}
	var e envelope
	if err := s.db.SurfaceGet(eventKind, id, &e); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil
		} // notification-only or ignored
		return err
	}
	if !e.AwaitingAck {
		return nil
	}
	current, err := s.load(c.ID)
	if err != nil || !current.Enabled || current.Revision != c.Revision || !s.allowed(current, e) {
		return errForbidden
	}
	if ack == nil {
		ack = map[string]any{"type": 5}
		if c.Dispatch == "host" {
			ack.(map[string]any)["data"] = map[string]int{"flags": 64}
		}
	}
	ackCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = s.request(ackCtx, "POST", "https://discord.com/api/v10/interactions/"+url.PathEscape(e.ProviderEventID)+"/"+url.PathEscape(e.InteractionToken)+"/callback", "", ack)
	var providerErr providerHTTPError
	if errors.As(err, &providerErr) && providerErr.code == 40060 {
		err = nil
	} // acknowledged before a crash
	if err != nil && !(errors.As(err, &providerErr) && providerErr.code == 10062) {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, loadErr := s.load(c.ID)
	if loadErr != nil || !current.Enabled || current.Revision != c.Revision {
		return errForbidden
	}
	e.AwaitingAck, e.ProviderEventID = false, ""
	if err != nil {
		e.State, e.Error = "failed", "Discord interaction expired before acknowledgement; send the command again."
	}
	if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
		return err
	}
	s.signal()
	return nil
}
