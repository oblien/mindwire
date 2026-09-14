package codex

import (
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestAggregateDiffUpdatesMissingFilesAndPreservesNativeOperations(t *testing.T) {
	st := newStreamState()
	var events []agent.Event
	emit := func(e agent.Event) { events = append(events, e) }
	native := "@@ -1 +1 @@\n-old\n+native\n"
	emitNorm(normItem{ID: "known", Kind: kindFileChange, Changes: []normChange{{Path: "known.swift", Diff: native}}}, phaseCompleted, nil, emit, st)
	emitNorm(normItem{ID: "missing", Kind: kindFileChange, Changes: []normChange{{Path: "missing.swift"}}}, phaseStarted, nil, emit, st)
	patch := func(path, body string) string {
		return "diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n@@ -1 +1 @@\n-old\n+" + body + "\n"
	}
	for _, body := range []string{"first", strings.Repeat("界", 100000)} {
		st.turnDiff = patch("known.swift", "aggregate") + patch("missing.swift", body) + patch("other.swift", body)
		st.emitTurnDiff("turn", emit)
	}
	latest := map[string]*agent.ToolEvent{}
	for _, event := range events {
		if event.Tool != nil {
			latest[event.Tool.ID] = event.Tool
		}
	}
	if len(latest) != 3 || latest["known"].Action.Files[0].Diff != native {
		t.Fatalf("native calls duplicated/replaced: %+v", latest)
	}
	for _, id := range []string{"missing", "turn-diff-turn"} {
		if len(latest[id].Action.Files[0].Diff) < 300000 {
			t.Fatalf("latest large diff missing for %s", id)
		}
	}
}

func TestSplitAggregateCreateDeleteRenameAndQuotedPaths(t *testing.T) {
	diff := "diff --git a/new.swift b/new.swift\nnew file mode 100644\n--- /dev/null\n+++ b/new.swift\n@@ -0,0 +1 @@\n+++ literal file content\n" +
		"diff --git a/old.swift b/old.swift\n--- a/old.swift\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n" +
		"diff --git a/first.swift b/next.swift\nsimilarity index 100%\nrename from first.swift\nrename to next.swift\n" +
		"diff --git \"a/my file.swift\" \"b/my file.swift\"\n--- \"a/my file.swift\"\n+++ \"b/my file.swift\"\n@@ -1 +1 @@\n-a\n+b\n"
	changes := splitTurnDiff(diff)
	if len(changes) != 4 || changes[0].Path != "new.swift" || changes[0].Kind != "add" ||
		changes[1].Path != "old.swift" || changes[1].Kind != "delete" || changes[2].Path != "next.swift" || changes[3].Path != "my file.swift" {
		t.Fatalf("bad file mapping: %+v", changes)
	}
}
