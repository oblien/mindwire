package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    *SubagentMeta
	}{
		{
			name:    "full",
			content: "---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep Bash\nmodel: sonnet\n---\nbody",
			want:    &SubagentMeta{Name: "reviewer", Description: "Reviews code", Tools: []string{"Read", "Grep", "Bash"}, Model: "sonnet"},
		},
		{
			name:    "quoted values and partial keys",
			content: "---\nname: \"quoted\"\nunknown: x\n---\n",
			want:    &SubagentMeta{Name: "quoted"},
		},
		{name: "no frontmatter", content: "just a body\nwith lines", want: nil},
		{name: "unterminated fence", content: "---\nname: x\nno closing fence\n", want: nil},
		{name: "empty frontmatter", content: "---\n---\nbody", want: nil},
		{name: "fence but only unknown keys", content: "---\nfoo: bar\n---\n", want: nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseFrontmatter(c.content)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("parseFrontmatter = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestSubagentLayoutRecursion verifies the layout scans recursively, derives the name from frontmatter
// (falling back to the filename stem), omits content in List but populates it on Read, and reports a
// missing definition as fs.ErrNotExist.
func TestSubagentLayoutRecursion(t *testing.T) {
	user := t.TempDir()
	l := SubagentLayout{UserDir: user} // user-scope only; project scope needs a dir (covered elsewhere)

	// A nested definition whose frontmatter name differs from its filename.
	nested := filepath.Join(user, "team")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "file-stem.md"),
		[]byte("---\nname: named-agent\ndescription: d\n---\nprompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A flat definition with no frontmatter → name is the filename stem, Meta nil.
	if err := os.WriteFile(filepath.Join(user, "plain.md"), []byte("no frontmatter here"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := l.ListSubagents("")
	if err != nil {
		t.Fatalf("ListSubagents: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListSubagents = %d entries, want 2: %+v", len(list), list)
	}
	byName := map[string]Subagent{}
	for _, s := range list {
		if s.Content != "" {
			t.Errorf("list entry %q leaked Content", s.Name)
		}
		byName[s.Name] = s
	}
	if _, ok := byName["named-agent"]; !ok {
		t.Fatalf("recursion/name-from-frontmatter failed: %+v", byName)
	}
	if s, ok := byName["plain"]; !ok || s.Meta != nil {
		t.Fatalf("filename-stem/no-meta failed: %+v", byName)
	}

	// Read resolves by frontmatter name across the recursion and populates raw Content.
	got, err := l.ReadSubagent(MemoryUser, "", "named-agent")
	if err != nil || got.Content != "---\nname: named-agent\ndescription: d\n---\nprompt" || got.Meta == nil {
		t.Fatalf("ReadSubagent(named-agent) = %+v err=%v", got, err)
	}

	if _, err := l.ReadSubagent(MemoryUser, "", "does-not-exist"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadSubagent(missing) err = %v, want fs.ErrNotExist", err)
	}
}

// TestSubagentWriteAndTraversal verifies a write lands flat at <scopeDir>/<name>.md and that a name
// with a path separator is rejected before any file IO.
func TestSubagentWriteAndTraversal(t *testing.T) {
	user := t.TempDir()
	l := SubagentLayout{UserDir: user}

	sub, err := l.WriteSubagent(MemoryUser, "", "helper", "---\nname: helper\n---\nbody")
	if err != nil || sub.Path != filepath.Join(user, "helper.md") {
		t.Fatalf("WriteSubagent = %+v err=%v", sub, err)
	}
	if sub.Meta == nil || sub.Meta.Name != "helper" {
		t.Fatalf("WriteSubagent Meta = %+v, want name=helper", sub.Meta)
	}

	if _, err := l.WriteSubagent(MemoryUser, "", "../escape", "x"); err == nil {
		t.Fatal("WriteSubagent with traversal name should error")
	}
	// Nothing escaped the scope dir.
	if _, err := os.Stat(filepath.Join(filepath.Dir(user), "escape.md")); err == nil {
		t.Fatal("traversal write escaped the scope dir")
	}
}

// TestSubagentDelete covers the delete lifecycle: absent is idempotent, a present definition is
// removed by its IDENTITY (frontmatter name, not filename), and a traversal name is rejected.
func TestSubagentDelete(t *testing.T) {
	user := t.TempDir()
	l := SubagentLayout{UserDir: user}

	// Absent → idempotent success (no error).
	if err := l.DeleteSubagent(MemoryUser, "", "ghost"); err != nil {
		t.Fatalf("DeleteSubagent(absent): %v", err)
	}

	// Write file "planner.md" whose frontmatter names it "architect", then delete by that IDENTITY —
	// proving the delete walks and matches the same way ReadSubagent does, not just by filename.
	if _, err := l.WriteSubagent(MemoryUser, "", "planner", "---\nname: architect\n---\nbody"); err != nil {
		t.Fatalf("WriteSubagent: %v", err)
	}
	if err := l.DeleteSubagent(MemoryUser, "", "architect"); err != nil {
		t.Fatalf("DeleteSubagent(by identity): %v", err)
	}
	if _, err := os.Stat(filepath.Join(user, "planner.md")); !os.IsNotExist(err) {
		t.Fatalf("subagent file still present after delete: %v", err)
	}
	if _, err := l.ReadSubagent(MemoryUser, "", "architect"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadSubagent after delete err = %v, want ErrNotExist", err)
	}

	// A traversal name is rejected before any IO.
	if err := l.DeleteSubagent(MemoryUser, "", "../escape"); err == nil {
		t.Error("DeleteSubagent(traversal) should be rejected")
	}
}
