package projectsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func sparseDependency(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "node_modules", "huge-cache.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate(12 << 30); err != nil {
		t.Fatal(err)
	}
}

func TestIgnorePreviewAndTransferPruneTwelveGBDependencies(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	tgit(t, x.project, "init", "-b", "main")
	putFile(t, filepath.Join(x.project, ".gitignore"), "node_modules/\n.env\nvendor/\nlogs/*\n!logs/keep.txt\n")
	putFile(t, filepath.Join(x.project, "source.txt"), "code")
	putFile(t, filepath.Join(x.project, "vendor", "tracked.txt"), "tracked despite ignore")
	tgit(t, x.project, "add", "-f", ".gitignore", "source.txt", "vendor/tracked.txt")
	tgit(t, x.project, "commit", "-m", "source")
	putFile(t, filepath.Join(x.project, "vendor", "ignored.txt"), "skip me")
	putFile(t, filepath.Join(x.project, ".env"), "source-local-secret")
	putFile(t, filepath.Join(x.project, "logs", "keep.txt"), "keep untracked")
	putFile(t, filepath.Join(x.project, "logs", "other.txt"), "skip log")
	putFile(t, filepath.Join(x.project, "nested", ".gitignore"), "*.tmp\n")
	putFile(t, filepath.Join(x.project, "nested", "scratch.tmp"), "ignored nested")
	sparseDependency(t, x.project)
	preview, err := x.s.Preview(context.Background(), PreviewRequest{ProjectID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Bytes > 1024 || preview.FileCount != 5 {
		t.Fatalf("preview scanned dependencies or missed source: %+v", preview)
	}
	found := false
	for _, p := range preview.Ignored {
		if p.Path == "node_modules" && p.Kind == "directory" && !p.Included {
			found = true
		}
	}
	if !found {
		t.Fatalf("ignored folder missing: %+v", preview)
	}
	if _, err = x.reg.Project("x"); err != nil {
		t.Fatal(err)
	}
	if operations, err := x.s.List(); err != nil || len(operations) != 0 {
		t.Fatal("preview must not start an export", err)
	}
	// A fresh destination need not have received .gitignore yet. Its existing
	// dependencies and private files must still be skipped before any file read.
	sparseDependency(t, y.project)
	putFile(t, filepath.Join(y.project, ".env"), "destination-local-secret")
	export := mustRun(t, x.s, "export", Request{ID: "ignore-export", ProjectID: "x"})
	c, err := x.s.Checkpoint(export.ResultCheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Bytes > 1<<20 {
		t.Fatalf("export included dependency content: %d", c.Bytes)
	}
	relay(t, x.s, y.s, c.ID)
	imp := mustRun(t, y.s, "import", Request{ID: "ignore-import", CheckpointID: c.ID, Path: y.project})
	for _, name := range []string{"source.txt", "vendor/tracked.txt", "logs/keep.txt", "nested/.gitignore"} {
		if _, err := os.Stat(filepath.Join(y.project, name)); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, name := range []string{"vendor/ignored.txt", "logs/other.txt", "nested/scratch.tmp"} {
		if _, err := os.Stat(filepath.Join(y.project, name)); !os.IsNotExist(err) {
			t.Fatal("copied ignored path", name, err)
		}
	}
	if readFile(t, filepath.Join(y.project, ".env")) != "destination-local-secret" {
		t.Fatal("changed destination private file")
	}
	putFile(t, filepath.Join(y.project, "source.txt"), "remote work")
	back := mustRun(t, y.s, "export", Request{ID: "ignore-back-export", ProjectID: imp.ResultProjectID})
	relay(t, y.s, x.s, back.ResultCheckpointID)
	mustRun(t, x.s, "import", Request{ID: "ignore-back-import", CheckpointID: back.ResultCheckpointID})
	if readFile(t, filepath.Join(x.project, "source.txt")) != "remote work" || readFile(t, filepath.Join(x.project, ".env")) != "source-local-secret" {
		t.Fatal("return sync lost code or private data")
	}
	for _, root := range []string{x.project, y.project} {
		info, err := os.Stat(filepath.Join(root, "node_modules", "huge-cache.bin"))
		if err != nil || info.Size() != 12<<30 {
			t.Fatal("excluded dependency changed", err)
		}
	}
}

func TestExplicitIgnoredSelectionsKeepSiblingsLocalAndSurviveRetry(t *testing.T) {
	x := testService(t)
	addProject(t, x, "x")
	// Rules also work without creating a .git directory in a plain folder.
	putFile(t, filepath.Join(x.project, ".gitignore"), "cache/\n.env\n")
	putFile(t, filepath.Join(x.project, "cache", "chosen", "keep"), "selected")
	putFile(t, filepath.Join(x.project, "cache", "other", "skip"), "private")
	putFile(t, filepath.Join(x.project, ".env"), "explicitly selected config")
	preview, err := x.s.Preview(context.Background(), PreviewRequest{ProjectID: "x", IncludeIgnored: []string{".env", "cache/chosen"}})
	if err != nil || preview.FileCount != 3 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	o := mustRun(t, x.s, "export", Request{ID: "selected", ProjectID: "x", IncludeIgnored: []string{"cache/chosen/", ".env"}})
	m, err := x.s.manifest(o.ResultCheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"files/.env", "files/cache/chosen/keep"} {
		if _, ok := m.Entries[path]; !ok {
			t.Fatal("missing selection", path)
		}
	}
	if _, ok := m.Entries["files/cache/other/skip"]; ok {
		t.Fatal("copied unselected sibling")
	}
	if _, err := x.s.Start("export", Request{ID: "selected", ProjectID: "x"}); !errors.Is(err, registry.ErrConflict) {
		t.Fatal("changed selection reused operation", err)
	}
	if _, err := x.s.Start("export", Request{ID: "selected", ProjectID: "x", IncludeIgnored: []string{".env", "cache/chosen"}}); err != nil {
		t.Fatal("equivalent selection could not resume", err)
	}
	if _, err := os.Stat(filepath.Join(x.project, ".git")); !os.IsNotExist(err) {
		t.Fatal("preview initialized the source repository")
	}
	for _, path := range []string{"../outside", "/absolute", ".git", "folder/.git/config"} {
		if _, err := x.s.Preview(context.Background(), PreviewRequest{ProjectID: "x", IncludeIgnored: []string{path}}); err == nil {
			t.Fatal("unsafe inclusion accepted", path)
		}
	}
}

func TestChangingSelectionFiltersOldHistoryWithoutLosingTheMergeBase(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, ".gitignore"), "cache/\n")
	putFile(t, filepath.Join(x.project, "source.txt"), "first\nsecond\nthird\nfourth\nfifth\nsixth\nseventh\n")
	secret := "unique-previously-included-cache-data"
	putFile(t, filepath.Join(x.project, "cache", "local.bin"), secret)
	old := mustRun(t, x.s, "export", Request{ID: "old-full", ProjectID: "x", IncludeIgnored: []string{"cache"}})
	// Simulate a pre-filtering v1 checkpoint, retained on this machine.
	m, err := x.s.manifest(old.ResultCheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	m.Version = 1
	c, err := x.s.seal(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if err = x.reg.SyncPut("head", m.ProjectID, c.ID); err != nil {
		t.Fatal(err)
	}
	current := mustRun(t, x.s, "export", Request{ID: "filtered", ProjectID: "x"})
	relay(t, x.s, y.s, current.ResultCheckpointID)
	if _, err := y.s.GetObject(digest([]byte(secret))); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("excluded content leaked through old checkpoints", err)
	}
	imp := mustRun(t, y.s, "import", Request{ID: "filtered-import", CheckpointID: current.ResultCheckpointID, Path: y.project})
	putFile(t, filepath.Join(x.project, "source.txt"), "X first\nsecond\nthird\nfourth\nfifth\nsixth\nseventh\n")
	putFile(t, filepath.Join(y.project, "source.txt"), "first\nsecond\nthird\nfourth\nfifth\nsixth\nY seventh\n")
	putFile(t, filepath.Join(y.project, "cache", "local.bin"), "different destination cache")
	back := mustRun(t, y.s, "export", Request{ID: "filtered-back", ProjectID: imp.ResultProjectID})
	relay(t, x.s, y.s, current.ResultCheckpointID) // duplicate relay remains harmless
	relay(t, y.s, x.s, back.ResultCheckpointID)
	mustRun(t, x.s, "import", Request{ID: "filtered-return", CheckpointID: back.ResultCheckpointID})
	result := readFile(t, filepath.Join(x.project, "source.txt"))
	if !strings.Contains(result, "X first") || !strings.Contains(result, "Y seventh") {
		t.Fatal("lost independent work", result)
	}
	if readFile(t, filepath.Join(x.project, "cache", "local.bin")) != secret {
		t.Fatal("overwrote original excluded cache")
	}
}

func TestNewIgnoreRuleNeverDeletesAnExistingReplicaFile(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, ".gitignore"), "")
	putFile(t, filepath.Join(x.project, "local.txt"), "original")
	o := mustRun(t, x.s, "export", Request{ID: "before-ignore", ProjectID: "x"})
	relay(t, x.s, y.s, o.ResultCheckpointID)
	imp := mustRun(t, y.s, "import", Request{ID: "before-import", CheckpointID: o.ResultCheckpointID, Path: y.project})
	putFile(t, filepath.Join(y.project, "local.txt"), "independent destination")
	putFile(t, filepath.Join(x.project, ".gitignore"), "local.txt\n")
	if err := os.Remove(filepath.Join(x.project, "local.txt")); err != nil {
		t.Fatal(err)
	}
	o = mustRun(t, x.s, "export", Request{ID: "after-ignore", ProjectID: "x"})
	relay(t, x.s, y.s, o.ResultCheckpointID)
	mustRun(t, y.s, "import", Request{ID: "after-import", CheckpointID: o.ResultCheckpointID})
	if readFile(t, filepath.Join(y.project, "local.txt")) != "independent destination" {
		t.Fatal("omitted path became deletion")
	}
	o = mustRun(t, y.s, "export", Request{ID: "return-ignore", ProjectID: imp.ResultProjectID})
	relay(t, y.s, x.s, o.ResultCheckpointID)
	mustRun(t, x.s, "import", Request{ID: "return-import", CheckpointID: o.ResultCheckpointID})
	if _, err := os.Stat(filepath.Join(x.project, "local.txt")); !os.IsNotExist(err) {
		t.Fatal("excluded path was restored without selection")
	}
}

func TestChangedReviewScopeStopsBeforeReadingNewlyIncludedDependencies(t *testing.T) {
	f := testService(t)
	addProject(t, f, "p")
	putFile(t, filepath.Join(f.project, ".gitignore"), "node_modules/\n")
	sparseDependency(t, f.project)
	preview, err := f.s.Preview(context.Background(), PreviewRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(f.project, ".gitignore"), "")
	o := run(t, f.s, "export", Request{ID: "changed-scope", ProjectID: "p", SelectionToken: preview.SelectionToken})
	if o.Status != "failed" || !strings.Contains(o.Error, "Review files again") || o.ResultCheckpointID != "" {
		t.Fatalf("unreviewed dependency scope exported: %+v", o)
	}
	putFile(t, filepath.Join(f.project, ".gitignore"), "node_modules/\n")
	preview, err = f.s.Preview(context.Background(), PreviewRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, f.s, "export", Request{ID: "reviewed-scope", ProjectID: "p", SelectionToken: preview.SelectionToken})
}

func TestReincludingPrivateFileConflictsInsteadOfOverwritingLocalChanges(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, ".gitignore"), ".env\n")
	putFile(t, filepath.Join(x.project, ".env"), "local X")
	putFile(t, filepath.Join(y.project, ".env"), "local Y")
	o := mustRun(t, x.s, "export", Request{ID: "omit-private", ProjectID: "x"})
	relay(t, x.s, y.s, o.ResultCheckpointID)
	mustRun(t, y.s, "import", Request{ID: "omit-private-import", CheckpointID: o.ResultCheckpointID, Path: y.project})
	o = mustRun(t, x.s, "export", Request{ID: "include-private", ProjectID: "x", IncludeIgnored: []string{".env"}})
	relay(t, x.s, y.s, o.ResultCheckpointID)
	result := run(t, y.s, "import", Request{ID: "include-private-import", CheckpointID: o.ResultCheckpointID})
	if result.Status != "conflicts" {
		t.Fatalf("re-included independent config must conflict: %+v", result)
	}
	if readFile(t, filepath.Join(x.project, ".env")) != "local X" || readFile(t, filepath.Join(y.project, ".env")) != "local Y" {
		t.Fatal("overwrote private data")
	}
}

func TestPreviewPagesIgnoredPathsWithoutExporting(t *testing.T) {
	f := testService(t)
	addProject(t, f, "p")
	putFile(t, filepath.Join(f.project, ".gitignore"), "*.cache\n")
	for i := 0; i < 205; i++ {
		putFile(t, filepath.Join(f.project, fmt.Sprintf("%03d.cache", i)), "ignored")
	}
	first, err := f.s.Preview(context.Background(), PreviewRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.Preview(context.Background(), PreviewRequest{ProjectID: "p", Cursor: first.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Ignored) != 200 || len(second.Ignored) != 5 || first.Next != 200 || second.Next != 0 || first.SelectionToken != second.SelectionToken {
		t.Fatal("inconsistent preview pagination")
	}
	if operations, err := f.s.List(); err != nil || len(operations) != 0 {
		t.Fatal("preview started a transfer", err)
	}
}

func TestExclusionsCannotAliasInstalledFiles(t *testing.T) {
	f := testService(t)
	addProject(t, f, "p")
	putFile(t, filepath.Join(f.project, "source.txt"), "work")
	o := mustRun(t, f.s, "export", Request{ID: "case-check", ProjectID: "p"})
	m, err := f.s.manifest(o.ResultCheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	m.Excluded = []string{"SOURCE.txt"}
	if err = f.s.validateManifest(m); err == nil {
		t.Fatal("case-folded exclusion could be overwritten on macOS")
	}
}

func TestUnreadableIgnoreRulesFailClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode-000 files")
	}
	f := testService(t)
	addProject(t, f, "p")
	path := filepath.Join(f.project, ".gitignore")
	putFile(t, path, "node_modules/\n")
	putFile(t, filepath.Join(f.project, "node_modules", "private.bin"), "excluded")
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0644)
	if _, err := f.s.Preview(context.Background(), PreviewRequest{ProjectID: "p"}); err == nil {
		t.Fatal("unreadable ignores were silently bypassed")
	}
	o := run(t, f.s, "export", Request{ID: "unreadable-rules", ProjectID: "p"})
	if o.Status != "failed" || o.ResultCheckpointID != "" {
		t.Fatalf("unreadable ignore rules exported content: %+v", o)
	}
}
