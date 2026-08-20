package agent

import (
	"strconv"
	"strings"
)

// toolaction.go is the deep-normalization layer for tool activity. The unified stream already agrees on
// the tool *envelope* ({id, name, output, isError}); this adds a structured, cross-agent view of what a
// tool actually DID — a canonical Kind plus the payload for that kind (file edits with a diff, a shell
// command with stdout/exit-code, a search/web/mcp call). It rides ALONGSIDE the untouched raw
// Input/Output, so a client can render "the diff / the command / the changes" without knowing any one
// agent's private tool vocabulary (Claude's Edit/Write/Bash vs Codex's apply_patch/shell).
//
// Every field beyond Kind is best-effort: an agent that supplies only a path+op leaves Diff/OldText/
// NewText empty, and Claude cannot report a shell exit code or stderr at all — hence the POINTERS on
// ShellCommand. Empty is "the agent didn't tell us", never "0" or "no change"; clients must treat it so.

// ToolKind is the canonical, agent-independent classification of a tool call.
type ToolKind string

const (
	KindFileEdit  ToolKind = "file_edit"  // create / edit / delete a file
	KindFileRead  ToolKind = "file_read"  // read a file
	KindShell     ToolKind = "shell"      // run a shell command
	KindSearch    ToolKind = "search"     // search the workspace (grep / glob)
	KindWebSearch ToolKind = "web_search" // search the web
	KindWebFetch  ToolKind = "web_fetch"  // fetch a URL
	KindMCP       ToolKind = "mcp"        // an MCP server tool call
	KindOther     ToolKind = "other"      // anything not yet classified
)

// ToolAction is the normalized view of a tool call. Only the sub-object matching Kind is populated;
// the rest stay nil/empty. It attaches to both ToolEvent (live stream) and ToolPart (transcript) as the
// same pointer type, so use and result carry the progressively-completed same shape.
type ToolAction struct {
	Kind   ToolKind      `json:"kind"`
	Title  string        `json:"title,omitempty"` // short human label (e.g. the command, the path)
	Files  []FileChange  `json:"files,omitempty"`
	Shell  *ShellCommand `json:"shell,omitempty"`
	Search *SearchQuery  `json:"search,omitempty"`
	Web    *WebSearch    `json:"web,omitempty"`
	MCP    *MCPCall      `json:"mcp,omitempty"`
}

// FileChange is one file touched by a file_edit action. Op is "create" | "edit" | "delete". Diff (a
// best-effort unified diff), OldText, and NewText are all optional: present only when the agent supplies
// enough to compute them (e.g. Codex's live file_change reports path+op only, so Diff is empty there).
type FileChange struct {
	Path    string `json:"path"`
	Op      string `json:"op,omitempty"`
	Diff    string `json:"diff,omitempty"`    // best-effort; absent when the agent doesn't supply enough
	OldText string `json:"oldText,omitempty"` // best-effort
	NewText string `json:"newText,omitempty"` // best-effort
}

// ShellCommand is a shell action. Stderr and ExitCode are POINTERS: nil means the agent didn't report
// them (Claude's Bash tool merges stderr into stdout and never surfaces an exit code), which is
// distinct from an empty stderr or exit 0. Stdout is the combined/aggregated output as the agent gave it.
type ShellCommand struct {
	Command  string `json:"command,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`   // best-effort; nil pointer ⇒ not reported (≠ "")
	ExitCode *int   `json:"exitCode,omitempty"` // best-effort; nil ⇒ not reported (≠ 0)
}

// SearchQuery is a workspace-search action (grep/glob). Fields are populated as the tool supplies them.
type SearchQuery struct {
	Query string `json:"query,omitempty"`
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
}

// WebSearch covers both a web search (Query) and a URL fetch (URL); Kind on the parent disambiguates.
type WebSearch struct {
	Query string `json:"query,omitempty"`
	URL   string `json:"url,omitempty"`
}

// MCPCall identifies an MCP server tool invocation.
type MCPCall struct {
	Server string `json:"server,omitempty"`
	Tool   string `json:"tool,omitempty"`
}

// MapChangeOp normalizes an agent's file-change verb (Codex uses "add"/"modify"/"delete", etc.) to the
// canonical Op vocabulary ("create" | "edit" | "delete"). Unknown verbs default to "edit".
func MapChangeOp(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "add", "create", "created", "new":
		return "create"
	case "delete", "deleted", "remove", "removed":
		return "delete"
	default:
		return "edit"
	}
}

// Diff-size guards. The stream scanners buffer up to 16 MiB, and both OldText/NewText are persisted
// alongside the diff, so an unbounded diff would bloat every transcript. Above these thresholds
// BuildUnifiedDiff returns "" (callers keep OldText/NewText) or appends a truncation marker.
const (
	maxDiffLines = 20000     // skip the O(n*m) LCS entirely past this many lines on either side
	maxDiffCells = 4_000_000 // and skip when len(old)*len(new) exceeds this (dp matrix budget)
	maxDiffBytes = 128 * 1024
	diffContext  = 3 // unchanged lines of context around each hunk (git default)
)

// BuildUnifiedDiff produces a best-effort unified diff (git-style, with a/ b/ headers and @@ hunks) from
// oldText → newText using a hand-rolled line-based LCS (the daemon carries zero third-party deps). It
// returns "" when there is no change or when the inputs are too large to diff cheaply; the caller then
// falls back to OldText/NewText. Very large diffs are truncated with a marker so transcripts stay bounded.
func BuildUnifiedDiff(path, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	a := splitLines(oldText)
	b := splitLines(newText)
	if len(a) > maxDiffLines || len(b) > maxDiffLines || len(a)*len(b) > maxDiffCells {
		return "" // too big to diff cheaply — caller keeps old/new text
	}

	ops := diffLines(a, b)

	// Annotate each op with its 1-based old/new line number, then group changes into hunks with context.
	type annOp struct {
		kind         byte // ' ' | '-' | '+'
		line         string
		oldNo, newNo int // 0 when the line doesn't exist on that side
	}
	ann := make([]annOp, len(ops))
	oldNo, newNo := 0, 0
	changed := false
	for i, op := range ops {
		a := annOp{kind: op.kind, line: op.line}
		switch op.kind {
		case ' ':
			oldNo++
			newNo++
			a.oldNo, a.newNo = oldNo, newNo
		case '-':
			oldNo++
			a.oldNo = oldNo
			changed = true
		case '+':
			newNo++
			a.newNo = newNo
			changed = true
		}
		ann[i] = a
	}
	if !changed {
		return ""
	}

	// Merge each changed op's ±context window into hunk ranges (inclusive op-index bounds).
	type rng struct{ start, end int }
	var ranges []rng
	for i, a := range ann {
		if a.kind == ' ' {
			continue
		}
		s := i - diffContext
		if s < 0 {
			s = 0
		}
		e := i + diffContext
		if e > len(ann)-1 {
			e = len(ann) - 1
		}
		if n := len(ranges); n > 0 && s <= ranges[n-1].end+1 {
			ranges[n-1].end = e // overlapping/adjacent — extend the previous hunk
		} else {
			ranges = append(ranges, rng{s, e})
		}
	}

	var sb strings.Builder
	if path != "" {
		sb.WriteString("--- a/" + path + "\n")
		sb.WriteString("+++ b/" + path + "\n")
	}
	for _, r := range ranges {
		var oldStart, newStart, oldCount, newCount int
		for k := r.start; k <= r.end; k++ {
			if ann[k].oldNo != 0 {
				if oldStart == 0 {
					oldStart = ann[k].oldNo
				}
				oldCount++
			}
			if ann[k].newNo != 0 {
				if newStart == 0 {
					newStart = ann[k].newNo
				}
				newCount++
			}
		}
		sb.WriteString("@@ -")
		sb.WriteString(strconv.Itoa(oldStart) + "," + strconv.Itoa(oldCount))
		sb.WriteString(" +")
		sb.WriteString(strconv.Itoa(newStart) + "," + strconv.Itoa(newCount))
		sb.WriteString(" @@\n")
		for k := r.start; k <= r.end; k++ {
			sb.WriteByte(ann[k].kind)
			sb.WriteString(ann[k].line)
			sb.WriteByte('\n')
		}
		if sb.Len() > maxDiffBytes {
			sb.WriteString("… diff truncated …\n")
			break
		}
	}
	return sb.String()
}

// diffOp is one line of an edit script: kind is ' ' (unchanged), '-' (removed), or '+' (added).
type diffOp struct {
	kind byte
	line string
}

// diffLines computes a line edit script from a → b via an LCS backtrack. Longest-common-subsequence
// gives the minimal set of add/remove ops; unchanged lines are the subsequence.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// dp[i][j] = LCS length of a[i:], b[j:]. Filled from the bottom-right so the forward walk below
	// can greedily reconstruct one optimal alignment.
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// splitLines splits text into lines without the trailing newline of each line, dropping the single
// phantom empty element a trailing "\n" would otherwise produce (so "a\n" is one line, not two).
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
