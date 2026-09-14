package codex

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func (st *streamState) filePath(path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) && st.cwd != "" {
		path = filepath.Join(st.cwd, path)
	}
	return filepath.Clean(path)
}

// splitTurnDiff retains complete per-file patches, including creates, deletes, renames
// and quoted paths. The aggregate is a snapshot of applied changes, not a text delta.
func splitTurnDiff(diff string) []normChange {
	var chunks []string
	var chunk strings.Builder
	gitFormat := strings.Contains(diff, "diff --git ")
	for _, line := range strings.SplitAfter(diff, "\n") {
		boundary := strings.HasPrefix(line, "diff --git ") || !gitFormat && strings.HasPrefix(line, "--- ")
		if boundary && chunk.Len() > 0 {
			chunks = append(chunks, chunk.String())
			chunk.Reset()
		}
		chunk.WriteString(line)
	}
	if chunk.Len() > 0 {
		chunks = append(chunks, chunk.String())
	}
	path := func(value string) string {
		value = strings.TrimSuffix(value, "\r")
		if strings.HasPrefix(value, "\"") {
			if decoded, err := strconv.Unquote(value); err == nil {
				value = decoded
			}
		} else if i := strings.IndexByte(value, '\t'); i >= 0 {
			value = value[:i]
		}
		if strings.HasPrefix(value, "a/") || strings.HasPrefix(value, "b/") {
			value = value[2:]
		}
		return value
	}
	var changes []normChange
	for _, patch := range chunks {
		c := normChange{Kind: "update", Diff: patch}
		for _, line := range strings.Split(patch, "\n") {
			if strings.HasPrefix(line, "@@") {
				break // Added/deleted file content is not another file header.
			}
			switch {
			case strings.HasPrefix(line, "--- "):
				old := path(strings.TrimPrefix(line, "--- "))
				if old == "/dev/null" {
					c.Kind = "add"
				} else {
					c.Path = old
				}
			case strings.HasPrefix(line, "+++ "):
				next := path(strings.TrimPrefix(line, "+++ "))
				if next == "/dev/null" {
					c.Kind = "delete"
				} else {
					c.Path = next
				}
			case strings.HasPrefix(line, "rename to "):
				c.Path = path(strings.TrimPrefix(line, "rename to "))
			case strings.HasPrefix(line, "diff --git "):
				// Older snapshots can omit ---/+++ headers. Use the destination when unquoted.
				if _, next, ok := strings.Cut(line, " b/"); ok {
					c.Path = next
				}
			case strings.HasPrefix(line, "new file mode "):
				c.Kind = "add"
			case strings.HasPrefix(line, "deleted file mode "):
				c.Kind = "delete"
			}
		}
		if c.Path != "" {
			changes = append(changes, c)
		}
	}
	return changes
}

// Fill missing native patches and expose aggregate-only paths using the same file_edit
// protocol. Existing per-operation patches win; one unrelated native patch must not
// suppress all the other files in an aggregate update.
func (st *streamState) emitTurnDiff(turnID string, emit agent.Emit) {
	changes := splitTurnDiff(st.turnDiff)
	var fallback []normChange
	for _, change := range changes {
		path := st.filePath(change.Path)
		matched := false
		for i := len(st.fileItems) - 1; i >= 0; i-- {
			id := st.fileItems[i]
			n := st.items[id]
			for j, native := range n.Changes {
				if st.filePath(native.Path) != path && st.filePath(native.MovePath) != path {
					continue
				}
				matched = true
				if native.Diff == "" || st.aggregateFilled[id+"\x00"+path] {
					n.Changes = append([]normChange(nil), n.Changes...)
					filled := change
					filled.Path, filled.MovePath = native.Path, native.MovePath
					filled.Kind = agent.FirstNonEmpty(native.Kind, change.Kind)
					n.Changes[j] = filled
					phase := phaseUpdated
					if st.completed[id] {
						phase = phaseCompleted
					}
					emitNorm(n, phase, st.raw[id], emit, st)
					st.aggregateFilled[id+"\x00"+path] = true
				}
				break
			}
			if matched {
				break
			}
		}
		if !matched || st.aggregatePaths[path] {
			fallback = append(fallback, change)
			st.aggregatePaths[path] = true
		}
	}
	if len(fallback) > 0 {
		st.aggregateID = "turn-diff-" + turnID
		n := normItem{ID: st.aggregateID, Kind: kindFileChange, Changes: fallback, Status: "completed"}
		// Aggregate updates describe changes already applied to disk. Updating the stable
		// tool ID replaces its file snapshot immediately, without waiting for turn/completed.
		st.visible = true
		st.emitTool(n, phaseCompleted, nil, emit)
	}
}
