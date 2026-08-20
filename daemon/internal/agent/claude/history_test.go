package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// mkTranscript writes a one-line transcript at <base>/projects/<slug>/<sid>.jsonl and returns its path.
func mkTranscript(t *testing.T, base, slug, sid, body string) string {
	t.Helper()
	dir := filepath.Join(base, "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(dir, sid+".jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// userLine is a minimal Claude transcript record for a user message with the given text.
func userLine(text string) string {
	return fmt.Sprintf(`{"type":"user","uuid":"u1","timestamp":"2026-01-01T00:00:00Z","message":{"content":%q}}`+"\n", text)
}

// symlinkDir creates a canonical real dir plus a symlink pointing at it, skipping the test if the OS
// won't make symlinks. It returns (resolvedReal, linkPath).
func symlinkDir(t *testing.T) (string, string) {
	t.Helper()
	real, err := filepath.EvalSymlinks(t.TempDir()) // canonicalize (macOS /var → /private/var)
	if err != nil {
		t.Fatalf("evalsymlinks real: %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}
	return real, link
}

// TestProjectSlugResolvesSymlinks is the regression test for the core bug: Claude Code writes its
// transcript under the SYMLINK-RESOLVED cwd slug, so projectSlug must resolve symlinks too — otherwise
// a chat under a symlinked cwd computes a divergent slug and misses the transcript.
func TestProjectSlugResolvesSymlinks(t *testing.T) {
	real, link := symlinkDir(t)
	if got, want := projectSlug(link), projectSlug(real); got != want {
		t.Fatalf("projectSlug(symlink)=%q != projectSlug(real)=%q", got, want)
	}
	want := strings.NewReplacer("/", "-", "\\", "-").Replace(real)
	if got := projectSlug(link); got != want {
		t.Fatalf("projectSlug(symlink)=%q, want resolved slug %q (not the symlink path)", got, want)
	}
}

// TestFindTranscript covers exact-slug hit, cross-project glob fallback (cwd drift / cwd-independent
// --resume), and exact-slug preference when the same session id exists under two project dirs.
func TestFindTranscript(t *testing.T) {
	base := t.TempDir()
	sid := "11111111-2222-3333-4444-555555555555"
	cwd := "/Users/x/proj"

	exact := mkTranscript(t, base, projectSlug(cwd), sid, userLine("hi"))

	// Exact slug: found for the matching cwd.
	if got := findTranscript(base, cwd, sid); got != exact {
		t.Fatalf("exact slug: got %q, want %q", got, exact)
	}
	// Cross-project: an unrelated cwd still finds the sid via the glob fallback.
	if got := findTranscript(base, "/some/unrelated/dir", sid); got == "" {
		t.Fatalf("cross-project fallback did not find sid under a different slug")
	}
	// A missing sid resolves to "".
	if got := findTranscript(base, cwd, "no-such-sid"); got != "" {
		t.Fatalf("absent sid: got %q, want \"\"", got)
	}
	// Same sid under a second project dir: the exact-slug hit is still preferred.
	mkTranscript(t, base, "-Users-x-other", sid, userLine("hi"))
	if got := findTranscript(base, cwd, sid); got != exact {
		t.Fatalf("exact slug not preferred over cross-project: got %q, want %q", got, exact)
	}
}

// TestHistoryReadsUnderSymlinkedCWD is the end-to-end read: Claude wrote the transcript under the
// resolved slug; History called with the symlinked cwd must still return the message.
func TestHistoryReadsUnderSymlinkedCWD(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	real, link := symlinkDir(t)
	sid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	mkTranscript(t, base, projectSlug(real), sid, userLine("hello"))

	msgs, err := adapter{}.History(agent.HistoryQuery{ChatID: "c", SessionID: sid, CWD: link})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Text != "hello" {
		t.Fatalf("History under symlinked cwd = %+v, want one 'hello' message", msgs)
	}
}

// TestHistorySurfacesCompaction covers F4b: a compact_boundary system record becomes a standalone
// system/compaction message, and the isCompactSummary user record that immediately follows folds its
// summary text onto that same marker (never a giant user bubble).
func TestHistorySurfacesCompaction(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	cwd := "/Users/x/proj"
	sid := "cccccccc-dddd-eeee-ffff-000000000000"

	body := userLine("first question") +
		`{"type":"system","subtype":"compact_boundary","uuid":"b1","timestamp":"2026-01-01T00:00:01Z","content":"boundary","compactMetadata":{"trigger":"manual","preTokens":120000,"postTokens":30000}}` + "\n" +
		`{"type":"user","uuid":"s1","timestamp":"2026-01-01T00:00:02Z","isCompactSummary":true,"message":{"content":"summary of the earlier conversation"}}` + "\n" +
		userLine("next question")
	mkTranscript(t, base, projectSlug(cwd), sid, body)

	msgs, err := adapter{}.History(agent.HistoryQuery{ChatID: "c", SessionID: sid, CWD: cwd})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// Expect: user(first), system(compaction), user(next) — the summary folds onto the boundary.
	var comp *agent.Message
	for i := range msgs {
		if msgs[i].Role == "system" && len(msgs[i].Parts) == 1 && msgs[i].Parts[0].Type == "compaction" {
			comp = &msgs[i]
		}
		if msgs[i].Role == "user" && strings.Contains(msgs[i].Text, "summary of the earlier") {
			t.Fatalf("isCompactSummary leaked as a user bubble: %+v", msgs[i])
		}
	}
	if comp == nil {
		t.Fatalf("no compaction message surfaced; got %+v", msgs)
	}
	ci := comp.Parts[0].Compaction
	if ci == nil || ci.Trigger != "manual" || ci.PreTokens != 120000 || ci.PostTokens != 30000 {
		t.Fatalf("compaction info = %+v, want trigger=manual pre=120000 post=30000", ci)
	}
	if !strings.Contains(ci.Summary, "summary of the earlier conversation") {
		t.Errorf("summary not folded onto the boundary, got %q", ci.Summary)
	}
}

// TestDeleteHistoryReportsRemovedVsAbsent asserts the truthful (removed, err) contract: a genuinely
// absent transcript reports (false, nil) — never a false "purged" — and a present one reports
// (true, nil) and is actually gone.
func TestDeleteHistoryReportsRemovedVsAbsent(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	cwd := "/Users/x/proj"
	sid := "99999999-8888-7777-6666-555555555555"

	// Absent: (false, nil).
	if removed, err := (adapter{}).DeleteHistory(agent.HistoryQuery{SessionID: sid, CWD: cwd}); err != nil || removed {
		t.Fatalf("absent delete = (%v, %v), want (false, nil)", removed, err)
	}

	// Present: (true, nil), and the file is removed.
	p := mkTranscript(t, base, projectSlug(cwd), sid, userLine("hi"))
	if removed, err := (adapter{}).DeleteHistory(agent.HistoryQuery{SessionID: sid, CWD: cwd}); err != nil || !removed {
		t.Fatalf("present delete = (%v, %v), want (true, nil)", removed, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("transcript still present after delete: statErr=%v", err)
	}
}
