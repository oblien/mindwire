package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

func metadataLine(value map[string]any) string { b, _ := json.Marshal(value); return string(b) + "\n" }

func nativeUser(id, cwd, text string) string {
	return metadataLine(map[string]any{"type": "user", "sessionId": id, "cwd": cwd, "uuid": "message-" + id,
		"timestamp": "2026-09-24T12:00:00Z", "message": map[string]any{"role": "user", "content": text}})
}

func TestNativeSessionsReadClaudeMetadataAndPreserveHistoryIdentity(t *testing.T) {
	home, cwd := t.TempDir(), filepath.Join(t.TempDir(), "project_name.中文")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	slug := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
	first := nativeUser("one", cwd, "First\nquestion") + metadataLine(map[string]any{"type": "ai-title", "sessionId": "one", "aiTitle": "Generated title"}) + metadataLine(map[string]any{"type": "custom-title", "sessionId": "one", "customTitle": "My native title"})
	mkTranscript(t, home, slug, "one", first)
	mkTranscript(t, home, projectSlug(cwd), "two", nativeUser("two", cwd, "Another conversation"))
	mkTranscript(t, home, slug, "wrong", nativeUser("wrong", cwd+"-sibling", "Wrong directory"))
	mkTranscript(t, home, slug, "agent-helper", nativeUser("agent-helper", cwd, "Subagent"))
	mkTranscript(t, home, slug, "sidechain", metadataLine(map[string]any{"type": "user", "sessionId": "sidechain", "cwd": cwd, "isSidechain": true, "message": map[string]any{"content": "Hidden"}}))
	mkTranscript(t, home, slug, "malformed", "not json\n")
	rows, err := (adapter{}).ListSessions(context.Background(), cwd)
	if err != nil || len(rows) != 2 {
		t.Fatalf("native list: %+v %v", rows, err)
	}
	for _, row := range rows {
		canonical, _ := workspacepath.Canonical(cwd)
		if row.CWD != canonical {
			t.Fatalf("wrong context: %+v", row)
		}
		if row.ID == "one" && row.Title != "My native title" {
			t.Fatal(row.Title)
		}
	}
	// The discovered ID goes straight back into the existing native reader.
	messages, err := (adapter{}).History(agent.HistoryQuery{ChatID: "app-chat", SessionID: "one", CWD: cwd})
	if err != nil || len(messages) != 1 || messages[0].Text != "First\nquestion" {
		t.Fatalf("history: %+v %v", messages, err)
	}
}

func TestNativeSessionMetadataReadsBoundedPrefixAndTail(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	// A >16 MiB line in the middle would fail a full transcript scan. Listing
	// only needs the small metadata prefix and the latest native title at EOF.
	body := nativeUser("large", cwd, "First prompt") + metadataLine(map[string]any{"type": "progress", "payload": strings.Repeat("x", 1<<20)}) + strings.Repeat("x", 17<<20) + "\n" + metadataLine(map[string]any{"type": "custom-title", "sessionId": "large", "customTitle": "Latest native name"})
	mkTranscript(t, home, projectSlug(cwd), "large", body)
	rows, err := (adapter{}).ListSessions(context.Background(), cwd)
	if err != nil || len(rows) != 1 || rows[0].Title != "Latest native name" {
		t.Fatalf("bounded metadata: %+v %v", rows, err)
	}
}

func TestNativeSessionListResolvesCallerSymlinkAndRejectsCancelledScan(t *testing.T) {
	home, real := t.TempDir(), t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	mkTranscript(t, home, projectSlug(real), "native", nativeUser("native", real, "Hello"))
	rows, err := (adapter{}).ListSessions(context.Background(), link)
	if err != nil || len(rows) != 1 {
		t.Fatalf("alias: %+v %v", rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (adapter{}).ListSessions(ctx, link); err == nil {
		t.Fatal("cancelled scan succeeded")
	}
}

func TestNativeSessionListSmoke(t *testing.T) {
	cwd := os.Getenv("MINDWIRE_TEST_NATIVE_SESSION_CWD")
	if cwd == "" {
		t.Skip("explicit read-only native metadata check")
	}
	rows, err := (adapter{}).ListSessions(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native Claude conversations discovered: %d", len(rows))
}
