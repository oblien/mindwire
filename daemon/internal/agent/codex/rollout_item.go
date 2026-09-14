package codex

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Current rollouts persist event_msg/item_completed with PascalCase item types and
// snake_case fields. They are a third wire representation, not app-server JSON. Decode
// their differences here, then reuse the live item normalizer for all display components.
func rolloutItem(raw json.RawMessage) (normItem, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return normItem{}, err
	}
	var itemType string
	if err := json.Unmarshal(fields["type"], &itemType); err != nil {
		return normItem{}, err
	}
	converted := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		words := strings.Split(key, "_")
		for i := 1; i < len(words); i++ {
			if words[i] != "" {
				words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
			}
		}
		converted[strings.Join(words, "")] = value
	}
	if itemType != "" {
		itemType = strings.ToLower(itemType[:1]) + itemType[1:]
	}
	converted["type"], _ = json.Marshal(itemType)
	switch itemType {
	case "agentMessage", "userMessage":
		converted["text"], _ = json.Marshal(strings.Join(textBlocks(fields["content"]), "\n\n"))
	case "reasoning":
		converted["summary"] = fields["summary_text"]
		converted["content"] = fields["raw_content"]
	case "commandExecution":
		var argv []string
		if json.Unmarshal(fields["command"], &argv) == nil {
			converted["command"], _ = json.Marshal(agent.JoinShellWords(argv))
		}
	case "fileChange":
		var changes map[string]struct {
			Type     string `json:"type"`
			Content  string `json:"content"`
			Diff     string `json:"unified_diff"`
			MovePath string `json:"move_path"`
		}
		if err := json.Unmarshal(fields["changes"], &changes); err != nil {
			return normItem{}, err
		}
		paths := make([]string, 0, len(changes))
		for path := range changes {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		files := make([]normChange, 0, len(paths))
		for _, path := range paths {
			change := changes[path]
			file := normChange{Path: path, Kind: change.Type, Diff: change.Diff, MovePath: change.MovePath}
			file.setContent(change.Content)
			files = append(files, file)
		}
		// The common decoder also accepts normalized string kinds and movePath.
		converted["changes"], _ = json.Marshal(files)
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		return normItem{}, err
	}
	var it asItem
	if err := json.Unmarshal(encoded, &it); err != nil {
		return normItem{}, err
	}
	n := fromAsItem(it)
	if cwd, err := url.Parse(n.Cwd); err == nil && cwd.Scheme == "file" && (cwd.Host == "" || cwd.Host == "localhost") {
		n.Cwd = cwd.Path
	}
	if itemType == "userMessage" {
		n.Text = it.Text
	}
	if n.Kind == kindCommand && n.Output == "" {
		var stdout, stderr string
		_ = json.Unmarshal(fields["stdout"], &stdout)
		_ = json.Unmarshal(fields["stderr"], &stderr)
		n.Output = stdout + stderr
	}
	return n, nil
}
