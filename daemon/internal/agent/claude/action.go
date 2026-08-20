package claude

import (
	"encoding/json"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// action.go deep-normalizes Claude's tool calls into the unified agent.ToolAction (canonical kind +
// structured payload). name/input come from the tool_use block; output/isError arrive with the paired
// tool_result (empty at use time). The field names below are Claude Code's documented tool-input schema
// (Edit/Write/Bash/…), but every decoder degrades gracefully: a missing or renamed field yields a
// Title-only action of the right Kind, never a panic — the raw Input/Output always survive alongside.
func claudeToolAction(name string, input json.RawMessage, output string, isError bool) *agent.ToolAction {
	switch {
	case name == "Edit":
		return editAction(input)
	case name == "MultiEdit":
		return multiEditAction(input)
	case name == "Write":
		return writeAction(input)
	case name == "Read", name == "NotebookRead":
		return readAction(input)
	case name == "Bash", name == "BashOutput":
		return bashAction(input, output)
	case name == "Grep", name == "Glob":
		return searchAction(name, input)
	case name == "WebFetch":
		return webFetchAction(input)
	case name == "WebSearch":
		return webSearchAction(input)
	case strings.HasPrefix(name, "mcp__"):
		return mcpAction(name)
	default:
		return &agent.ToolAction{Kind: agent.KindOther, Title: name}
	}
}

// editAction: Edit {file_path, old_string, new_string}. The synthesized diff is of the replaced
// fragment (line numbers relative to the fragment, not the file) — best-effort but renderable.
func editAction(input json.RawMessage) *agent.ToolAction {
	var in struct {
		FilePath  string `json:"file_path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	_ = json.Unmarshal(input, &in)
	fc := agent.FileChange{Path: in.FilePath, Op: "edit", OldText: in.OldString, NewText: in.NewString}
	fc.Diff = agent.BuildUnifiedDiff(in.FilePath, in.OldString, in.NewString)
	return &agent.ToolAction{Kind: agent.KindFileEdit, Title: in.FilePath, Files: []agent.FileChange{fc}}
}

// multiEditAction: MultiEdit {file_path, edits:[{old_string,new_string}]} — one file, many hunks. We
// can't rebuild a whole-file diff without the file content, so concatenate per-edit fragment diffs
// under a single file header; OldText/NewText are left empty (ambiguous across edits).
func multiEditAction(input json.RawMessage) *agent.ToolAction {
	var in struct {
		FilePath string `json:"file_path"`
		Edits    []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	_ = json.Unmarshal(input, &in)
	var b strings.Builder
	if in.FilePath != "" {
		b.WriteString("--- a/" + in.FilePath + "\n+++ b/" + in.FilePath + "\n")
	}
	for _, e := range in.Edits {
		if d := agent.BuildUnifiedDiff("", e.OldString, e.NewString); d != "" {
			b.WriteString(d)
		}
	}
	fc := agent.FileChange{Path: in.FilePath, Op: "edit", Diff: b.String()}
	return &agent.ToolAction{Kind: agent.KindFileEdit, Title: in.FilePath, Files: []agent.FileChange{fc}}
}

// writeAction: Write {file_path, content}. Claude doesn't say whether the file pre-existed, so op:create
// is the best-effort label; the diff is all-additions.
func writeAction(input json.RawMessage) *agent.ToolAction {
	var in struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	_ = json.Unmarshal(input, &in)
	fc := agent.FileChange{
		Path: in.FilePath, Op: "create", NewText: in.Content,
		Diff: agent.BuildUnifiedDiff(in.FilePath, "", in.Content),
	}
	return &agent.ToolAction{Kind: agent.KindFileEdit, Title: in.FilePath, Files: []agent.FileChange{fc}}
}

// readAction: Read {file_path}. file_read carries only a Title (the path); no file body is normalized.
func readAction(input json.RawMessage) *agent.ToolAction {
	var in struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	_ = json.Unmarshal(input, &in)
	path := in.FilePath
	if path == "" {
		path = in.NotebookPath
	}
	return &agent.ToolAction{Kind: agent.KindFileRead, Title: path}
}

// bashAction: Bash {command}. Claude's Bash merges stderr into stdout and reports no exit code, so
// Stderr/ExitCode stay nil (distinct from "" / 0) — the whole point of the pointer fields.
func bashAction(input json.RawMessage, output string) *agent.ToolAction {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &in)
	return &agent.ToolAction{
		Kind: agent.KindShell, Title: in.Command,
		Shell: &agent.ShellCommand{Command: in.Command, Stdout: output},
	}
}

// searchAction: Grep {pattern, path?, glob?} / Glob {pattern, path?}. For Glob the `pattern` IS the
// glob, so it moves to Glob for a truer shape.
func searchAction(name string, input json.RawMessage) *agent.ToolAction {
	var in struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	_ = json.Unmarshal(input, &in)
	sq := &agent.SearchQuery{Query: in.Pattern, Path: in.Path, Glob: in.Glob}
	if name == "Glob" {
		sq = &agent.SearchQuery{Glob: in.Pattern, Path: in.Path}
	}
	return &agent.ToolAction{Kind: agent.KindSearch, Title: in.Pattern, Search: sq}
}

// webFetchAction: WebFetch {url}.
func webFetchAction(input json.RawMessage) *agent.ToolAction {
	var in struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(input, &in)
	return &agent.ToolAction{Kind: agent.KindWebFetch, Title: in.URL, Web: &agent.WebSearch{URL: in.URL}}
}

// webSearchAction: WebSearch {query}.
func webSearchAction(input json.RawMessage) *agent.ToolAction {
	var in struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(input, &in)
	return &agent.ToolAction{Kind: agent.KindWebSearch, Title: in.Query, Web: &agent.WebSearch{Query: in.Query}}
}

// mcpAction classifies an `mcp__<server>__<tool>` call. The server/tool are recovered from the name.
func mcpAction(name string) *agent.ToolAction {
	server, tool := parseMCPName(name)
	return &agent.ToolAction{Kind: agent.KindMCP, Title: name, MCP: &agent.MCPCall{Server: server, Tool: tool}}
}

// parseMCPName splits Claude's `mcp__server__tool` naming. A name with no tool segment yields the
// server only.
func parseMCPName(name string) (server, tool string) {
	rest := strings.TrimPrefix(name, "mcp__")
	if i := strings.Index(rest, "__"); i >= 0 {
		return rest[:i], rest[i+2:]
	}
	return rest, ""
}
