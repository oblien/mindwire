package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The adapter owns Claude's native transcript, so it can both read and delete it.
var _ agent.HistoryDeleter = adapter{}

// History reads Claude Code's own transcript (the agent owns its data) and maps it
// to unified messages. Best-effort: returns nil on any miss so the caller can fall
// back to the recorded log.
func (adapter) History(q agent.HistoryQuery) ([]agent.Message, error) {
	if q.SessionID == "" {
		return nil, nil
	}
	base := configBase()
	if base == "" {
		return nil, nil // can't resolve home; caller falls back to the recorded log
	}
	path := findTranscript(base, q.CWD, q.SessionID)
	if path == "" {
		return nil, nil // no transcript on disk; caller falls back to the recorded log
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []agent.Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec struct {
			Type             string          `json:"type"`
			Subtype          string          `json:"subtype"`
			UUID             string          `json:"uuid"`
			Timestamp        string          `json:"timestamp"`
			Message          json.RawMessage `json:"message"`
			Content          string          `json:"content"`          // compact_boundary line
			CompactMetadata  json.RawMessage `json:"compactMetadata"`  // compact_boundary metadata
			IsCompactSummary bool            `json:"isCompactSummary"` // the continuation-summary user record
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		// A compaction boundary: surface it as a standalone compaction message so a reloaded transcript
		// shows the same marker the live stream emitted (see parseStream's compact_boundary case).
		if rec.Type == "system" && rec.Subtype == "compact_boundary" {
			out = append(out, agent.Message{
				ID: rec.UUID, ChatID: q.ChatID, Role: "system", CreatedAt: rec.Timestamp,
				Parts: []agent.Part{{Type: "compaction", At: rec.Timestamp,
					Compaction: compactionFromMeta(rec.Content, rec.CompactMetadata)}},
			})
			continue
		}
		if rec.Type != "user" && rec.Type != "assistant" {
			continue
		}
		// The continuation-summary record (Claude injects it right after the boundary, or at the head of a
		// resumed session) is machine-generated context, not a real user turn — never a giant user bubble.
		// When it follows a boundary we just emitted, fold the summary text onto that marker; otherwise it
		// stands alone as its own compaction marker.
		if rec.Type == "user" && rec.IsCompactSummary {
			summary := strings.TrimSpace(messageText(rec.Message))
			if n := len(out); n > 0 && isCompactionMarker(out[n-1]) {
				out[n-1].Parts[0].Compaction.Summary = summary
			} else {
				out = append(out, agent.Message{
					ID: rec.UUID, ChatID: q.ChatID, Role: "system", CreatedAt: rec.Timestamp,
					Parts: []agent.Part{{Type: "compaction", At: rec.Timestamp,
						Compaction: &agent.CompactionInfo{Summary: summary}}},
				})
			}
			continue
		}
		text := strings.TrimSpace(messageText(rec.Message))
		parts := messageParts(rec.Message)
		// Claude records tool RESULTS as role "user" with no text — fold them onto the preceding
		// assistant turn (correlate by tool-use id) instead of emitting a stray empty user bubble.
		if rec.Type == "user" && text == "" && len(parts) > 0 && allTool(parts) {
			if n := len(out); n > 0 && out[n-1].Role == "assistant" {
				mergeToolResults(&out[n-1], parts)
				continue
			}
		}
		if text == "" && len(parts) == 0 {
			continue
		}
		out = append(out, agent.Message{
			ID: rec.UUID, ChatID: q.ChatID, Role: rec.Type, Text: text, Parts: parts, CreatedAt: rec.Timestamp,
		})
	}
	if err := sc.Err(); err != nil {
		// A read error (e.g. a line over the 16 MiB cap) truncated the transcript. Surface it
		// so the caller falls back to the recorded log rather than serving a partial history.
		return nil, err
	}
	return out, nil
}

// DeleteHistory removes Claude Code's native transcript for a session — the projects/<slug>/<sid>.jsonl
// file findTranscript locates (the same path History reads). Best-effort and idempotent: a missing
// session id, unresolvable home, or already-absent file is not an error, so a true chat delete never
// fails just because the agent left nothing on disk. The bool reports whether a file was ACTUALLY
// removed, so the caller only claims `nativePurged` for transcripts that truly existed — a genuinely
// absent file is (false, nil), not a false "purged".
//
// This implements agent.HistoryDeleter; the API's DELETE /chats/{id} drives it per session.
func (adapter) DeleteHistory(q agent.HistoryQuery) (bool, error) {
	if q.SessionID == "" {
		return false, nil
	}
	base := configBase()
	if base == "" {
		return false, nil
	}
	path := findTranscript(base, q.CWD, q.SessionID)
	if path == "" {
		return false, nil // nothing on disk for this session
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil // raced away between find and remove — treat as absent
		}
		return false, err
	}
	return true, nil
}

// Title returns Claude Code's own auto-generated session title. Claude writes it into the session's
// transcript as a record of shape {"type":"ai-title","aiTitle":"…","sessionId":"…"}, a few records
// in (once the CLI has summarized the chat) and RE-EMITS it as the chat evolves — so the LAST
// ai-title wins. Best-effort: returns "" on any miss so the caller keeps its derived fallback.
//
// This implements agent.Titler; the API prefers it over the first-user-message snippet.
func (adapter) Title(q agent.HistoryQuery) string {
	if q.SessionID == "" {
		return ""
	}
	base := configBase()
	if base == "" {
		return ""
	}
	path := findTranscript(base, q.CWD, q.SessionID)
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	title := ""
	needle := []byte(`"ai-title"`)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		// Cheap pre-filter: only JSON-decode lines that could be the title record, so listing chats
		// doesn't pay a full unmarshal per line of a large transcript.
		if !bytes.Contains(line, needle) {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			AITitle string `json:"aiTitle"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "ai-title" {
			continue
		}
		if t := strings.TrimSpace(rec.AITitle); t != "" {
			title = t // keep scanning — a later record supersedes it
		}
	}
	return title
}

// isCompactionMarker reports whether a message is a standalone compaction marker (one compaction
// part) — the boundary record the summary that follows it folds onto.
func isCompactionMarker(m agent.Message) bool {
	return len(m.Parts) == 1 && m.Parts[0].Type == "compaction" && m.Parts[0].Compaction != nil
}

// allTool reports whether every part is a tool part (i.e. a tool_result-only "user" record).
func allTool(parts []agent.Part) bool {
	for _, p := range parts {
		if p.Type != "tool" {
			return false
		}
	}
	return len(parts) > 0
}

// mergeToolResults attaches tool_result outputs (a following "user" record) onto the matching
// tool_use parts of the assistant message, correlated by tool-use id.
func mergeToolResults(m *agent.Message, results []agent.Part) {
	for _, r := range results {
		if r.Tool == nil {
			continue
		}
		matched := false
		for i := range m.Parts {
			if m.Parts[i].Type == "tool" && m.Parts[i].Tool != nil && m.Parts[i].Tool.ID == r.Tool.ID {
				tp := m.Parts[i].Tool
				tp.Output = r.Tool.Output
				tp.IsError = r.Tool.IsError
				// The tool_use part carries the name+input; recompute the action now that the output is
				// known, so reloaded Bash cards get their stdout (the input-only action lacked it).
				tp.Action = claudeToolAction(tp.Name, tp.Input, tp.Output, tp.IsError)
				matched = true
				break
			}
		}
		if !matched {
			m.Parts = append(m.Parts, r) // defensive: unmatched result — show it anyway
		}
	}
}

// projectSlug mirrors Claude Code's project-dir naming: the absolute, SYMLINK-RESOLVED cwd with path
// separators replaced by '-'. Claude Code resolves symlinks when it writes the transcript, so the slug
// MUST resolve them too — otherwise a chat under a symlinked cwd (macOS /tmp → /private/tmp, or any
// symlinked project path) computes a divergent slug and silently misses the transcript. EvalSymlinks
// requires the path to exist; when it doesn't (dir removed), we fall back to the un-resolved abs path,
// and the cross-project findTranscript glob is the final safety net.
func projectSlug(cwd string) string {
	abs := cwd
	if a, err := filepath.Abs(cwd); err == nil {
		abs = a
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return strings.NewReplacer("/", "-", "\\", "-").Replace(abs)
}

// findTranscript locates a session's transcript under <base>/projects. It tries the exact cwd slug
// first (the common case), then falls back to globbing projects/*/<sid>.jsonl for the session id — so
// reads/title/delete stay robust to cwd drift and to Claude's now-cwd-independent --resume (v2.1.223+),
// which can leave a session's records under a different project dir than where it started. Session ids
// are UUIDs, so a cross-project collision is negligible; the exact-slug hit is always preferred. Returns
// "" when no transcript exists for the session id.
func findTranscript(base, cwd, sid string) string {
	exact := filepath.Join(base, "projects", projectSlug(cwd), sid+".jsonl")
	if _, err := os.Stat(exact); err == nil {
		return exact
	}
	matches, _ := filepath.Glob(filepath.Join(base, "projects", "*", sid+".jsonl"))
	if len(matches) > 0 {
		return matches[0] // Glob returns lexically-sorted matches → deterministic
	}
	return ""
}
