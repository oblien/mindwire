package codex

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Codex exposes the same failure through exec, notifications and RPC responses. Keep the
// useful message and upstream details together, including when an error is nested or a string.
func errorText(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	var message string
	if json.Unmarshal(raw, &message) == nil {
		if message = strings.TrimSpace(message); message != "" {
			return message
		}
		return fallback
	}
	var e struct {
		Message           string          `json:"message"`
		Error             json.RawMessage `json:"error"`
		AdditionalDetails string          `json:"additionalDetails"`
		Details           string          `json:"additional_details"`
		Info              json.RawMessage `json:"codexErrorInfo"`
		NativeInfo        json.RawMessage `json:"codex_error_info"`
		Misalignment      struct {
			Explanation       string `json:"detailedExplanation"`
			NativeExplanation string `json:"detailed_explanation"`
		} `json:"misalignment"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return fallback
	}
	message = strings.TrimSpace(e.Message)
	for _, detail := range []string{errorText(e.Error, ""), e.AdditionalDetails, e.Details, e.Misalignment.Explanation, e.Misalignment.NativeExplanation} {
		detail = strings.TrimSpace(detail)
		if detail != "" && !strings.Contains(message, detail) {
			if message != "" {
				message += "\n"
			}
			message += detail
		}
	}
	info := e.Info
	if len(info) == 0 || string(info) == "null" {
		info = e.NativeInfo
	}
	label, status := errorInfo(info)
	if message == "" {
		message = label
	}
	if status != 0 && !strings.Contains(message, fmt.Sprintf("HTTP %d", status)) {
		message = strings.TrimSpace(message + fmt.Sprintf(" (HTTP %d)", status))
	}
	if message == "" {
		return fallback
	}
	return message
}

// The live schema uses camelCase; native rollouts use snake_case or PascalCase.
// Unknown error codes keep the server's message instead of causing a decode failure.
func errorInfo(raw json.RawMessage) (string, int) {
	var code string
	var status int
	if json.Unmarshal(raw, &code) != nil {
		var variants map[string]json.RawMessage
		if json.Unmarshal(raw, &variants) == nil {
			for name, detail := range variants {
				if name == "httpStatusCode" || name == "http_status_code" {
					_ = json.Unmarshal(detail, &status)
					continue
				}
				code = name
				var info struct {
					Status       int `json:"httpStatusCode"`
					NativeStatus int `json:"http_status_code"`
				}
				if json.Unmarshal(detail, &info) == nil {
					if info.Status != 0 {
						status = info.Status
					} else if info.NativeStatus != 0 {
						status = info.NativeStatus
					}
				}
			}
		}
	}
	label := map[string]string{
		"contextwindowexceeded":          "The conversation exceeds the model's context window.",
		"sessionbudgetexceeded":          "The session budget was exceeded.",
		"usagelimitexceeded":             "The usage limit was exceeded.",
		"ratelimitexceeded":              "The request rate limit was exceeded.",
		"serveroverloaded":               "The model service is overloaded.",
		"cyberpolicy":                    "The request was blocked by the model's cyber policy.",
		"misalignmentpolicyviolation":    "The request was blocked by the model's policy.",
		"internalservererror":            "The model service encountered an internal error.",
		"unauthorized":                   "Authentication failed.",
		"badrequest":                     "The model service rejected the request.",
		"threadrollbackfailed":           "The conversation could not be rolled back.",
		"sandboxerror":                   "The sandbox encountered an error.",
		"httpconnectionfailed":           "Could not connect to the model service.",
		"responsestreamconnectionfailed": "Could not connect to the response stream.",
		"responsestreamdisconnected":     "The response stream disconnected before completion.",
		"responsetoomanyfailedattempts":  "The response failed after retrying.",
		"activeturnnotsteerable":         "This turn cannot accept another message while it is running.",
	}[strings.ToLower(strings.ReplaceAll(code, "_", ""))]
	return label, status
}

func imageFailure(failure, fallback json.RawMessage) json.RawMessage {
	if len(failure) == 0 || string(failure) == "null" {
		return fallback
	}
	var detail struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(failure, &detail)
	message := "Image generation failed."
	if detail.Type == "usageLimitExceeded" || detail.Type == "usage_limit_exceeded" {
		message = "Image generation usage limit exceeded."
	}
	encoded, _ := json.Marshal(errorText(failure, message))
	return encoded
}

// textBlocks accepts the human-readable string and content-block shapes used in the two
// transports. Opaque/encrypted reasoning is never treated as display text.
func textBlocks(raw json.RawMessage) []string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if json.Unmarshal(entry, &s) == nil {
			out = append(out, s)
			continue
		}
		var block struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(entry, &block) == nil && block.Text != "" {
			out = append(out, block.Text)
		}
	}
	return out
}

// renderOutput handles MCP content, dynamic tool content and rollout tool outputs without
// losing structured errors. Unknown output shapes survive as JSON for the generic tool view.
func renderOutput(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, false
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) == nil {
		var lines []string
		failed := false
		for _, entry := range entries {
			text, isErr := renderOutput(entry)
			if text != "" {
				lines = append(lines, text)
			}
			failed = failed || isErr
		}
		return strings.Join(lines, "\n"), failed
	}
	var obj struct {
		Type             string          `json:"type"`
		Text             string          `json:"text"`
		Content          json.RawMessage `json:"content"`
		Output           json.RawMessage `json:"output"`
		Structured       json.RawMessage `json:"structuredContent"`
		Error            json.RawMessage `json:"error"`
		Success          *bool           `json:"success"`
		IsError          bool            `json:"isError"`
		NativeIsError    bool            `json:"is_error"`
		NativeStructured json.RawMessage `json:"structured_content"`
		Resource         json.RawMessage `json:"resource"`
		Blob             json.RawMessage `json:"blob"`
		URI              string          `json:"uri"`
		Name             string          `json:"name"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return string(raw), false
	}
	failed := obj.IsError || obj.NativeIsError || (obj.Success != nil && !*obj.Success)
	if err := errorText(obj.Error, ""); err != "" {
		return err, true
	}
	if obj.Text != "" {
		return obj.Text, failed
	}
	var lines []string
	for _, body := range []json.RawMessage{obj.Content, obj.Output, obj.Structured, obj.NativeStructured, obj.Resource} {
		if text, isErr := renderOutput(body); text != "" {
			failed = failed || isErr
			duplicate := false
			for _, line := range lines {
				duplicate = duplicate || line == text
			}
			if !duplicate {
				lines = append(lines, text)
			}
		}
	}
	if len(lines) > 0 {
		return strings.Join(lines, "\n"), failed
	}
	// Binary content remains in the raw input. Don't flood the tool's text pane with base64.
	switch obj.Type {
	case "image", "inputImage", "input_image":
		return "Image", failed
	case "audio", "inputAudio", "input_audio":
		return "Audio", failed
	case "encrypted_content":
		return "Encrypted tool content", failed
	case "resource_link":
		return strings.TrimSpace(obj.Name + "\n" + obj.URI), failed
	}
	if len(obj.Blob) > 0 {
		return strings.TrimSpace("Resource\n" + obj.URI), failed
	}
	return strings.TrimSpace(string(raw)), failed
}
