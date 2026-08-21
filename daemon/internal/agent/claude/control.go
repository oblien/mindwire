package claude

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The Claude stream-json control protocol (used only by the persistent transport, when a turn runs in
// a non-bypass permission mode). Two response mechanisms exist and the parser tags each interaction's
// Meta with which one applies, so encodeInbound can stay stateless:
//
//   - control  — a `can_use_tool` permission ask is answered with a `control_response` whose
//     response.request_id echoes the CLI's control_request id (the interaction id).
//   - tool_result — an AskUserQuestion / ExitPlanMode ask is answered by injecting a user message
//     carrying a tool_result block keyed by the tool_use id.
//
// Both markers are stashed in Interaction.Meta by parse.go and echoed back on Inbound.Meta by the
// supervisor, so the adapter reads them here without any server-side lookup.
const (
	respondViaControl    = "control"
	respondViaToolResult = "tool_result"

	// initializeRequestID is the client→CLI handshake id. Sending an `initialize` control_request at
	// session start arms bidirectional control routing (the SDK's hasBidirectionalNeeds gate), so the
	// CLI routes tool-permission asks back as `can_use_tool` control_requests instead of auto-running.
	// One handshake per process, so a fixed id is fine; the CLI's ack echoes it and the parser ignores it.
	initializeRequestID = "mw-initialize"
	interruptRequestID  = "mw-interrupt"
	// Runtime-control request ids. Fire-and-forget like interrupt: the CLI acks each with a
	// control_response the parser ignores, so a fixed id per subtype is fine.
	setModelRequestID          = "mw-set-model"
	setPermissionModeRequestID = "mw-set-permission-mode"
)

// persistentPreamble is the pair of NDJSON lines written to the CLI's stdin before the pump starts:
// the initialize handshake (arms can_use_tool routing) then the first user message. In persistent
// mode the message is delivered over stdin, NOT as a `claude -p <msg>` argument. When the turn carries
// image blocks they ride on that first message (true vision), exactly as the one-shot stream-json path.
func persistentPreamble(message string, imageBlocks []any) [][]byte {
	init, _ := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": initializeRequestID,
		"request":    map[string]any{"subtype": "initialize"},
	})
	msg, _ := json.Marshal(userMessageBlocks(message, imageBlocks))
	return [][]byte{init, msg}
}

// userMessage builds an SDKUserMessage envelope with plain-text content.
func userMessage(text string) map[string]any {
	return map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": text},
	}
}

// userMessageBlocks builds an SDKUserMessage whose content is a text block (when non-empty) followed by
// the given image content blocks. With no image blocks it degrades to the plain-text userMessage shape,
// so a single builder serves both the vision and text paths.
func userMessageBlocks(text string, imageBlocks []any) map[string]any {
	if len(imageBlocks) == 0 {
		return userMessage(text)
	}
	content := make([]any, 0, len(imageBlocks)+1)
	if strings.TrimSpace(text) != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	content = append(content, imageBlocks...)
	return map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": content},
	}
}

// imageMediaType returns an attachment's image/* media type — from its declared MIME, else inferred
// from the file extension — or "" when it isn't a supported image (so it stays a path reference).
func imageMediaType(at agent.Attachment) string {
	if mt := strings.TrimSpace(at.Mime); strings.HasPrefix(mt, "image/") {
		return mt
	}
	switch strings.ToLower(filepath.Ext(at.Path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

// imageBlock builds a Claude base64 image content block from an attachment, or ok=false when the
// attachment isn't a readable image (no image MIME/extension, or the bytes can't be obtained). Inline
// Data is preferred; otherwise the file at Path is read. Callers that get ok=false fall back to a
// path-reference text block so the model can still open the file with its Read tool.
func imageBlock(at agent.Attachment) (map[string]any, bool) {
	mt := imageMediaType(at)
	if mt == "" {
		return nil, false
	}
	raw := at.Data
	if len(raw) == 0 {
		if at.Path == "" {
			return nil, false
		}
		b, err := os.ReadFile(at.Path)
		if err != nil {
			return nil, false
		}
		raw = b
	}
	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": mt,
			"data":       base64.StdEncoding.EncodeToString(raw),
		},
	}, true
}

// encodeInbound maps one unified Inbound to a single NDJSON stdin line for the CLI. ok=false ⇒ nothing
// to send (skipped by the driver). All JSON is built with encoding/json — no string interpolation, so
// user text can never break the wire framing or inject a control message.
func encodeInbound(in agent.Inbound) ([]byte, bool) {
	switch in.Kind {
	case "response":
		if in.Meta["respondVia"] == respondViaControl {
			return encodePermission(in)
		}
		return encodeToolResult(in)
	case "input":
		if strings.TrimSpace(in.Text) == "" {
			return nil, false
		}
		b, err := json.Marshal(userMessage(in.Text))
		return b, err == nil
	case "interrupt":
		b, err := json.Marshal(map[string]any{
			"type":       "control_request",
			"request_id": interruptRequestID,
			"request":    map[string]any{"subtype": "interrupt"},
		})
		return b, err == nil
	case "set_model":
		// model is OPTIONAL on the wire: an empty Text omits it, which resets the turn to the
		// account/CLI default model (SDKControlSetModelRequest.model is `model?: string`).
		req := map[string]any{"subtype": "set_model"}
		if m := strings.TrimSpace(in.Text); m != "" {
			req["model"] = m
		}
		b, err := json.Marshal(map[string]any{
			"type":       "control_request",
			"request_id": setModelRequestID,
			"request":    req,
		})
		return b, err == nil
	case "set_permission_mode":
		// mode is REQUIRED (SDKControlSetPermissionModeRequest.mode: PermissionMode); an empty value
		// has nothing to send, so skip it.
		mode := strings.TrimSpace(in.Text)
		if mode == "" {
			return nil, false
		}
		b, err := json.Marshal(map[string]any{
			"type":       "control_request",
			"request_id": setPermissionModeRequestID,
			"request":    map[string]any{"subtype": "set_permission_mode", "mode": mode},
		})
		return b, err == nil
	}
	return nil, false
}

// encodePermission answers a `can_use_tool` ask over the control channel. The response.request_id
// echoes the control_request id (the interaction id); the inner response is the SDK PermissionResult
// (allow, or deny with a message).
func encodePermission(in agent.Inbound) ([]byte, bool) {
	var result map[string]any
	if agent.Denied(in.Decision) {
		msg := strings.TrimSpace(in.Text)
		if msg == "" {
			msg = "Denied by user"
		}
		result = map[string]any{"behavior": "deny", "message": msg}
	} else {
		result = map[string]any{"behavior": "allow"}
	}
	b, err := json.Marshal(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": in.InteractionID,
			"response":   result,
		},
	})
	return b, err == nil
}

// encodeToolResult answers an AskUserQuestion / ExitPlanMode ask by injecting a user message with a
// tool_result block keyed by the tool_use id. A rejected plan carries an error result so the agent
// knows the plan was declined.
func encodeToolResult(in agent.Inbound) ([]byte, bool) {
	toolUseID := in.Meta["toolUseId"]
	if toolUseID == "" {
		toolUseID = in.InteractionID
	}
	if toolUseID == "" {
		return nil, false
	}
	content := in.Text
	if content == "" && len(in.Options) > 0 {
		content = strings.Join(in.Options, ", ")
	}
	if content == "" {
		content = in.Decision
	}
	b, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"tool_use_id": toolUseID,
				"type":        "tool_result",
				"content":     content,
				"is_error":    agent.Denied(in.Decision),
			}},
		},
	})
	return b, err == nil
}
