package agent

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Codex does not persist every app-server notification. Keep its authoritative item
// snapshots and imported turns, adding recorded warnings, failures and partial output
// within the matching user turn. Matching is chronological so repeated prompts cannot
// consume an earlier reply; compaction boundaries stay separate messages.
func MergeRecordedHistory(native, recorded []Message) []Message {
	if len(native) == 0 {
		return recorded
	}
	if len(recorded) == 0 {
		return native
	}
	groups, records := historyTurns(native), historyTurns(recorded)
	cursor := 0
	for r, record := range records {
		if len(record) == 0 {
			continue
		}
		start, hasStart := historyTime(record[0].CreatedAt)
		var end time.Time
		if r+1 < len(records) {
			end, _ = historyTime(records[r+1][0].CreatedAt)
		}
		match := -1
		for i := cursor; i < len(groups); i++ {
			candidate := groups[i][0]
			if candidate.Role != record[0].Role || candidate.Text != record[0].Text {
				continue
			}
			if stamp, ok := historyTime(candidate.CreatedAt); ok && hasStart {
				// Legacy stored user timestamps have second precision; native echoes use milliseconds.
				if stamp.Before(start.Add(-time.Second)) || end.After(start) && !stamp.Before(end) {
					continue
				}
			}
			match = i
			break
		}
		if match >= 0 {
			record = withoutSteeredPrefix(groups, match, record)
			groups[match] = mergeTurnParts(groups[match], record)
			cursor = match + 1
			continue
		}
		// A failed start may have no native user echo or item at all. Retain the whole
		// recorded turn at its chronological position instead of dropping its error.
		insert := len(groups)
		if hasStart {
			for i := cursor; i < len(groups); i++ {
				if stamp, ok := historyTime(groups[i][0].CreatedAt); ok && stamp.After(start) {
					insert = i
					break
				}
			}
		}
		groups = append(groups, nil)
		copy(groups[insert+1:], groups[insert:])
		groups[insert] = record
		cursor = insert + 1
	}
	var result []Message
	for _, group := range groups {
		result = append(result, group...)
	}
	return result
}

// Native steering adds a user-message boundary inside one daemon run. Its final
// recorded snapshot still contains the output from before that boundary. Locate
// that prefix by a native async question's stable ID, then omit components already
// present there. Scope the match to those native turns, so repeated text or local
// item IDs from an unrelated earlier run cannot consume the current response.
func withoutSteeredPrefix(groups [][]Message, current int, recorded []Message) []Message {
	questions := map[string]bool{}
	for _, message := range recorded {
		for _, part := range historyParts(message) {
			if part.Interaction != nil && part.Interaction.IsMessageQuestion() {
				questions[part.Interaction.ID] = true
			}
		}
	}
	if len(questions) == 0 {
		return recorded
	}
	start := current
	for i := 0; i < current; i++ {
		for _, message := range groups[i] {
			for _, part := range historyParts(message) {
				if part.Interaction != nil && questions[part.Interaction.ID] {
					start = min(start, i)
				}
			}
		}
	}
	if start == current {
		return recorded
	}
	var prefix []Part
	for _, group := range groups[start:current] {
		for _, message := range group {
			if message.Role != "user" {
				prefix = append(prefix, historyParts(message)...)
			}
		}
	}
	cursor := 0
	var result []Message
	for _, message := range recorded {
		if message.Role == "user" {
			result = append(result, message)
			continue
		}
		var remaining []Part
		var text []string
		for _, part := range historyParts(message) {
			match := -1
			for i := cursor; i < len(prefix); i++ {
				if SameHistoryPart(prefix[i], part) {
					match = i
					break
				}
			}
			if match >= 0 {
				cursor = match + 1
				continue
			}
			remaining = append(remaining, part)
			if part.Type == "text" {
				text = append(text, part.Text)
			}
		}
		if len(remaining) > 0 {
			message.Parts, message.Text = remaining, strings.Join(text, "\n\n")
			result = append(result, message)
		}
	}
	return result
}

func historyTurns(messages []Message) [][]Message {
	var groups [][]Message
	for _, message := range messages {
		if len(groups) == 0 || message.Role == "user" {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], message)
	}
	return groups
}

func historyTime(value string) (time.Time, bool) {
	stamp, err := time.Parse(time.RFC3339Nano, value)
	return stamp, err == nil
}

func historyParts(message Message) []Part {
	if len(message.Parts) > 0 {
		return message.Parts
	}
	if message.Text != "" {
		return []Part{{Type: "text", Text: message.Text}}
	}
	return nil
}

func mergeTurnParts(native, recorded []Message) []Message {
	type fragment struct {
		part  Part
		owner int
	}
	owners := append([]Message(nil), native...)
	var parts []fragment
	for i, message := range native {
		if message.Role == "user" {
			continue
		}
		for _, part := range historyParts(message) {
			parts = append(parts, fragment{part, i})
		}
	}
	cursor := 0
	for _, message := range recorded {
		if message.Role == "user" {
			continue
		}
		for _, part := range historyParts(message) {
			match := -1
			for i := cursor; i < len(parts); i++ {
				if SameHistoryPart(parts[i].part, part) {
					match = i
					break
				}
			}
			if match >= 0 {
				final := &parts[match].part
				if final.Text == "" && (part.Type == "text" || part.Type == "thinking") {
					// Interrupted items can be persisted without their streamed body.
					final.Text = part.Text
				}
				if final.DurationMs == 0 {
					final.DurationMs = part.DurationMs
				}
				if final.Tokens == 0 {
					final.Tokens = part.Tokens
				}
				// Return the same identity the client saw live. Exec's item_N identifiers
				// are unrelated to the native msg_/exec- identifiers for the same item.
				if part.ID != "" {
					final.ID = part.ID
				}
				if final.Tool != nil && part.Tool != nil && part.Tool.ID != "" {
					tool := *final.Tool
					tool.ID = part.Tool.ID
					if part.Tool.Action != nil && part.Tool.Action.Surface != nil {
						if tool.Action == nil {
							tool.Action = part.Tool.Action
						} else {
							a := *tool.Action
							a.Surface = part.Tool.Action.Surface
							tool.Action = &a
						}
					}
					final.Tool = &tool
				}
				if final.Interaction != nil && part.Interaction != nil && part.Interaction.ID != "" {
					interaction := *final.Interaction
					interaction.ID = part.Interaction.ID
					if part.Interaction.RunID != "" {
						interaction.RunID = part.Interaction.RunID
					}
					final.Interaction = &interaction
				}
				cursor = match + 1
				continue
			}
			owner := -1
			if cursor > 0 && owners[parts[cursor-1].owner].Role == message.Role {
				owner = parts[cursor-1].owner
			}
			if owner < 0 && cursor < len(parts) && owners[parts[cursor].owner].Role == message.Role {
				owner = parts[cursor].owner
			}
			if owner < 0 || part.Type == "compaction" {
				owner = len(owners)
				copy := message
				if part.Type == "compaction" {
					copy.Role = "system"
				}
				owners = append(owners, copy)
			}
			parts = append(parts, fragment{})
			copy(parts[cursor+1:], parts[cursor:])
			parts[cursor] = fragment{part, owner}
			cursor++
		}
	}
	var result []Message
	if native[0].Role == "user" {
		result = append(result, native[0])
	}
	lastOwner := -1
	usedIDs := map[string]int{}
	for _, fragment := range parts {
		if fragment.owner != lastOwner {
			message := owners[fragment.owner]
			message.Text, message.Parts = "", nil
			usedIDs[message.ID]++
			if usedIDs[message.ID] > 1 {
				message.ID += fmt.Sprintf(":%d", usedIDs[message.ID])
			}
			result = append(result, message)
			lastOwner = fragment.owner
		}
		message := &result[len(result)-1]
		message.Parts = append(message.Parts, fragment.part)
		if fragment.part.Type == "text" {
			if message.Text != "" {
				message.Text += "\n\n"
			}
			message.Text += fragment.part.Text
		}
	}
	return result
}

func SameHistoryPart(final, recorded Part) bool {
	if final.Type != recorded.Type {
		return false
	}
	switch final.Type {
	case "text", "thinking":
		if final.ID != "" && final.ID == recorded.ID {
			return true
		}
		a, b := strings.TrimSpace(final.Text), strings.TrimSpace(recorded.Text)
		if a == "" || b == "" {
			return false
		}
		return a == b || (final.ID == "" || recorded.ID == "") && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
	case "tool":
		a, b := final.Tool, recorded.Tool
		if a == nil || b == nil {
			return false
		}
		if a.ID != "" && a.ID == b.ID {
			return true
		}
		if !a.Action.SameInvocation(b.Action) {
			return false
		}
		// MCP action metadata identifies a tool, not its arguments. Distinct calls
		// to the same server/tool must not consume each other's native results.
		return a.Action.Kind != KindMCP || reflect.DeepEqual(historyToolArguments(a.Input), historyToolArguments(b.Input))
	case "interaction":
		a, b := final.Interaction, recorded.Interaction
		return a != nil && b != nil && (a.ID != "" && a.ID == b.ID || a.Kind == b.Kind && a.Detail == b.Detail && (a.Kind == "error" || a.Title == b.Title))
	case "compaction":
		return true // chronological boundary, never copied into an earlier user turn
	}
	return false
}

func historyToolArguments(raw json.RawMessage) any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	if fields, ok := value.(map[string]any); ok {
		if arguments, found := fields["arguments"]; found {
			return arguments
		}
	}
	return value
}
