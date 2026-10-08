package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/session"
)

func TestConditionalNativeHistorySeesExternalCLIChangesWithoutRedownloadingUnchangedOutput(t *testing.T) {
	for _, harness := range []string{"codex", "claude-code"} {
		t.Run(harness, func(t *testing.T) {
			base, cwd := t.TempDir(), t.TempDir()
			t.Setenv("CODEX_HOME", base)
			t.Setenv("CLAUDE_CONFIG_DIR", base)
			handler, store := newChatTestEnv(t, cwd)
			if err := store.SetSession(harness, "chat", "session"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(base, "sessions", "rollout-test-session.jsonl")
			if harness == "claude-code" {
				path = claudeTranscriptPath(base, cwd, "session")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			line := func(text string) []byte {
				var value any
				if harness == "codex" {
					value = map[string]any{"timestamp": "2026-10-07T12:00:00Z", "type": "event_msg",
						"payload": map[string]any{"type": "agent_message", "message": text}}
				} else {
					value = map[string]any{"timestamp": "2026-10-07T12:00:00Z", "type": "assistant", "uuid": "answer",
						"message": map[string]any{"role": "assistant", "content": text}}
				}
				encoded, _ := json.Marshal(value)
				return append(encoded, '\n')
			}
			if err := os.WriteFile(path, line(strings.Repeat("large tool result ", 20000)), 0600); err != nil {
				t.Fatal(err)
			}
			endpoint := "/chats/chat/messages?agent=" + harness + "&paged=true&limit=12&maxBytes=1048576"
			read := func(suffix string) (messagePage, int) {
				t.Helper()
				response := serve(t, handler, "GET", endpoint+suffix, "")
				if response.Code != http.StatusOK {
					t.Fatalf("history status %d: %s", response.Code, response.Body.String())
				}
				var page messagePage
				if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				return page, response.Body.Len()
			}
			first, bytes := read("")
			if first.Revision == "" || len(first.Messages) != 1 || first.NotModified {
				t.Fatal("initial page missing its native revision")
			}
			unchanged, small := read("&ifRevision=" + first.Revision)
			if !unchanged.NotModified || len(unchanged.Messages) != 0 || small > 180 || bytes < 200000 {
				t.Fatalf("unchanged read downloaded history: %d -> %d", bytes, small)
			}
			// Other chats and ordinary daemon configuration do not invalidate this one.
			_ = store.AddMessage(session.Message{ID: "other", ChatID: "other", Text: "another conversation"})
			_ = store.Set("codex:model", "fixture")
			still, _ := read("&ifRevision=" + first.Revision)
			if !still.NotModified {
				t.Fatal("unrelated daemon activity invalidated the cached chat")
			}
			if err := os.WriteFile(path, line("Native CLI continued this conversation"), 0600); err != nil {
				t.Fatal(err)
			}
			changed, _ := read("&ifRevision=" + first.Revision)
			if changed.NotModified || changed.Revision == first.Revision || !strings.Contains(string(changed.Messages[0]), "Native CLI continued") {
				t.Fatal("external harness changes were hidden by a stale cache")
			}
			// A new inode is a new transcript even with the same size and timestamp.
			stamp, _ := os.Stat(path)
			if err := os.WriteFile(path+".new", line("Native CLI continued this conversatioN"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path+".new", stamp.ModTime(), stamp.ModTime()); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path+".new", path); err != nil {
				t.Fatal(err)
			}
			replaced, _ := read("&ifRevision=" + changed.Revision)
			if replaced.NotModified || replaced.Revision == changed.Revision {
				t.Fatal("atomic transcript replacement was missed")
			}
			_ = store.SaveRun(session.Run{ID: "run", ChatID: "chat", Agent: harness, Status: "cancelled"})
			terminal, _ := read("&ifRevision=" + replaced.Revision)
			if terminal.NotModified {
				t.Fatal("run status changes must invalidate history overlays")
			}
			invalid := serve(t, handler, "GET", "/chats/chat/messages?agent="+harness+"&paged=true&limit=0&ifRevision="+terminal.Revision, "")
			if invalid.Code != http.StatusBadRequest {
				t.Fatal("conditional reads must still validate paging parameters")
			}
			t.Logf("%s unchanged native history: %d bytes instead of %d", harness, small, bytes)
		})
	}
}
