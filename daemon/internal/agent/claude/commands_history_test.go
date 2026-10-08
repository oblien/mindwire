package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandHistoryHidesOnlyNativeExpansionAndKeepsTheNextUserMessage(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	write := func(id, text string, meta bool) {
		t.Helper()
		err := json.NewEncoder(file).Encode(map[string]any{"type": "user", "uuid": id, "isMeta": meta,
			"message": map[string]any{"role": "user", "content": text}})
		if err != nil {
			t.Fatal(err)
		}
	}
	write("command", "<command-message>review</command-message>\n<command-name>/review</command-name>\n<command-args>src</command-args>", false)
	write("expanded", "Instructions expanded from the skill file", true)
	write("next", "Please also inspect the tests", false)
	write("unrelated", "Another native user record", true)
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	history, err := parseTranscript(file, "chat")
	if err != nil || len(history) != 3 {
		t.Fatalf("history: %+v %v", history, err)
	}
	if history[0].Text != "/review src" || history[0].Command == nil || history[0].Parts[0].Text != "/review src" {
		t.Fatalf("command envelope leaked into presentation: %+v", history[0])
	}
	if history[1].ID != "next" || history[2].ID != "unrelated" {
		t.Fatal("command expansion hid unrelated history")
	}
}
