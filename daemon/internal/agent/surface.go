package agent

import (
	"encoding/json"
	"strings"
)

// SurfaceToolAction is shared by live events and saved tool cards. Images are
// durable artifact references; base64 image/clipboard content stays out of feeds.
type SurfaceToolAction struct {
	SurfaceID string              `json:"surfaceId"`
	Operation string              `json:"operation"`
	SessionID string              `json:"sessionId,omitempty"`
	ReceiptID string              `json:"receiptId,omitempty"`
	Status    string              `json:"status,omitempty"`
	Capture   *SurfaceToolCapture `json:"capture,omitempty"`
}
type SurfaceToolCapture struct {
	ID         string `json:"id"`
	ArtifactID string `json:"artifactId"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

func NormalizeSurfaceTool(tool *ToolEvent) {
	if tool == nil {
		return
	}
	known := strings.Contains(tool.Name, "mindwire_desktop")
	if tool.Action != nil && tool.Action.MCP != nil {
		known = known || tool.Action.MCP.Server == "mindwire_desktop"
	}
	if !known {
		return
	}
	tool.Input = surfaceCardInput(tool.Input)
	if tool.Action == nil {
		tool.Action = &ToolAction{Kind: KindMCP, MCP: &MCPCall{Server: "mindwire_desktop"}}
	} else {
		copy := *tool.Action
		tool.Action = &copy
	}
	op := "desktop"
	for _, name := range []string{"status", "open", "capture", "action", "release"} {
		if strings.Contains(tool.Name, "surface_"+name) {
			op = name
		}
	}
	view := &SurfaceToolAction{SurfaceID: "desktop", Operation: op}
	if tool.Action.Surface != nil {
		copy := *tool.Action.Surface
		view = &copy
	}
	if payload := surfaceOutputPayload(tool.Output); payload != nil {
		if operation, ok := payload["operation"].(string); ok {
			view.Operation = operation
		}
		if session, ok := payload["session"].(map[string]any); ok {
			view.SessionID, _ = session["id"].(string)
		}
		if receipt, ok := payload["receipt"].(map[string]any); ok {
			view.ReceiptID, _ = receipt["id"].(string)
			view.Status, _ = receipt["status"].(string)
			if kind, ok := receipt["kind"].(string); ok {
				view.Operation = kind
			}
		}
		if capture, ok := payload["capture"].(map[string]any); ok {
			var parsed struct {
				ID         string
				ArtifactID string
				Geometry   struct{ Width, Height int }
			}
			encoded, _ := json.Marshal(capture)
			if json.Unmarshal(encoded, &parsed) == nil && parsed.ArtifactID != "" {
				view.Capture = &SurfaceToolCapture{ID: parsed.ID, ArtifactID: parsed.ArtifactID, Width: parsed.Geometry.Width, Height: parsed.Geometry.Height}
			}
		}
		tool.Action.Surface = view
		// Only the compact, canonical reference is published or stored. The MCP
		// client already received the full image and clipboard data separately.
		encoded, _ := json.Marshal(view)
		tool.Output = string(encoded)
	}
	if tool.Action.Surface == nil {
		tool.Action.Surface = view
	}
	title := map[string]string{"status": "Check desktop", "open": "Open desktop", "capture": "Capture desktop", "action": "Desktop input", "click": "Click desktop", "drag": "Drag on desktop", "scroll": "Scroll desktop", "pointer": "Move pointer", "key": "Press keys", "text": "Type text", "clipboard_read": "Read desktop clipboard", "clipboard_write": "Write desktop clipboard", "release": "Release desktop"}[view.Operation]
	if title == "" {
		title = "Desktop"
	}
	tool.Action.Title = title
}

func surfaceOutputPayload(output string) map[string]any {
	var raw any
	if json.Unmarshal([]byte(output), &raw) == nil {
		if payload := surfacePayload(raw, 0); payload != nil {
			return payload
		}
	}
	// Native Codex wraps saved results with "Wall time / Output" and renders
	// mixed text/image content as JSON followed by an Image label. Recover the
	// canonical JSON line before that presentation loses its artifact reference.
	for _, line := range strings.Split(output, "\n") {
		if json.Unmarshal([]byte(line), &raw) == nil {
			if payload := surfacePayload(raw, 0); payload != nil {
				return payload
			}
		}
	}
	return nil
}

func surfacePayload(value any, depth int) map[string]any {
	if depth > 8 {
		return nil
	}
	switch value := value.(type) {
	case map[string]any:
		if payload, ok := value["mindwireSurface"].(map[string]any); ok {
			return payload
		}
		for _, key := range []string{"structuredContent", "structured_content", "content", "result", "output", "text"} {
			if payload := surfacePayload(value[key], depth+1); payload != nil {
				return payload
			}
		}
	case []any:
		for _, part := range value {
			if payload := surfacePayload(part, depth+1); payload != nil {
				return payload
			}
		}
	case string:
		var inner any
		if json.Unmarshal([]byte(value), &inner) == nil {
			return surfacePayload(inner, depth+1)
		}
	}
	return nil
}

func NormalizeSurfaceHistory(messages []Message) []Message {
	out := append([]Message(nil), messages...)
	for i := range out {
		out[i].Parts = append([]Part(nil), out[i].Parts...)
		for j, part := range out[i].Parts {
			if part.Tool == nil {
				continue
			}
			tool := *part.Tool
			event := ToolEvent{ID: tool.ID, Name: tool.Name, Input: tool.Input, Output: tool.Output, IsError: tool.IsError, Action: tool.Action}
			NormalizeSurfaceTool(&event)
			if event.Input != nil {
				tool.Input, _ = json.Marshal(event.Input)
			}
			tool.Output = event.Output
			tool.Action = event.Action
			out[i].Parts[j].Tool = &tool
		}
	}
	return out
}

// Other adapters can retain only service-origin additions without changing their
// normal native history merge policy. User boundaries anchor repeated prompts.
func SurfaceRecords(messages []Message) []Message {
	var out []Message
	for _, message := range messages {
		if message.Role == "user" {
			out = append(out, message)
			continue
		}
		copy := message
		copy.Text = ""
		copy.Parts = nil
		for _, part := range message.Parts {
			if part.Interaction != nil && part.Interaction.Meta["origin"] == "surface" || part.Tool != nil && part.Tool.Action != nil && part.Tool.Action.Surface != nil {
				copy.Parts = append(copy.Parts, part)
			}
		}
		if len(copy.Parts) > 0 {
			out = append(out, copy)
		}
	}
	return out
}

// Text and clipboard bodies are delivered to the harness, but are not duplicated
// in the shared feed/receipt store. Keep request IDs and coordinates for matching.
func surfaceCardInput(input any) any {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	// Codex supplies the complete MCP item as Input, including its result on
	// completion. Keep only arguments; otherwise PNG/clipboard output is copied
	// back into the feed even after Output has been reduced to an artifact ID.
	if item, ok := value.(map[string]any); ok {
		if arguments, exists := item["arguments"]; exists {
			value = arguments
			if encoded, ok := value.(string); ok {
				var decoded any
				if json.Unmarshal([]byte(encoded), &decoded) == nil {
					value = decoded
				}
			}
		} else if kind, _ := item["type"].(string); kind == "mcpToolCall" || kind == "mcp_tool_call" {
			value = nil
		}
	}
	var redact func(any, int) any
	redact = func(v any, depth int) any {
		if depth > 8 {
			return nil
		}
		switch v := v.(type) {
		case map[string]any:
			if kind, _ := v["kind"].(string); kind == "text" || kind == "clipboard_write" {
				if _, exists := v["text"]; exists {
					v["text"] = "[text omitted]"
				}
			}
			for k, item := range v {
				v[k] = redact(item, depth+1)
			}
		case []any:
			for i, item := range v {
				v[i] = redact(item, depth+1)
			}
		}
		return v
	}
	return redact(value, 0)
}
