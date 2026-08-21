package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// The file channel appends each notification as one JSON line; a second send appends rather than
// truncates, so the file is a growing JSONL log.
func TestFileNotifierAppendsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.jsonl")
	f := &Notifier{Path: path}

	if err := f.Notify(context.Background(), agent.Notification{Condition: agent.Finished, Title: "one", ChatID: "c1"}); err != nil {
		t.Fatalf("notify 1: %v", err)
	}
	if err := f.Notify(context.Background(), agent.Notification{Condition: agent.WaitingApproval, Title: "two", ChatID: "c2"}); err != nil {
		t.Fatalf("notify 2: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d:\n%s", len(lines), body)
	}
	var first agent.Notification
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 0 is not valid JSON: %v", err)
	}
	if first.Title != "one" || first.ChatID != "c1" {
		t.Errorf("line 0 = %+v, want the first notification", first)
	}
}

// factory enables the channel only when NOTIFY_FILE is set.
func TestFactoryEnvGated(t *testing.T) {
	t.Setenv("NOTIFY_FILE", "")
	if _, ok := factory(nil); ok {
		t.Error("factory should be disabled without NOTIFY_FILE")
	}
	path := filepath.Join(t.TempDir(), "n.jsonl")
	t.Setenv("NOTIFY_FILE", path)
	n, ok := factory(nil)
	if !ok || n == nil {
		t.Fatal("factory should be enabled with NOTIFY_FILE set")
	}
	if fn, _ := n.(*Notifier); fn == nil || fn.Path != path {
		t.Errorf("factory should target NOTIFY_FILE, got %+v", n)
	}
}
