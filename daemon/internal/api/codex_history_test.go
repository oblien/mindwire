package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
)

func TestRunningChatHistoryExcludesNativePartialAssistant(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CODEX_HOME", base)
	handler, store := newChatTestEnv(t, t.TempDir())
	if err := store.SetSession("codex", "chat", "session"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "sessions", "rollout-test-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := `{"timestamp":"2026-09-14T09:00:00Z","type":"event_msg","payload":{"type":"user_message","message":"Earlier question from a forked session"}}
{"timestamp":"2026-09-14T09:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"Earlier answer"}}
{"timestamp":"2026-09-14T10:00:00Z","type":"event_msg","payload":{"type":"user_message","message":"Build an app"}}
{"timestamp":"2026-09-14T10:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"Creating the files."}}
`
	if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.AddMessage(session.Message{ID: "user", ChatID: "chat", Role: "user", Text: "Build an app", CreatedAt: "2026-09-14T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	run := session.Run{ID: "run", ChatID: "chat", Agent: "codex", Status: "running", CreatedAt: "2026-09-14T10:00:00Z"}
	if err := store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	read := func() []agent.Message {
		t.Helper()
		response := serve(t, handler, "GET", "/chats/chat/messages?agent=codex", "")
		var messages []agent.Message
		if response.Code != http.StatusOK {
			t.Fatalf("history: %d %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &messages); err != nil {
			t.Fatal(err)
		}
		return messages
	}
	if messages := read(); len(messages) != 3 || messages[2].ID != "user" || messages[1].Text != "Earlier answer" {
		t.Fatalf("active output duplicated in history: %+v", messages)
	}
	run.Status = "done"
	if err := store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if messages := read(); len(messages) != 4 || messages[3].Text != "Creating the files." {
		t.Fatalf("idle chat must still read native history: %+v", messages)
	}
}

func TestCodexMessagesPreserveRecordedWarningsAndErrorsWithNativeHistory(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CODEX_HOME", base)
	handler, store := newChatTestEnv(t, t.TempDir())
	if err := store.SetSession("codex", "chat", "session"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "sessions", "rollout-test-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-09-11T10:00:00.100Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"user","content":[{"type":"text","text":"Inspect it"}]}}}`,
		`{"timestamp":"2026-09-11T10:00:01Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"answer","content":[{"type":"output_text","text":"Done"}]}}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, message := range []session.Message{
		{ID: "u1", ChatID: "chat", Role: "user", Text: "Inspect it", CreatedAt: "2026-09-11T10:00:00Z"},
		{ID: "a1", ChatID: "chat", Role: "assistant", Text: "Done", CreatedAt: "2026-09-11T10:00:02Z", Parts: []agent.Part{
			{Type: "interaction", Interaction: &agent.Interaction{ID: "warning", Kind: "warning", Title: "Configuration warning", Detail: "One setting was ignored."}},
			{Type: "text", ID: "item_0", Text: "Done"},
		}},
		{ID: "u2", ChatID: "chat", Role: "user", Text: "Try the other model", CreatedAt: "2026-09-11T10:01:00Z"},
		{ID: "a2", ChatID: "chat", Role: "assistant", CreatedAt: "2026-09-11T10:01:01Z", Parts: []agent.Part{
			{Type: "interaction", Interaction: &agent.Interaction{ID: "failure", Kind: "error", Title: "Turn failed", Detail: "Deployment not found (HTTP 404)"}},
		}},
	} {
		if err := store.AddMessage(message); err != nil {
			t.Fatal(err)
		}
	}

	response := serve(t, handler, "GET", "/chats/chat/messages?agent=codex", "")
	if response.Code != http.StatusOK {
		t.Fatalf("messages: %d %s", response.Code, response.Body.String())
	}
	var messages []agent.Message
	if err := json.Unmarshal(response.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || len(messages[1].Parts) != 2 || messages[1].Parts[0].Interaction.Kind != "warning" || messages[1].Text != "Done" || messages[3].Parts[0].Interaction.Detail != "Deployment not found (HTTP 404)" {
		t.Fatalf("native history hid recorded diagnostics: %+v", messages)
	}
	if messages[1].Parts[1].ID != "item_0" {
		t.Fatal("history did not preserve the item identity delivered to the client")
	}
	response = serve(t, handler, "GET", "/chats/chat/messages?agent=codex&limit=1", "")
	if err := json.Unmarshal(response.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "a2" {
		t.Fatalf("pagination lost the failed turn: %+v", messages)
	}
	response = serve(t, handler, "GET", "/chats/chat/messages?agent=codex&before=a2&limit=1", "")
	if err := json.Unmarshal(response.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "u2" {
		t.Fatalf("pagination cursor changed: %+v", messages)
	}
}
