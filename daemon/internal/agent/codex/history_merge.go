package codex

import (
	"fmt"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Codex does not persist every app-server notification. Keep its authoritative item
// snapshots and imported turns, adding recorded warnings, failures and partial output
// within the matching user turn. Matching is chronological so repeated prompts cannot
// consume an earlier reply; compaction boundaries stay separate messages.
func mergeRecordedHistory(native, recorded []agent.Message) []agent.Message {
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
				// The stored user timestamp has second precision; the native echo has milliseconds.
				if stamp.Before(start.Add(-time.Second)) || end.After(start) && !stamp.Before(end) {
					continue
				}
			}
			match = i
			break
		}
		if match >= 0 {
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
	var result []agent.Message
	for _, group := range groups {
		result = append(result, group...)
	}
	return result
}

func historyTurns(messages []agent.Message) [][]agent.Message {
	var groups [][]agent.Message
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

func historyParts(message agent.Message) []agent.Part {
	if len(message.Parts) > 0 {
		return message.Parts
	}
	if message.Text != "" {
		return []agent.Part{{Type: "text", Text: message.Text}}
	}
	return nil
}

func mergeTurnParts(native, recorded []agent.Message) []agent.Message {
	type fragment struct {
		part  agent.Part
		owner int
	}
	owners := append([]agent.Message(nil), native...)
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
				if sameHistoryPart(parts[i].part, part) {
					match = i
					break
				}
			}
			if match >= 0 {
				final := &parts[match].part
				if final.DurationMs == 0 {
					final.DurationMs = part.DurationMs
				}
				if final.Tokens == 0 {
					final.Tokens = part.Tokens
				}
				if final.ID == "" {
					final.ID = part.ID
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
	var result []agent.Message
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

func sameHistoryPart(final, recorded agent.Part) bool {
	if final.Type != recorded.Type {
		return false
	}
	switch final.Type {
	case "text", "thinking":
		if final.ID != "" && recorded.ID != "" {
			return final.ID == recorded.ID
		}
		return final.Text != "" && recorded.Text != "" &&
			(strings.HasPrefix(final.Text, recorded.Text) || strings.HasPrefix(recorded.Text, final.Text))
	case "tool":
		return final.Tool != nil && recorded.Tool != nil && final.Tool.ID != "" && final.Tool.ID == recorded.Tool.ID
	case "interaction":
		a, b := final.Interaction, recorded.Interaction
		return a != nil && b != nil && (a.ID != "" && a.ID == b.ID || a.Kind == b.Kind && a.Detail == b.Detail && (a.Kind == "error" || a.Title == b.Title))
	case "compaction":
		return true // chronological boundary, never copied into an earlier user turn
	}
	return false
}
