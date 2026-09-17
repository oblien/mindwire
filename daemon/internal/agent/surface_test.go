package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSurfaceToolsKeepArtifactReferencesAndOmitPayloads(t *testing.T) {
	payload := `{"mindwireSurface":{"operation":"capture","capture":{"id":"frame","artifactId":"artifact","geometry":{"width":640,"height":480}}}}`
	for _, name := range []string{"mcp__mindwire_desktop__surface_capture", "mindwire_desktop.surface_capture"} {
		output, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": payload}, map[string]any{"type": "image", "data": "IMAGE_BODY"}}})
		for _, rendered := range []string{string(output), payload + "\nImage\n" + payload, "Wall time: 0.1 seconds\nOutput:\n" + payload} {
			event := ToolEvent{ID: "tool", Name: name, Output: rendered}
			NormalizeSurfaceTool(&event)
			if event.Action.Surface.Capture == nil || event.Action.Surface.Capture.ArtifactID != "artifact" || event.Action.Surface.Capture.Width != 640 {
				t.Fatalf("image reference missing: %#v", event.Action)
			}
			if strings.Contains(event.Output, "IMAGE_BODY") {
				t.Fatal("image body leaked into feed")
			}
			NormalizeSurfaceTool(&event)
			if event.Action.Surface.Capture.ArtifactID != "artifact" {
				t.Fatal("normalizing twice discarded the artifact")
			}
		}
	}
	input := map[string]any{"action": map[string]any{"kind": "clipboard_write", "text": "PRIVATE_TEXT"}, "requestId": "input-1"}
	event := ToolEvent{Name: "mcp__mindwire_desktop__surface_action", Input: input, Output: `{"structuredContent":{"mindwireSurface":{"operation":"action","receipt":{"id":"input-1","kind":"clipboard_read","status":"dispatched","text":"PRIVATE_CLIPBOARD"}}}}`}
	NormalizeSurfaceTool(&event)
	encoded, _ := json.Marshal(event)
	if strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatal("private input/output retained in feed")
	}
	if input["action"].(map[string]any)["text"] != "PRIVATE_TEXT" {
		t.Fatal("normalization changed the harness's original input")
	}
	if event.Action.Surface.ReceiptID != "input-1" || event.Action.Surface.Operation != "clipboard_read" {
		t.Fatalf("receipt metadata missing: %s", encoded)
	}
	wrapped := map[string]any{"type": "mcpToolCall", "arguments": input, "result": map[string]any{"content": []any{map[string]any{"type": "image", "data": "PRIVATE_IMAGE_BODY"}}}}
	event = ToolEvent{Name: "mindwire_desktop.surface_action", Input: wrapped}
	NormalizeSurfaceTool(&event)
	encoded, _ = json.Marshal(event)
	if strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatal("native MCP item copied private result bytes into Input")
	}
	if wrapped["result"] == nil {
		t.Fatal("normalizer mutated the native MCP item")
	}
}

func TestSurfaceHistoryRetainsOneToolAndResolvedApproval(t *testing.T) {
	user := Message{ID: "user", Role: "user", Text: "check the desktop"}
	nativeTool := &ToolPart{ID: "tool-1", Name: "mcp__mindwire_desktop__surface_capture", Output: `{"mindwireSurface":{"operation":"capture","capture":{"id":"frame","artifactId":"artifact","geometry":{"width":10,"height":20}}}}`}
	native := []Message{user, {ID: "native", Role: "assistant", Parts: []Part{{Type: "tool", Tool: nativeTool}, {Type: "text", Text: "Done"}}}}
	recorded := NormalizeSurfaceHistory([]Message{user, {ID: "live", Role: "assistant", Parts: []Part{
		{Type: "interaction", Interaction: &Interaction{ID: "approval", Kind: "approval", NeedsResponse: false, Meta: map[string]any{"origin": "surface", "decision": "allow"}}},
		{Type: "tool", Tool: nativeTool},
	}}})
	result := MergeRecordedHistory(NormalizeSurfaceHistory(native), SurfaceRecords(recorded))
	tools, approvals := 0, 0
	for _, message := range result {
		for _, part := range message.Parts {
			if part.Tool != nil {
				tools++
				if part.Tool.Action.Surface.Capture.ArtifactID != "artifact" {
					t.Fatal("history lost screenshot")
				}
			}
			if part.Interaction != nil {
				approvals++
				if part.Interaction.NeedsResponse {
					t.Fatal("resolved permission reopened")
				}
			}
		}
	}
	if tools != 1 || approvals != 1 {
		t.Fatalf("history duplicated or lost components: tools=%d approvals=%d", tools, approvals)
	}
	if nativeTool.Action != nil {
		t.Fatal("normalizer mutated native transcript")
	}
}
