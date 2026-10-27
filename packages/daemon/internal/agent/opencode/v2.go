package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

// V2 intentionally replaced the V1 server API. Keep its REST shapes and event
// translation here so both protocols share the existing turn/approval lifecycle.
// Reference: https://opencode.ai/v2/docs/api
func (s server) ensureV2Session(ctx context.Context, client *http.Client, base string) (string, error) {
	if s.system != "" {
		return "", fmt.Errorf("OpenCode V2 does not support the V1 per-turn system prompt override; use a native OpenCode agent definition")
	}
	model := map[string]string{"providerID": s.provider, "id": s.model}
	if id, variant, ok := strings.Cut(s.model, "#"); ok {
		model["id"], model["variant"] = id, variant
	}
	if s.resumeID != "" {
		root := base + "/api/session/" + url.PathEscape(s.resumeID)
		for _, setting := range []struct {
			key   string
			value any
			set   bool
		}{
			{"agent", s.agentName, s.agentName != ""},
			{"model", model, s.provider != "" && s.model != ""},
		} {
			if !setting.set {
				continue
			}
			body, _ := json.Marshal(map[string]any{setting.key: setting.value})
			if err := s.doJSON(ctx, client, http.MethodPost, root+"/"+setting.key, body, nil); err != nil {
				return "", err
			}
		}
		return s.resumeID, nil
	}
	body := map[string]any{"title": "mindwire"}
	if s.cwd != "" {
		body["location"] = map[string]string{"directory": s.cwd}
	}
	if s.agentName != "" {
		body["agent"] = s.agentName
	}
	if s.provider != "" && s.model != "" {
		body["model"] = model
	}
	raw, _ := json.Marshal(body)
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := s.doJSON(ctx, client, http.MethodPost, base+"/api/session", raw, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", fmt.Errorf("OpenCode V2 session response had no id")
	}
	return out.Data.ID, nil
}

func (s server) promptV2(ctx context.Context, client *http.Client, base, sid string) error {
	root := base + "/api/session/" + url.PathEscape(sid)
	if s.contextInstructions != "" {
		body, _ := json.Marshal(map[string]any{"text": s.contextInstructions, "resume": false})
		if err := s.doJSON(ctx, client, http.MethodPost, root+"/synthetic", body, nil); err != nil {
			return err
		}
	}
	body := map[string]any{"text": s.message}
	var files []map[string]any
	for _, part := range s.extraParts {
		if part["type"] != "file" {
			return fmt.Errorf("unsupported OpenCode V2 prompt part %v", part["type"])
		}
		file := map[string]any{"uri": part["url"]}
		if name, ok := part["filename"].(string); ok && name != "" {
			file["name"] = name
		}
		files = append(files, file)
	}
	if len(files) > 0 {
		body["files"] = files
	}
	raw, _ := json.Marshal(body)
	return s.doJSON(ctx, client, http.MethodPost, root+"/prompt", raw, nil)
}

type v2Stream struct {
	text  map[string]string
	tools map[string]ocPart
	usage ocMessage // aggregate step usage for this turn, not the session's lifetime
}

func newV2Stream() *v2Stream {
	return &v2Stream{text: map[string]string{}, tools: map[string]ocPart{}}
}

func legacyFrame(kind string, properties any) sseEvent {
	raw, _ := json.Marshal(properties)
	return sseEvent{Type: kind, Properties: raw}
}

func (v *v2Stream) translate(ev sseEvent, sid string, emit agent.Emit) []sseEvent {
	if ev.Type == "server.connected" {
		return []sseEvent{legacyFrame(ev.Type, map[string]any{})}
	}
	var data struct {
		SessionID string          `json:"sessionID"`
		MessageID string          `json:"assistantMessageID"`
		Ordinal   int             `json:"ordinal"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Delta     string          `json:"delta"`
		Text      string          `json:"text"`
		Input     json.RawMessage `json:"input"`
		Content   []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Cost   float64  `json:"cost"`
		Tokens ocTokens `json:"tokens"`
	}
	if json.Unmarshal(ev.Data, &data) != nil {
		return nil
	}
	if ev.Type == "form.created" {
		var p struct {
			Form v2Form `json:"form"`
		}
		if json.Unmarshal(ev.Data, &p) == nil && sid != "" && p.Form.SessionID == sid {
			emit(agent.Event{Type: agent.EventInteraction, Interaction: p.Form.interaction()})
		}
		return nil
	}
	// Ignore unrelated sessions, including events received while creating our new one.
	if sid == "" || data.SessionID != sid {
		return nil
	}
	properties := map[string]any{"sessionID": sid}
	switch ev.Type {
	case "session.text.delta", "session.text.ended", "session.reasoning.delta", "session.reasoning.ended":
		kind := "text"
		if strings.HasPrefix(ev.Type, "session.reasoning.") {
			kind = "reasoning"
		}
		id := fmt.Sprintf("%s/%s/%d", data.MessageID, kind, data.Ordinal)
		if strings.HasSuffix(ev.Type, ".delta") {
			v.text[id] += data.Delta
		} else {
			v.text[id] = data.Text
		}
		return []sseEvent{legacyFrame("message.part.updated", map[string]any{"part": ocPart{
			ID: id, SessionID: sid, MessageID: data.MessageID, Type: kind, Text: v.text[id],
		}})}
	case "session.tool.input.started":
		v.tools[data.ID] = ocPart{ID: data.ID, SessionID: sid, MessageID: data.MessageID, Type: "tool", CallID: data.ID, Tool: data.Name, State: &ocToolState{}}
	case "session.tool.called", "session.tool.success", "session.tool.failed":
		part := v.tools[data.ID]
		if part.ID == "" {
			part = ocPart{ID: data.ID, SessionID: sid, Type: "tool", CallID: data.ID, Tool: data.Name, State: &ocToolState{}}
		}
		// Each frame gets its own state so later updates cannot mutate emitted input.
		state := *part.State
		if len(data.Input) > 0 {
			state.Input = data.Input
		}
		state.Status = "running"
		if ev.Type != "session.tool.called" {
			state.Status = "completed"
			var output []string
			for _, content := range data.Content {
				if content.Type == "text" {
					output = append(output, content.Text)
				}
			}
			state.Output = strings.Join(output, "\n")
			if ev.Type == "session.tool.failed" {
				state.Status, state.Error = "error", data.Error.Message
			}
		}
		part.State = &state
		v.tools[data.ID] = part
		return []sseEvent{legacyFrame("message.part.updated", map[string]any{"part": part})}
	case "permission.asked":
		var ask struct {
			ID, SessionID, Action, Message string
			Resources                      []string
		}
		if json.Unmarshal(ev.Data, &ask) != nil {
			return nil
		}
		return []sseEvent{legacyFrame("permission.updated", ocPermission{
			ID: ask.ID, SessionID: sid, Type: ask.Action,
			Title: agent.FirstNonEmpty(ask.Message, ask.Action+": "+strings.Join(ask.Resources, ", ")),
		})}
	case "session.step.ended", "session.compaction.ended":
		v.usage.SessionID, v.usage.Role = sid, "assistant"
		v.usage.Cost += data.Cost
		v.usage.Tokens.Input += data.Tokens.Input
		v.usage.Tokens.Output += data.Tokens.Output
		v.usage.Tokens.Reasoning += data.Tokens.Reasoning
		v.usage.Tokens.Cache.Read += data.Tokens.Cache.Read
		v.usage.Tokens.Cache.Write += data.Tokens.Cache.Write
		frames := []sseEvent{legacyFrame("message.updated", map[string]any{"info": v.usage})}
		if ev.Type == "session.compaction.ended" {
			frames = append(frames, legacyFrame("session.compacted", properties))
		}
		return frames
	case "session.execution.succeeded", "session.execution.interrupted":
		return []sseEvent{legacyFrame("session.idle", properties)}
	case "session.execution.failed", "session.compaction.failed":
		properties["error"] = map[string]string{"message": agent.FirstNonEmpty(data.Error.Message, "OpenCode V2 execution failed")}
		return []sseEvent{legacyFrame("session.error", properties)}
	}
	return nil
}

type v2Field struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Custom   bool   `json:"custom"`
	Options  []struct {
		Value       string `json:"value"`
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}

type v2Form struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionID"`
	Title     string    `json:"title"`
	Fields    []v2Field `json:"fields"`
}

func (f v2Form) interaction() *agent.Interaction {
	raw, _ := json.Marshal(f.Fields)
	it := &agent.Interaction{ID: f.ID, Kind: "form", Title: f.Title, NeedsResponse: true,
		Meta: map[string]any{"opencodeForm": string(raw)}}
	for _, field := range f.Fields {
		q := agent.Question{ID: field.Key, Title: agent.FirstNonEmpty(field.Title, field.Key),
			Optional: !field.Required, MultiSelect: field.Type == "multiselect", AllowOther: field.Custom}
		for _, option := range field.Options {
			q.Options = append(q.Options, agent.Action{ID: option.Value, Label: agent.FirstNonEmpty(option.Label, option.Value), Description: option.Description})
		}
		if field.Type == "boolean" {
			q.Options = []agent.Action{{ID: "true", Label: "Yes"}, {ID: "false", Label: "No"}}
		}
		if len(q.Options) == 0 {
			q.AllowOther = true
		}
		it.Questions = append(it.Questions, q)
	}
	return it
}

func (s server) replyV2Form(ctx context.Context, client *http.Client, base, sid string, msg agent.Inbound) error {
	var fields []v2Field
	if json.Unmarshal([]byte(msg.Meta["opencodeForm"]), &fields) != nil {
		return fmt.Errorf("invalid OpenCode form")
	}
	answers := map[string]any{}
	for _, field := range fields {
		answer := msg.Answers[field.Key]
		value := answer.Text
		if len(answer.Options) > 0 {
			value = answer.Options[0]
		}
		if value == "" && len(answer.Options) == 0 && !field.Required {
			continue
		}
		switch field.Type {
		case "boolean":
			boolean, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid boolean for OpenCode form field %s", field.Key)
			}
			answers[field.Key] = boolean
		case "number", "integer":
			number, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("invalid number for OpenCode form field %s", field.Key)
			}
			answers[field.Key] = number
		case "multiselect":
			values := append([]string{}, answer.Options...)
			if answer.Text != "" {
				values = append(values, answer.Text)
			}
			answers[field.Key] = values
		default:
			answers[field.Key] = value
		}
	}
	body, _ := json.Marshal(map[string]any{"answer": answers})
	return s.doJSON(ctx, client, http.MethodPost, base+"/api/session/"+url.PathEscape(sid)+"/form/"+url.PathEscape(msg.InteractionID)+"/reply", body, nil)
}
