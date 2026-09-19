package agent

import "strings"

// LegacyAsyncFormID recognizes the informational question rows emitted by older
// daemons. It uses their explicit native item IDs, never guesses from chat prose.
func LegacyAsyncFormID(it *Interaction) string {
	if it == nil || it.Kind != "info" || it.Meta["asyncQuestion"] != true {
		return ""
	}
	base, _, ok := strings.Cut(it.ID, ":question:")
	if !ok || base == "" {
		return ""
	}
	return base + ":questions"
}

// UpgradeAsyncQuestionParts replaces legacy notices only when the actual native
// structured form is available. One form replaces all of its old question rows
// and their duplicated native text. Used by both native history and state migration.
func UpgradeAsyncQuestionParts(parts []Part, forms map[string]Interaction) []Part {
	legacy := map[string]bool{}
	for _, part := range parts {
		if id := LegacyAsyncFormID(part.Interaction); id != "" {
			if _, ok := forms[id]; ok {
				legacy[id] = true
			}
		}
	}
	if len(legacy) == 0 {
		return parts
	}
	var out []Part
	seen := map[string]bool{}
	for _, part := range parts {
		if part.Type == "text" && part.ID != "" && legacy[part.ID+":questions"] {
			continue
		}
		if id := LegacyAsyncFormID(part.Interaction); legacy[id] {
			if seen[id] {
				continue
			}
			seen[id] = true
			form := forms[id]
			if part.Interaction.RunID != "" {
				form.RunID = part.Interaction.RunID
			}
			part.Interaction = &form
		}
		out = append(out, part)
	}
	return out
}
