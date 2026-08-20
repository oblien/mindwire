package agent

import (
	"strings"
	"unicode"
)

// SubagentMeta is the best-effort parsed view of a subagent definition's YAML-ish frontmatter. It is a
// CONVENIENCE only — the raw Content of a Subagent is the canonical, lossless unit. Every field is
// optional (omitempty); a definition with no parseable frontmatter yields a nil *SubagentMeta.
type SubagentMeta struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Model       string   `json:"model,omitempty"`
}

// Subagent is one persistent subagent definition file (Claude .claude/agents/<name>.md). Content is the
// canonical raw file body — omitted from LIST results and populated only on a single READ/WRITE, so
// listing stays cheap. Meta is the parsed convenience view (nil when the file has no usable frontmatter).
// Name is the definition's identity: its frontmatter `name` when present, else the filename stem.
type Subagent struct {
	Name    string        `json:"name"`
	Scope   MemoryScope   `json:"scope"`
	Path    string        `json:"path"`
	Content string        `json:"content,omitempty"`
	Meta    *SubagentMeta `json:"meta,omitempty"`
}

// SubagentsModule is an OPTIONAL adapter capability (type-asserted like PromptsModule): list, read, and
// write the agent's persistent subagent definition files. Distinct from the per-turn Subagents
// passthrough (Capabilities.Subagents / TurnOptions.Subagents) — this is the on-disk definition store.
// An implementer sets Capabilities.SubagentDefs=true; the API type-asserts this interface as the
// authoritative gate before serving /subagents. Claude-only today.
type SubagentsModule interface {
	// SubagentScopes lists the scopes this agent's subagent definitions support (Claude: project + user).
	SubagentScopes() []MemoryScope
	// ListSubagents returns every definition across the supported scopes with Content omitted (Meta
	// populated). dir is the resolved project directory. A missing scope directory yields NO entries for
	// that scope (reads are forgiving; only writes fail on a missing project directory).
	ListSubagents(dir string) ([]Subagent, error)
	// ReadSubagent returns one definition's full raw Content plus parsed Meta. A missing definition
	// returns an error satisfying errors.Is(err, fs.ErrNotExist), which the API maps to 404.
	ReadSubagent(scope MemoryScope, dir, name string) (Subagent, error)
	// WriteSubagent writes a definition verbatim (creating the scope directory as needed) and returns it.
	WriteSubagent(scope MemoryScope, dir, name, content string) (Subagent, error)
	// DeleteSubagent removes the definition in the scope whose identity matches name (the same match
	// ReadSubagent uses, so a nested file is found). Removing an absent definition is not an error
	// (idempotent).
	DeleteSubagent(scope MemoryScope, dir, name string) error
}

// parseFrontmatter extracts a subagent definition's leading `---\n … \n---` YAML-ish block into a
// SubagentMeta. It is a deliberately minimal, zero-dependency reader: it understands flat `key: value`
// lines (name/description/model as scalars, tools as a comma/space list) and ignores everything else.
// Returns nil when there is no fenced frontmatter block or it carries none of the recognized keys — the
// raw Content remains the source of truth in every case.
func parseFrontmatter(content string) *SubagentMeta {
	if !strings.HasPrefix(content, "---") {
		return nil
	}
	nl := strings.IndexByte(content, '\n')
	if nl < 0 || strings.TrimRight(content[:nl], "\r ") != "---" {
		return nil // the opening line must be exactly "---"
	}
	body := content[nl+1:]

	var block []string
	closed := false
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimRight(line, "\r ") == "---" {
			closed = true
			break
		}
		block = append(block, line)
	}
	if !closed {
		return nil // no closing fence → not real frontmatter
	}

	meta := &SubagentMeta{}
	any := false
	for _, line := range block {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		val := strings.Trim(strings.TrimSpace(line[colon+1:]), `"'`)
		switch key {
		case "name":
			meta.Name, any = val, true
		case "description":
			meta.Description, any = val, true
		case "model":
			meta.Model, any = val, true
		case "tools":
			if tools := splitTools(val); len(tools) > 0 {
				meta.Tools, any = tools, true
			}
		}
	}
	if !any {
		return nil
	}
	return meta
}

// splitTools parses a frontmatter `tools:` value — a list separated by commas and/or whitespace — into
// a clean slice (surrounding quotes stripped, empties dropped). Returns nil for an empty value.
func splitTools(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.Trim(f, `"'`); f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// subagentName resolves a definition's identity: its frontmatter `name` when present, else the filename
// stem (the basename with the ".md" suffix removed).
func subagentName(meta *SubagentMeta, filename string) string {
	if meta != nil && strings.TrimSpace(meta.Name) != "" {
		return meta.Name
	}
	return strings.TrimSuffix(filename, ".md")
}
