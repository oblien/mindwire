package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func (s *Service) receiveTelegram(ctx context.Context, c savedConnection, cursor *receiverCursor) error {
	base := "https://api.telegram.org/bot" + c.Secrets.BotToken + "/"
	info, err := s.request(ctx, "POST", base+"getWebhookInfo", "", map[string]any{})
	if err != nil {
		return err
	}
	var webhook struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(info["result"], &webhook); err != nil {
		return err
	}
	if webhook.URL != "" {
		return receiveError{message: "This Telegram bot has a webhook. Remove it with deleteWebhook (without dropping pending updates), or use a separate bot for this connection.", fatal: true}
	}
	for {
		// Below the shared client's 15s timeout, with keep-alive between polls.
		result, err := s.request(ctx, "POST", base+"getUpdates", "", map[string]any{"offset": cursor.Offset, "limit": 100, "timeout": 10, "allowed_updates": []string{"message"}})
		if err != nil {
			return err
		}
		var updates []json.RawMessage
		if err := json.Unmarshal(result["result"], &updates); err != nil {
			return errors.New("invalid Telegram updates")
		}
		s.setReceiver(c, "connected", "")
		for _, raw := range updates {
			var update struct {
				ID *int64 `json:"update_id"`
			}
			if json.Unmarshal(raw, &update) != nil || update.ID == nil || *update.ID < 0 {
				return errors.New("invalid Telegram update ID")
			}
			if *update.ID < cursor.Offset {
				continue
			}
			if _, _, err := s.receiveEvent(c, raw, "application/json"); err != nil {
				return err
			}
			cursor.Offset = *update.ID + 1
			if err := s.saveCursor(c, cursor); err != nil {
				return err
			}
			s.signal()
		}
		// Avoid a hot loop when Telegram responds immediately with empty batches.
		if len(updates) == 0 && !receiveWait(ctx, 250*time.Millisecond) {
			return ctx.Err()
		}
	}
}
