package agent

import (
	"strings"
	"testing"
)

func TestBuildUnifiedDiffEdit(t *testing.T) {
	d := BuildUnifiedDiff("f.txt", "a\nb\nc\n", "a\nB\nc\n")
	if d == "" {
		t.Fatal("expected a diff for a changed line")
	}
	for _, want := range []string{"--- a/f.txt", "+++ b/f.txt", "@@ -", "-b", "+B", " a", " c"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff missing %q:\n%s", want, d)
		}
	}
}

func TestBuildUnifiedDiffCreate(t *testing.T) {
	d := BuildUnifiedDiff("new.txt", "", "x\ny\n")
	// A create is all-additions with a 0-count old side (git's new-file header).
	if !strings.Contains(d, "@@ -0,0 +1,2 @@") {
		t.Errorf("create header wrong:\n%s", d)
	}
	if !strings.Contains(d, "+x") || !strings.Contains(d, "+y") {
		t.Errorf("create diff should add both lines:\n%s", d)
	}
	if strings.Contains(d, "\n-") {
		t.Errorf("create diff should have no removals:\n%s", d)
	}
}

func TestBuildUnifiedDiffNoChange(t *testing.T) {
	if d := BuildUnifiedDiff("f", "same\n", "same\n"); d != "" {
		t.Errorf("identical text must produce no diff, got:\n%s", d)
	}
}

func TestBuildUnifiedDiffTooLargeFallsBack(t *testing.T) {
	huge := strings.Repeat("line\n", maxDiffLines+1)
	if d := BuildUnifiedDiff("big", "", huge); d != "" {
		t.Errorf("oversized input must fall back to empty diff, got %d bytes", len(d))
	}
}

func TestMapChangeOp(t *testing.T) {
	cases := map[string]string{
		"add": "create", "create": "create", "new": "create",
		"delete": "delete", "removed": "delete",
		"modify": "edit", "": "edit", "weird": "edit",
	}
	for in, want := range cases {
		if got := MapChangeOp(in); got != want {
			t.Errorf("MapChangeOp(%q) = %q, want %q", in, got, want)
		}
	}
}
