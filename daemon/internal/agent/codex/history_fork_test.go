package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func writeForkRollout(t *testing.T, sid, cwd string, base *rolloutHistoryBase, content string) (string, []byte) {
	t.Helper()
	header, err := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{
		"id": sid, "cwd": cwd, "history_base": base,
	}})
	if err != nil {
		t.Fatal(err)
	}
	data := append(append(header, '\n'), content...)
	path := filepath.Join(configBase(), "sessions", "rollout-test-"+sid+".jsonl")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func forkTurn(id, text string) string {
	return fmt.Sprintf("{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":%q}}\n"+
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":%q}}\n"+
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":%q}}\n"+
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":%q}}\n", id, text, text+" reply", id)
}

func TestLinkedForkHistoryKeepsBoundariesThroughNestedForksAndCacheRefresh(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	cwd := t.TempDir()
	future := forkTurn("discarded-turn", "Discarded")
	rootPath, root := writeForkRollout(t, "original", cwd, nil, forkTurn("first-turn", "Keep")+future)
	end := int64(len(root) - len(future))
	_, branch := writeForkRollout(t, "branch", cwd, &rolloutHistoryBase{ThreadID: "original", EndByteOffset: end}, forkTurn("branch-turn", "Edited"))
	writeForkRollout(t, "nested", cwd, &rolloutHistoryBase{ThreadID: "branch", EndByteOffset: int64(len(branch))}, forkTurn("nested-turn", "Nested"))
	query := agent.HistoryQuery{ChatID: "chat", SessionID: "nested", CWD: cwd}
	read := func(first string) {
		t.Helper()
		rows, err := (adapter{}).History(query)
		if err != nil || len(rows) != 6 || rows[0].Text != first || rows[2].Text != "Edited" || rows[4].Text != "Nested" {
			t.Fatalf("linked prefix: %+v %v", rows, err)
		}
		point, err := agent.PrepareFork(adapter{}, query, rows[2].ID)
		if err != nil || point.ResumeAt != "first-turn" || point.Fresh {
			t.Fatalf("inherited turn boundary: %+v %v", point, err)
		}
	}
	read("Keep")
	// The original can keep working; the fork must remain on its immutable prefix.
	if err := os.WriteFile(rootPath, append(root, forkTurn("later", "More work")...), 0600); err != nil {
		t.Fatal(err)
	}
	read("Keep")
	// Cache stamps include every ancestor, not just the unchanged leaf file.
	rewritten := bytes.ReplaceAll(root, []byte("Keep"), []byte("Read"))
	if err := os.WriteFile(rootPath, rewritten, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(time.Second)
	if err := os.Chtimes(rootPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	read("Read")
}

func TestDeletingAnAncestorKeepsSurvivingNativeBranchesReadable(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	cwd := t.TempDir()
	rootPath, root := writeForkRollout(t, "original", cwd, nil, forkTurn("first", "Keep"))
	writeForkRollout(t, "branch", cwd, &rolloutHistoryBase{ThreadID: "original", EndByteOffset: int64(len(root))}, forkTurn("next", "Edited"))
	if purged, err := (adapter{}).DeleteHistory(agent.HistoryQuery{SessionID: "original"}); err != nil || purged {
		t.Fatalf("a surviving branch still needs its ancestor: %v %v", purged, err)
	}
	if after, err := os.ReadFile(rootPath); err != nil || !bytes.Equal(root, after) {
		t.Fatal("the retained ancestor changed")
	}
	rows, err := (adapter{}).History(agent.HistoryQuery{SessionID: "branch", ChatID: "chat"})
	if err != nil || len(rows) != 4 {
		t.Fatalf("branch became unreadable: %+v %v", rows, err)
	}
	for _, id := range []string{"branch", "original"} {
		if purged, err := (adapter{}).DeleteHistory(agent.HistoryQuery{SessionID: id}); err != nil || !purged {
			t.Fatalf("independent transcript %s was not deleted: %v %v", id, purged, err)
		}
	}
}

func TestBrokenForkReferencesNeverShowAnUnboundedParentOrLoop(t *testing.T) {
	for _, kind := range []string{"missing", "cycle", "truncated", "mid-record", "unknown-format"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("CODEX_HOME", t.TempDir())
			cwd := t.TempDir()
			_, root := writeForkRollout(t, "original", cwd, nil, forkTurn("first", "Keep"))
			base := &rolloutHistoryBase{ThreadID: "original", EndByteOffset: int64(len(root))}
			switch kind {
			case "missing":
				base.ThreadID = "missing"
			case "cycle":
				base.ThreadID, base.EndByteOffset = "branch", 1
			case "truncated":
				base.EndByteOffset++
			case "mid-record":
				base.EndByteOffset--
			case "unknown-format":
				base.EndByteOffset = 0
			}
			writeForkRollout(t, "branch", cwd, base, forkTurn("next", "Edited"))
			if rows, err := (adapter{}).History(agent.HistoryQuery{SessionID: "branch", ChatID: "chat"}); err == nil || rows != nil {
				t.Fatalf("broken fork returned partial/future history: %+v %v", rows, err)
			}
		})
	}
}

func TestForkBoundaryCannotDiscardWorkBeforeAMidTurnSteeringMessage(t *testing.T) {
	data := strings.Replace(forkTurn("first", "Original"), `{"type":"event_msg","payload":{"type":"task_complete"`,
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"Steer\"}}\n"+`{"type":"event_msg","payload":{"type":"task_complete"`, 1)
	rows, err := parseRollout(strings.NewReader(data+forkTurn("second", "Next")), "chat")
	if err != nil || len(rows) != 5 {
		t.Fatalf("steered transcript: %+v %v", rows, err)
	}
	if !rows[0].CanFork || !rows[0].ForkPoint.Fresh || rows[2].CanFork || rows[2].ForkPoint != nil || !rows[3].CanFork || rows[3].ForkPoint.ResumeAt != "first" {
		t.Fatalf("unsafe steering boundary: %+v", rows)
	}
}

func TestWorkspaceSwitchRejectsLinkedForkArchivesBeforeChangingAnyFiles(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	cwd := t.TempDir()
	rootPath, root := writeForkRollout(t, "original", cwd, nil, forkTurn("first", "Keep"))
	if _, err := (adapter{}).ExportSession(context.Background(), cwd, "original"); err != nil {
		t.Fatalf("ordinary sessions remain portable: %v", err)
	}
	branchPath, branch := writeForkRollout(t, "branch", cwd, &rolloutHistoryBase{ThreadID: "original", EndByteOffset: int64(len(root))}, forkTurn("next", "Edited"))
	for _, id := range []string{"original", "branch"} {
		archive, err := (adapter{}).ExportSession(context.Background(), cwd, id)
		if err == nil || !strings.Contains(err.Error(), "linked fork history") || len(archive.Files) != 0 {
			t.Fatalf("incomplete archive accepted for %s: %+v %v", id, archive, err)
		}
	}
	for path, want := range map[string][]byte{rootPath: root, branchPath: branch} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("rejected archive changed %s", path)
		}
	}
}
