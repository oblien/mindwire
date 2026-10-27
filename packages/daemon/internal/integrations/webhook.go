package integrations

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var errForbidden = errors.New("sender or channel is not allowed")

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// Webhook is deliberately outside daemon bearer authentication. Each provider's
// signature is checked against the raw body before any event is acknowledged.
func (s *Service) Webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	c, err := s.load(r.PathValue("id"))
	if err != nil || !c.Enabled {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	if !s.verify(r, c, body) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	e, ack, err := decodeEvent(c, r, body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid provider event"})
		return
	}
	if e != nil {
		if err = s.accept(c, *e); err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, errForbidden) {
				status = http.StatusForbidden
			}
			writeJSON(w, status, map[string]string{"error": publicError(err, c.Secrets)})
			return
		}
		if c.Mode == "notifications" && c.Provider == "discord" {
			ack = map[string]any{"type": 4, "data": map[string]any{"content": "This connection accepts notifications only.", "flags": 64}}
		}
	}
	if ack == nil {
		ack = map[string]bool{"ok": true}
	}
	writeJSON(w, http.StatusOK, ack)
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	if e != nil {
		s.signal()
	}
}

func recent(raw string) bool {
	t, err := strconv.ParseInt(raw, 10, 64)
	return err == nil && t >= time.Now().Unix()-300 && t <= time.Now().Unix()+300
}

func (s *Service) verify(r *http.Request, c savedConnection, body []byte) bool {
	switch c.Provider {
	case "slack":
		ts := r.Header.Get("X-Slack-Request-Timestamp")
		if !recent(ts) {
			return false
		}
		mac := hmac.New(sha256.New, []byte(c.Secrets.SigningSecret))
		mac.Write([]byte("v0:" + ts + ":"))
		mac.Write(body)
		return hmac.Equal([]byte("v0="+hex.EncodeToString(mac.Sum(nil))), []byte(r.Header.Get("X-Slack-Signature")))
	case "telegram":
		return subtle.ConstantTimeCompare([]byte(c.Secrets.WebhookSecret), []byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"))) == 1
	case "discord":
		ts := r.Header.Get("X-Signature-Timestamp")
		if !recent(ts) {
			return false
		}
		key, err := hex.DecodeString(c.Secrets.PublicKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return false
		}
		signature, err := hex.DecodeString(r.Header.Get("X-Signature-Ed25519"))
		if err != nil {
			return false
		}
		return ed25519.Verify(key, append([]byte(ts), body...), signature)
	case "teams":
		return s.verifyTeams(r.Context(), c, r.Header.Get("Authorization"), body) == nil
	}
	return false
}

func decodeEvent(c savedConnection, r *http.Request, body []byte) (*envelope, any, error) {
	switch c.Provider {
	case "slack":
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			values, err := url.ParseQuery(string(body))
			if err != nil {
				return nil, nil, err
			}
			if values.Get("command") != c.Command {
				return nil, nil, nil
			}
			return &envelope{Receipt: Receipt{ID: values.Get("trigger_id"), ChannelID: values.Get("channel_id"), UserID: values.Get("user_id")}, Workspace: values.Get("team_id"), Text: values.Get("text"), Command: true}, map[string]string{"response_type": "ephemeral", "text": "Request received."}, nil
		}
		var event struct {
			Type      string `json:"type"`
			Challenge string `json:"challenge"`
			ID        string `json:"event_id"`
			Team      string `json:"team_id"`
			Event     struct {
				Type        string `json:"type"`
				Subtype     string `json:"subtype"`
				BotID       string `json:"bot_id"`
				User        string `json:"user"`
				Channel     string `json:"channel"`
				Text        string `json:"text"`
				TS          string `json:"ts"`
				Thread      string `json:"thread_ts"`
				ChannelType string `json:"channel_type"`
			} `json:"event"`
		}
		if err := json.Unmarshal(body, &event); err != nil {
			return nil, nil, err
		}
		if event.Type == "url_verification" {
			return nil, map[string]string{"challenge": event.Challenge}, nil
		}
		v := event.Event
		if event.Type != "event_callback" || (v.Type != "message" && v.Type != "app_mention") || v.BotID != "" || v.Subtype != "" {
			return nil, nil, nil
		}
		thread := v.Thread
		if thread == "" && v.ChannelType != "im" && v.ChannelType != "mpim" {
			thread = v.TS
		}
		// Slack may emit message and app_mention for the same message.
		id := event.ID
		if v.TS != "" {
			id = event.Team + ":" + v.Channel + ":" + v.TS
		}
		text := stripMention(v.Text)
		return &envelope{Receipt: Receipt{ID: id, ChannelID: v.Channel, UserID: v.User}, Workspace: event.Team, ThreadID: thread, Text: text}, nil, nil
	case "telegram":
		var update struct {
			ID      int64 `json:"update_id"`
			Message *struct {
				ID     int64  `json:"message_id"`
				Thread int64  `json:"message_thread_id"`
				Text   string `json:"text"`
				From   struct {
					ID  int64 `json:"id"`
					Bot bool  `json:"is_bot"`
				} `json:"from"`
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		}
		if err := json.Unmarshal(body, &update); err != nil {
			return nil, nil, err
		}
		if update.Message == nil || update.Message.From.Bot {
			return nil, nil, nil
		}
		m := update.Message
		text := m.Text
		command := false
		if strings.HasPrefix(text, "/") {
			head, tail, _ := strings.Cut(text, " ")
			name, _, _ := strings.Cut(head, "@")
			if name == c.Command {
				command = true
				text = tail
			}
		}
		thread := ""
		if m.Thread != 0 {
			thread = strconv.FormatInt(m.Thread, 10)
		}
		return &envelope{Receipt: Receipt{ID: strconv.FormatInt(update.ID, 10), ChannelID: strconv.FormatInt(m.Chat.ID, 10), UserID: strconv.FormatInt(m.From.ID, 10)}, ThreadID: thread, Text: text, Command: command}, nil, nil
	case "discord":
		var interaction struct {
			ID          string `json:"id"`
			Type        int    `json:"type"`
			Token       string `json:"token"`
			Application string `json:"application_id"`
			Guild       string `json:"guild_id"`
			Channel     string `json:"channel_id"`
			User        struct {
				ID  string `json:"id"`
				Bot bool   `json:"bot"`
			} `json:"user"`
			Member struct {
				User struct {
					ID  string `json:"id"`
					Bot bool   `json:"bot"`
				} `json:"user"`
			} `json:"member"`
			Data struct {
				Name    string `json:"name"`
				Options []struct {
					Name  string          `json:"name"`
					Value json.RawMessage `json:"value"`
				} `json:"options"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &interaction); err != nil {
			return nil, nil, err
		}
		if interaction.Type == 1 {
			return nil, map[string]int{"type": 1}, nil
		}
		if interaction.Application != c.ApplicationID {
			return nil, nil, errForbidden
		}
		if interaction.Type != 2 || interaction.Data.Name != strings.TrimPrefix(c.Command, "/") {
			return nil, map[string]any{"type": 4, "data": map[string]string{"content": "Unknown command."}}, nil
		}
		user := interaction.Member.User.ID
		if user == "" {
			user = interaction.User.ID
		}
		if interaction.Member.User.Bot || interaction.User.Bot {
			return nil, nil, nil
		}
		text := ""
		for _, option := range interaction.Data.Options {
			if option.Name == "message" || option.Name == "text" {
				_ = json.Unmarshal(option.Value, &text)
			}
		}
		ack := map[string]any{"type": 5}
		if c.Dispatch == "host" {
			ack["data"] = map[string]int{"flags": 64}
		}
		return &envelope{Receipt: Receipt{ID: interaction.ID, ChannelID: interaction.Channel, UserID: user}, Workspace: interaction.Guild, Text: text, Command: true, InteractionToken: interaction.Token}, ack, nil
	case "teams":
		var activity struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Text    string `json:"text"`
			Channel string `json:"channelId"`
			From    struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			} `json:"from"`
			Conversation struct {
				ID string `json:"id"`
			} `json:"conversation"`
			ChannelData struct {
				Tenant struct {
					ID string `json:"id"`
				} `json:"tenant"`
			} `json:"channelData"`
		}
		if err := json.Unmarshal(body, &activity); err != nil {
			return nil, nil, err
		}
		if activity.Type != "message" || activity.Channel != "msteams" || activity.From.Role == "bot" {
			return nil, nil, nil
		}
		text := strings.TrimSpace(activity.Text)
		if strings.HasPrefix(text, "<at>") {
			if _, rest, ok := strings.Cut(text, "</at>"); ok {
				text = strings.TrimSpace(rest)
			}
		}
		return &envelope{Receipt: Receipt{ID: activity.ID, ChannelID: activity.Conversation.ID, UserID: activity.From.ID}, Workspace: activity.ChannelData.Tenant.ID, Text: text}, nil, nil
	}
	return nil, nil, invalid("unsupported provider")
}

func stripMention(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<@") {
		if _, rest, ok := strings.Cut(text, ">"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return text
}
