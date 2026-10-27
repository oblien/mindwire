package acp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// SetModel prefers the negotiated stable config-option interface. Older ACP
// agents expose the model setter alongside their availableModels response.
func SetModel(ctx context.Context, c *Client, session Session, model string, setup bool) error {
	if value, ok := c.configOptions.Load(session.ID); ok {
		session.ConfigOptions = value.([]ConfigOption)
	}
	method := "session/set_model"
	params := map[string]any{"sessionId": session.ID, "modelId": model}
	found := false
	for _, option := range session.ConfigOptions {
		if option.Category == "model" || option.ID == "model" {
			method = "session/set_config_option"
			params = map[string]any{"sessionId": session.ID, "configId": option.ID, "value": model}
			found = true
			break
		}
	}
	if !found && len(session.Models.Available) == 0 {
		return errors.New("this session does not expose model selection")
	}
	var err error
	if setup {
		_, err = c.Setup(ctx, method, params)
	} else {
		_, err = c.Call(ctx, method, params)
	}
	return err
}

func Configure(ctx context.Context, c *Client, session Session, model, mode string) error {
	if model != "" {
		var err error
		session, err = AwaitModelOptions(ctx, c, session)
		if err != nil {
			return err
		}
		if err := SetModel(ctx, c, session, model, true); err != nil {
			return err
		}
	}
	if mode != "" {
		if _, err := c.Setup(ctx, "session/set_mode", map[string]string{"sessionId": session.ID, "modeId": mode}); err != nil {
			return err
		}
	}
	return nil
}

// Some agents advertise model options in a notification immediately after the
// new/resume response. Setup must retain that evidence without replaying text.
func AwaitModelOptions(ctx context.Context, c *Client, session Session) (Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		if value, ok := c.configOptions.Load(session.ID); ok {
			session.ConfigOptions = value.([]ConfigOption)
		}
		if len(sessionModels(session)) > 0 {
			return session, nil
		}
		select {
		case <-ctx.Done():
			return session, errors.New("native session did not advertise model choices")
		case _, ok := <-c.Notifications:
			if !ok {
				return session, errors.New("native agent exited while discovering models")
			}
		case req, ok := <-c.Requests:
			if !ok {
				return session, errors.New("native agent exited while discovering models")
			}
			if err := c.Reject(req.ID, -32601, "Client method unavailable during model discovery"); err != nil {
				return session, err
			}
		}
	}
}

// lineOutput handles stderr writes that split an authorization URL across chunks.
// It intentionally retains at most one bounded line and never logs its contents.
type lineOutput struct {
	mu     sync.Mutex
	data   []byte
	output func(string)
}

func (w *lineOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			w.output(string(w.data))
			w.data = nil
		} else if len(w.data) < 64*1024 {
			w.data = append(w.data, b)
		}
	}
	return len(p), nil
}

func sessionModels(session Session) []struct{ ID, Name string } {
	out := []struct{ ID, Name string }{}
	for _, m := range session.Models.Available {
		out = append(out, struct{ ID, Name string }{m.ID, m.Name})
	}
	if len(out) > 0 {
		return out
	}
	var flatten func(json.RawMessage)
	flatten = func(raw json.RawMessage) {
		var options []struct {
			Value, Name string
			Options     json.RawMessage
		}
		if json.Unmarshal(raw, &options) != nil {
			return
		}
		for _, o := range options {
			if o.Value != "" {
				out = append(out, struct{ ID, Name string }{o.Value, o.Name})
			} else {
				flatten(o.Options)
			}
		}
	}
	for _, o := range session.ConfigOptions {
		if o.Category == "model" || o.ID == "model" {
			flatten(o.Options)
		}
	}
	return out
}
