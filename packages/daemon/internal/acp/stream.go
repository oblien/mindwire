package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

type toolState struct {
	Name              string
	Input             json.RawMessage
	Output            string
	Started, Finished bool
}

// Stream keeps native tool identity and cumulative partial updates. A running
// tool update is not a tool result, and omitted fields do not erase earlier data.
type Stream struct {
	SessionID  string
	Text       string
	Usage      *agent.Usage
	Emit       agent.Emit
	tools      map[string]*toolState
	Background agent.TaskTracker
}

func (s *Stream) Task(task agent.TaskInfo, start bool) {
	if s.Background.Update(task, start) {
		task = s.Background.Get(task.ID)
		s.Emit(agent.Event{Type: agent.EventTask, SessionID: s.SessionID, Task: &task})
	}
}

func NewStream(id string, emit agent.Emit) *Stream {
	return &Stream{SessionID: id, Emit: emit, tools: map[string]*toolState{}}
}

func (s *Stream) Update(params json.RawMessage) {
	var envelope struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &envelope) != nil || envelope.SessionID != "" && envelope.SessionID != s.SessionID {
		return
	}
	var u struct {
		SessionUpdate             string `json:"sessionUpdate"`
		ToolCallID, Title, Status string
		RawInput, Content         json.RawMessage
		Entries                   []struct{ Content, Status, Priority string }
		Used, Size                int
	}
	if json.Unmarshal(envelope.Update, &u) != nil {
		return
	}
	emit := func(ev agent.Event) { ev.SessionID = s.SessionID; s.Emit(ev) }
	switch u.SessionUpdate {
	case "agent_message_chunk":
		if text := contentText(u.Content); text != "" {
			s.Text += text
			emit(agent.Event{Type: agent.EventText, Text: text, Delta: true})
		}
	case "agent_thought_chunk":
		if text := contentText(u.Content); text != "" {
			emit(agent.Event{Type: agent.EventThinking, Text: text, Delta: true})
		}
	case "tool_call", "tool_call_update":
		if u.ToolCallID == "" {
			return
		}
		t := s.tools[u.ToolCallID]
		if t == nil {
			t = &toolState{}
			s.tools[u.ToolCallID] = t
		}
		if t.Finished {
			return
		}
		if u.Title != "" {
			t.Name = u.Title
		}
		if len(u.RawInput) > 0 {
			t.Input = u.RawInput
		}
		if len(u.Content) > 0 {
			t.Output = contentText(u.Content)
		}
		if !t.Started {
			t.Started = true
			emit(agent.Event{Type: agent.EventToolUse, Tool: &agent.ToolEvent{ID: u.ToolCallID, Name: t.Name, Input: t.Input}})
		}
		if u.Status == "completed" || u.Status == "failed" || u.Status == "cancelled" {
			t.Finished = true
			emit(agent.Event{Type: agent.EventToolResult, Tool: &agent.ToolEvent{ID: u.ToolCallID, Name: t.Name, Output: t.Output, IsError: u.Status != "completed", Status: u.Status}})
		}
	case "plan":
		items := []agent.TodoItem{}
		for _, entry := range u.Entries {
			items = append(items, agent.TodoItem{Content: entry.Content, Status: entry.Status})
		}
		emit(agent.Event{Type: agent.EventInteraction, Interaction: &agent.Interaction{Kind: "todos", Title: "Plan", Items: items}})
	case "usage_update":
		emit(agent.Event{Type: agent.EventStatus, Meta: map[string]any{"contextUsed": u.Used, "contextWindow": u.Size}})
	case "config_option_update", "current_mode_update", "available_commands_update":
		emit(agent.Event{Type: agent.EventStatus, Meta: map[string]any{"nativeSessionUpdate": json.RawMessage(envelope.Update)}})
	}
}

func (s *Stream) Finish(failed bool) {
	for id, t := range s.tools {
		if t.Started && !t.Finished {
			t.Finished = true
			s.Emit(agent.Event{Type: agent.EventToolResult, SessionID: s.SessionID, Tool: &agent.ToolEvent{ID: id, Name: t.Name, Output: t.Output, IsError: failed, Status: "incomplete"}})
		}
	}
}

func contentText(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	var walk func(any) string
	walk = func(v any) string {
		switch x := v.(type) {
		case string:
			return x
		case []any:
			parts := []string{}
			for _, p := range x {
				if t := walk(p); t != "" {
					parts = append(parts, t)
				}
			}
			return strings.Join(parts, "\n")
		case map[string]any:
			if t, ok := x["text"].(string); ok {
				return t
			}
			if c, ok := x["content"]; ok {
				return walk(c)
			}
			if resource, ok := x["resource"]; ok {
				return walk(resource)
			}
			if diff, ok := x["newText"].(string); ok {
				return diff
			}
		}
		return ""
	}
	return walk(value)
}

type permissionRequest struct {
	ToolCall struct {
		Title string `json:"title"`
	} `json:"toolCall"`
	Options []struct{ OptionID, Name, Kind string } `json:"options"`
}

func Permission(req Message, name string) *agent.Interaction {
	var p permissionRequest
	if json.Unmarshal(req.Params, &p) != nil || len(p.Options) == 0 {
		return nil
	}
	options := []agent.Action{}
	for _, o := range p.Options {
		options = append(options, agent.Action{ID: o.OptionID, Label: agent.FirstNonEmpty(o.Name, o.OptionID)})
	}
	return &agent.Interaction{ID: string(bytes.TrimSpace(req.ID)), Kind: "approval", Title: agent.FirstNonEmpty(p.ToolCall.Title, name+" requests permission"), Options: options, NeedsResponse: true}
}

func PermissionReply(req Message, in agent.Inbound) (any, error) {
	var p permissionRequest
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return nil, err
	}
	for _, option := range p.Options {
		if option.OptionID == in.Decision {
			return map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": option.OptionID}}, nil
		}
	}
	if agent.Denied(in.Decision) {
		return map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}, nil
	}
	return nil, fmt.Errorf("choose one of the native permission options")
}
