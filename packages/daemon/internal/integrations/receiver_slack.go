package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/coder/websocket/wsjson"
)

func (s *Service) receiveSlack(ctx context.Context, c savedConnection) error {
	result, err := s.request(ctx, "POST", "https://slack.com/api/apps.connections.open", "Bearer "+c.Secrets.AppToken, map[string]any{})
	if err != nil {
		return err
	}
	var address string
	if json.Unmarshal(result["url"], &address) != nil {
		return errors.New("Slack did not return a socket URL")
	}
	ws, err := s.dialProvider(ctx, address, "slack")
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	liveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-liveCtx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(liveCtx, 10*time.Second)
				err := ws.Ping(pingCtx)
				pingCancel()
				if err != nil {
					ws.CloseNow()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	s.setReceiver(c, "connected", "")
	for {
		var packet struct {
			Type    string          `json:"type"`
			ID      string          `json:"envelope_id"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := wsjson.Read(ctx, ws, &packet); err != nil {
			return err
		}
		if packet.Type == "disconnect" {
			return errors.New("Slack requested a reconnect")
		}
		if packet.ID == "" {
			continue
		}
		body, contentType := []byte(packet.Payload), "application/json"
		if packet.Type == "slash_commands" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(body, &fields); err != nil {
				return err
			}
			values := url.Values{}
			for _, name := range []string{"command", "trigger_id", "channel_id", "user_id", "team_id", "text"} {
				var value string
				_ = json.Unmarshal(fields[name], &value)
				values.Set(name, value)
			}
			body, contentType = []byte(values.Encode()), "application/x-www-form-urlencoded"
		}
		if packet.Type == "slash_commands" || packet.Type == "events_api" {
			if _, _, err := s.receiveEvent(c, body, contentType); err != nil {
				return err
			}
		}
		// Persist first, acknowledge second. Slack redelivery uses the same ID.
		if err := wsjson.Write(ctx, ws, map[string]string{"envelope_id": packet.ID}); err != nil {
			return err
		}
		s.signal()
	}
}
