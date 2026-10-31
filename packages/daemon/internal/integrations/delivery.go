package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/registry"
)

type rateLimit struct{ after time.Duration }

func (e rateLimit) Error() string { return "provider rate limit; retry later" }

type providerHTTPError struct{ status, code int }

func (e providerHTTPError) Error() string { return fmt.Sprintf("provider returned HTTP %d", e.status) }

// request deliberately returns provider error codes, never raw bodies or URLs:
// Telegram URLs and Discord interaction URLs contain credentials.
func (s *Service) request(ctx context.Context, method, endpoint, token string, payload any) (map[string]json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("invalid provider request")
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		resp, err := s.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("provider request failed")
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, errors.New("could not read provider response")
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(raw, &result); len(bytes.TrimSpace(raw)) > 0 && err != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil, errors.New("provider returned an invalid JSON response")
		}
		if resp.StatusCode == 429 {
			after, _ := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64)
			if after <= 0 {
				_ = json.Unmarshal(result["retry_after"], &after)
			}
			if after <= 0 {
				var p struct {
					Retry int `json:"retry_after"`
				}
				_ = json.Unmarshal(result["parameters"], &p)
				after = float64(p.Retry)
			}
			if after <= 0 {
				after = 1
			}
			if after > 30 {
				return nil, rateLimit{time.Duration(after * float64(time.Second))}
			}
			timer := time.NewTimer(time.Duration(after * float64(time.Second)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		// A fast local turn can reach Discord before its deferred HTTP response
		// has propagated. Retry only the original interaction response in that
		// short window; unrelated destination errors remain permanent failures.
		if resp.StatusCode == 404 && strings.HasPrefix(endpoint, "https://discord.com/api/v10/webhooks/") && strings.HasSuffix(endpoint, "/messages/@original") && attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			var code int
			_ = json.Unmarshal(result["code"], &code)
			return nil, providerHTTPError{status: resp.StatusCode, code: code}
		}
		var ok bool
		if value, exists := result["ok"]; exists && json.Unmarshal(value, &ok) == nil && !ok {
			var code string
			_ = json.Unmarshal(result["error"], &code)
			if code == "message_not_modified" {
				return result, nil
			}
			return nil, errors.New("provider rejected the message; check bot permissions and destination")
		}
		return result, nil
	}
	return nil, errors.New("provider rate limit persisted after retries")
}

func chunks(text string, limit int) []string {
	out := []string{}
	// Providers count supplementary characters conservatively as two UTF-16
	// units. Split on rune boundaries without exceeding their message budget.
	start, units := 0, 0
	for offset, r := range text {
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > limit && offset > start {
			out = append(out, text[start:offset])
			start = offset
			units = 0
		}
		units += width
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

func (s *Service) reply(ctx context.Context, c savedConnection, e *envelope, text string) error {
	// Re-read immediately before delivery so disconnecting or disabling revokes it.
	current, err := s.load(c.ID)
	if err != nil {
		return err
	}
	if !current.Enabled {
		return errors.New("integration is disabled")
	}
	if !slices.Contains(current.ChannelIDs, e.ChannelID) || e.UserID != "" && !s.allowed(current, *e) {
		return errForbidden
	}
	c = current
	limit := 3500
	if c.Provider == "discord" {
		limit = 1900
	}
	parts := chunks(text, limit)
	if len(parts) == 0 {
		return nil
	}
	for i, part := range parts {
		messageID := e.MessageID
		if i > 0 {
			messageID = ""
		}
		var result map[string]json.RawMessage
		switch c.Provider {
		case "slack":
			endpoint := "https://slack.com/api/chat.postMessage"
			payload := map[string]any{"channel": e.ChannelID, "text": part, "unfurl_links": false, "unfurl_media": false, "parse": "none"}
			if c.Dispatch == "host" {
				endpoint = "https://slack.com/api/chat.postEphemeral"
				payload["user"] = e.UserID
				payload["mrkdwn"] = false
				if e.ThreadID != "" {
					payload["thread_ts"] = e.ThreadID
				}
			} else if messageID != "" {
				endpoint = "https://slack.com/api/chat.update"
				payload["ts"] = messageID
			} else if e.ThreadID != "" {
				payload["thread_ts"] = e.ThreadID
			}
			result, err = s.request(ctx, "POST", endpoint, "Bearer "+c.Secrets.BotToken, payload)
			if err == nil && i == 0 {
				if c.Dispatch == "host" {
					_ = json.Unmarshal(result["message_ts"], &e.MessageID)
				} else {
					_ = json.Unmarshal(result["ts"], &e.MessageID)
				}
			}
		case "telegram":
			method := "sendMessage"
			payload := map[string]any{"chat_id": e.ChannelID, "text": part, "link_preview_options": map[string]bool{"is_disabled": true}}
			if messageID != "" {
				method = "editMessageText"
				payload["message_id"], _ = strconv.ParseInt(messageID, 10, 64)
			} else if e.ThreadID != "" {
				payload["message_thread_id"], _ = strconv.ParseInt(e.ThreadID, 10, 64)
			}
			result, err = s.request(ctx, "POST", "https://api.telegram.org/bot"+c.Secrets.BotToken+"/"+method, "", payload)
			if err == nil && i == 0 {
				var value struct {
					ID int64 `json:"message_id"`
				}
				_ = json.Unmarshal(result["result"], &value)
				if value.ID != 0 {
					e.MessageID = strconv.FormatInt(value.ID, 10)
				}
			}
		case "discord":
			endpoint := "https://discord.com/api/v10/channels/" + url.PathEscape(e.ChannelID) + "/messages"
			method := "POST"
			token := "Bot " + c.Secrets.BotToken
			if e.InteractionToken != "" {
				token = ""
				endpoint = "https://discord.com/api/v10/webhooks/" + url.PathEscape(c.ApplicationID) + "/" + url.PathEscape(e.InteractionToken)
				if i == 0 {
					method = "PATCH"
					endpoint += "/messages/@original"
				} else {
					endpoint += "?wait=true"
				}
			} else if messageID != "" {
				method = "PATCH"
				endpoint += "/" + url.PathEscape(messageID)
			}
			payload := map[string]any{"content": part, "allowed_mentions": map[string]any{"parse": []string{}}}
			if c.Dispatch == "host" && e.InteractionToken != "" && method == "POST" {
				payload["flags"] = 64
			}
			result, err = s.request(ctx, method, endpoint, token, payload)
			if err == nil && i == 0 {
				_ = json.Unmarshal(result["id"], &e.MessageID)
			}
		case "teams":
			var token string
			token, err = s.teamsToken(ctx, c)
			if err != nil {
				return err
			}
			endpoint := strings.TrimRight(c.ServiceURL, "/") + "/v3/conversations/" + url.PathEscape(e.ChannelID) + "/activities"
			method := "POST"
			if messageID != "" {
				method = "PUT"
				endpoint += "/" + url.PathEscape(messageID)
			}
			result, err = s.request(ctx, method, endpoint, "Bearer "+token, map[string]any{"type": "message", "text": part, "textFormat": "plain", "from": map[string]string{"id": c.ApplicationID}, "conversation": map[string]string{"id": e.ChannelID}})
			if err == nil && i == 0 {
				_ = json.Unmarshal(result["id"], &e.MessageID)
			}
		}
		if err != nil {
			return err
		}
		if i == 0 && e.MessageID == "" {
			return errors.New("provider did not return a message receipt")
		}
		if i == 0 && e.ID != "" {
			e.DeliveredText = part
			if err := s.db.SurfacePut(eventKind, e.ID, e); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) streamReply(c savedConnection, e *envelope) error {
	// Snapshotting the shared reducer coalesces token events and survives a slow
	// provider. There is no polling of harness history or unbounded token queue.
	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	_, events, closed, cancel := s.hub.SubscribeRetainedAfter(e.RunID, 0)
	defer cancel()
	last := e.DeliveredText
	for {
		snapshot, exists := s.hub.Snapshot(e.RunID)
		text := ""
		finished := closed
		if exists {
			var b strings.Builder
			for _, part := range snapshot.Parts {
				if part.Type == "text" {
					b.WriteString(part.Text)
				}
				if part.Interaction != nil && part.Interaction.NeedsResponse {
					b.WriteString("\nOpen this conversation in Mindwire to respond to the pending approval or question.")
				}
			}
			text = b.String()
			finished = finished || snapshot.Result != nil || snapshot.Error != ""
			if snapshot.Error != "" {
				text += "\nThe task failed. Open Mindwire for details."
			}
		}
		if !exists || finished {
			task, err := s.bots.Task(registry.Bot{}, c.BotID, e.RunID)
			if err != nil {
				return err
			}
			finished = task.Run.Status != "running"
			if text == "" {
				text = task.Output
			}
			if text == "" && finished {
				text = "Task " + task.Run.Status + ". Open Mindwire to view the conversation."
			}
		}
		limit := 3500
		if c.Provider == "discord" {
			limit = 1900
		}
		if !finished {
			if parts := chunks(text, limit); len(parts) > 0 {
				text = parts[0]
			}
		}
		if text != "" && text != last {
			if err := s.reply(s.ctx, c, e, text); err != nil {
				return err
			}
			last = text
		}
		if finished {
			return nil
		}
		// Events only wake completion; all partial text is coalesced on the ticker.
	waiting:
		for {
			select {
			case <-s.ctx.Done():
				return s.ctx.Err()
			case _, ok := <-events:
				if !ok {
					closed = true
					events = nil
					break waiting
				}
			case <-ticker.C:
				break waiting
			}
		}
	}
}
