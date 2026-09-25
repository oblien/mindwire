package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestNativeHistoryHTTPPagesReduceLargeToolPayloads(t *testing.T) {
	h, _, _ := newRegistryAPIEnv(t)
	h = registryAuthenticated(h)
	cwd := t.TempDir()
	path := claudeTranscriptPath(os.Getenv("CLAUDE_CONFIG_DIR"), cwd, "large-native")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for i := range 24 {
		id := strconv.Itoa(i)
		stamp := time.Date(2026, 9, 24, 10, i, 0, 0, time.UTC).Format(time.RFC3339Nano)
		for _, row := range []struct {
			role, id string
			content  any
		}{
			{"user", "user-" + id, "Inspect the project " + id},
			{"assistant", "answer-" + id, []any{map[string]any{"type": "tool_use", "id": "tool-" + id,
				"name": "Bash", "input": map[string]any{"command": "git diff --stat"}}}},
			{"user", "result-" + id, []any{map[string]any{"type": "tool_result", "tool_use_id": "tool-" + id,
				"content": strings.Repeat("fixture output\n", 20_000)}}},
		} {
			err = encoder.Encode(map[string]any{"type": row.role, "uuid": row.id, "cwd": cwd,
				"sessionId": "large-native", "timestamp": stamp,
				"message": map[string]any{"role": row.role, "content": row.content}})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := putNativeProject(t, h, "project", cwd)
	if len(snapshot.Chats) != 1 {
		t.Fatal("native session not discovered")
	}
	endpoint := "/chats/" + snapshot.Chats[0].ID + "/messages"
	start := time.Now()
	legacy := serve(t, h, "GET", endpoint+"?limit=40", "")
	firstRead := time.Since(start)
	start = time.Now()
	paged := serve(t, h, "GET", endpoint+"?paged=true&limit=12&maxBytes=1048576", "")
	warmRead := time.Since(start)
	var result messagePage
	if legacy.Code != 200 || paged.Code != 200 || json.Unmarshal(paged.Body.Bytes(), &result) != nil {
		t.Fatal("native history HTTP request failed")
	}
	if len(result.Messages) < 2 || !result.HasMore || result.Before == "" {
		t.Fatal("missing native page metadata")
	}
	if paged.Body.Len()*4 >= legacy.Body.Len() {
		t.Fatal("large initial payload was not reduced")
	}
	t.Logf("native history: old page %d bytes in %s; bounded warm page %d bytes in %s (%d complete messages)",
		legacy.Body.Len(), firstRead.Round(time.Millisecond), paged.Body.Len(), warmRead.Round(time.Millisecond), len(result.Messages))
}

func TestHistoryPageBudgetPreservesEveryNativeMessageAndTool(t *testing.T) {
	var source []agent.Message
	for i, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		source = append(source, agent.Message{ID: id, Role: "assistant", Text: strings.Repeat(id, 256),
			Parts: []agent.Part{{Type: "tool", Tool: &agent.ToolPart{ID: id, Output: strings.Repeat("output", 50+i)}}}})
	}
	id := func(m agent.Message) string { return m.ID }
	var restored []agent.Message
	before := ""
	for {
		page, err := boundedHistory(source, 12, before, 1500, id)
		if err != nil {
			t.Fatal(err)
		}
		var messages []agent.Message
		for _, raw := range page.Messages {
			var message agent.Message
			if err := json.Unmarshal(raw, &message); err != nil {
				t.Fatal(err)
			}
			messages = append(messages, message)
		}
		if len(messages) == 0 {
			t.Fatal("nonterminal empty page")
		}
		restored = append(messages, restored...)
		if !page.HasMore {
			break
		}
		if page.Before == before {
			t.Fatal("cursor failed to advance")
		}
		before = page.Before
	}
	if !reflect.DeepEqual(restored, source) {
		t.Fatal("paging lost or duplicated native content")
	}
}

func TestHistoryPageOversizedAnswerRetainsInputAndLegacyShape(t *testing.T) {
	source := []agent.Message{{ID: "older"}, {ID: "user", Role: "user", Text: "request"},
		{ID: "answer", Role: "assistant", Text: strings.Repeat("large answer", 1000)}}
	id := func(m agent.Message) string { return m.ID }
	page, err := boundedHistory(source, 12, "", 100, id)
	if err != nil || len(page.Messages) != 2 || page.Before != "user" || !page.HasMore {
		t.Fatalf("oversized context: len=%d before=%s more=%v err=%v", len(page.Messages), page.Before, page.HasMore, err)
	}
	for _, tc := range []struct {
		query  string
		status int
		array  bool
	}{
		{"?limit=1", 200, true},
		{"?paged=true&limit=1&maxBytes=100", 200, false},
		{"?paged=true&before=missing", 409, false},
		{"?paged=true&limit=0", 400, false},
		{"?paged=true&maxBytes=-1", 400, false},
	} {
		w := httptest.NewRecorder()
		writeHistory(w, httptest.NewRequest("GET", "/chats/chat/messages"+tc.query, nil), source, id)
		if w.Code != tc.status || (strings.HasPrefix(w.Body.String(), "[") != tc.array) {
			t.Fatalf("%s: status=%d body=%s", tc.query, w.Code, w.Body.String())
		}
	}
	empty, err := boundedHistory([]agent.Message(nil), 12, "", 100, id)
	if err != nil || empty.Messages == nil || empty.HasMore {
		t.Fatal("empty page must encode an empty array")
	}
}
