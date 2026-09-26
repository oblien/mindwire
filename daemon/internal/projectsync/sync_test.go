package projectsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
)

type fixture struct {
	s             *Service
	reg           *registry.Store
	store         *session.Store
	root, project string
}

func testService(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	reg, err := registry.Open(filepath.Join(root, "state", "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := session.Open(filepath.Join(root, "state", "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(reg, st, nil, &sync.Mutex{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{s, reg, st, root, filepath.Join(root, "project")}
	t.Cleanup(func() { f.s.Close(); f.reg.Close() })
	return f
}
func putFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func addProject(t *testing.T, f *fixture, id string) {
	t.Helper()
	if err := os.MkdirAll(f.project, 0755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(registry.Project{Record: registry.Record{ID: id}, Name: "Sample", Path: f.project})
	if err := f.reg.Put("projects", id, b, nil); err != nil {
		t.Fatal(err)
	}
}
func tgit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.name=Sync Fixture", "-c", "user.email=sync@example.invalid", "-C", cwd}, args...)...)
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}
func run(t *testing.T, s *Service, kind string, r Request) Operation {
	t.Helper()
	o, err := s.Start(kind, r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	o, err = s.Wait(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func mustRun(t *testing.T, s *Service, kind string, r Request) Operation {
	t.Helper()
	o := run(t, s, kind, r)
	if o.Status != "succeeded" {
		t.Fatalf("%s: %+v", kind, o)
	}
	return o
}
func relay(t *testing.T, from, to *Service, id string) {
	t.Helper()
	if _, err := to.Checkpoint(id); err == nil {
		return
	}
	c, err := from.Checkpoint(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range c.Parents {
		relay(t, from, to, parent)
	}
	for cursor := 0; ; {
		page, err := from.Objects(id, cursor)
		if err != nil {
			t.Fatal(err)
		}
		missing, err := to.Missing(page.Objects)
		if err != nil {
			t.Fatal(err)
		}
		for _, oid := range missing {
			b, err := from.GetObject(oid)
			if err != nil {
				t.Fatal(err)
			}
			if err = to.PutObject(oid, b); err != nil {
				t.Fatal(err)
			}
		}
		if page.Next == 0 {
			break
		}
		cursor = page.Next
	}
	if err = to.AcceptCheckpoint(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}
func treeHash(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, p)
		fmt.Fprintf(h, "%s:%s\x00", rel, info.Mode())
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				t.Fatal(err)
			}
			h.Write([]byte(link))
		} else if !info.IsDir() {
			h.Write([]byte(readFile(t, p)))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestRoundTripPreservesSourceStagingCommitsAndIndependentLocalWork(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	tgit(t, x.project, "init", "-b", "main")
	putFile(t, filepath.Join(x.project, "main.txt"), "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n")
	putFile(t, filepath.Join(x.project, "remove.txt"), "old")
	tgit(t, x.project, "add", ".")
	tgit(t, x.project, "commit", "-m", "base")
	tgit(t, x.project, "config", "remote.private.url", "https://fixture-token@example.invalid/private.git")
	putFile(t, filepath.Join(x.project, "staged.txt"), "staged\n")
	tgit(t, x.project, "add", "staged.txt")
	putFile(t, filepath.Join(x.project, "staged.txt"), "staged then unstaged\n")
	putFile(t, filepath.Join(x.project, "binary.bin"), "\x00\x01\xff\x02")
	putFile(t, filepath.Join(x.project, "executable"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(x.project, "executable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("main.txt", filepath.Join(x.project, "link")); err != nil {
		t.Fatal(err)
	}
	before := treeHash(t, x.project)
	export := mustRun(t, x.s, "export", Request{ID: "export-x", ProjectID: "x"})
	if got := treeHash(t, x.project); got != before {
		t.Fatal("export changed the source tree or Git data")
	}
	relay(t, x.s, y.s, export.ResultCheckpointID)
	imported := mustRun(t, y.s, "import", Request{ID: "import-y", CheckpointID: export.ResultCheckpointID, Path: y.project})
	for _, name := range []string{"main.txt", "staged.txt", "binary.bin", "executable"} {
		if readFile(t, filepath.Join(x.project, name)) != readFile(t, filepath.Join(y.project, name)) {
			t.Fatalf("lost %s", name)
		}
	}
	if tgit(t, x.project, "diff", "--cached", "--name-only") != tgit(t, y.project, "diff", "--cached", "--name-only") {
		t.Fatal("staging did not survive")
	}
	if tgit(t, x.project, "show", ":staged.txt") != tgit(t, y.project, "show", ":staged.txt") {
		t.Fatal("index was replaced with working content")
	}
	if strings.Contains(readFile(t, filepath.Join(y.project, ".git", "config")), "fixture-token") {
		t.Fatal("Git credentials crossed workspaces")
	}
	info, _ := os.Stat(filepath.Join(y.project, "executable"))
	if info.Mode().Perm() != 0755 {
		t.Fatal("executable mode lost")
	}
	link, _ := os.Readlink(filepath.Join(y.project, "link"))
	if link != "main.txt" {
		t.Fatal("symlink lost")
	}
	// Y commits new work; X independently edits a distant line, creates an
	// untracked file and a local branch. All must survive the return journey.
	putFile(t, filepath.Join(y.project, "main.txt"), "one from Y\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n")
	putFile(t, filepath.Join(y.project, "new-y.txt"), "Y work")
	if err := os.Remove(filepath.Join(y.project, "remove.txt")); err != nil {
		t.Fatal(err)
	}
	tgit(t, y.project, "add", ".")
	tgit(t, y.project, "commit", "-m", "work in Y")
	putFile(t, filepath.Join(x.project, "main.txt"), "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten from X\n")
	putFile(t, filepath.Join(x.project, "only-x.txt"), "independent X")
	tgit(t, x.project, "branch", "local-x")
	yBefore := treeHash(t, y.project)
	back := mustRun(t, y.s, "export", Request{ID: "export-y", ProjectID: imported.ResultProjectID})
	if yBefore != treeHash(t, y.project) {
		t.Fatal("return export mutated Y")
	}
	relay(t, y.s, x.s, back.ResultCheckpointID)
	returned := mustRun(t, x.s, "import", Request{ID: "return-x", CheckpointID: back.ResultCheckpointID})
	text := readFile(t, filepath.Join(x.project, "main.txt"))
	if !strings.Contains(text, "one from Y") || !strings.Contains(text, "ten from X") {
		t.Fatal("disjoint edits were lost")
	}
	if readFile(t, filepath.Join(x.project, "only-x.txt")) != "independent X" || readFile(t, filepath.Join(x.project, "new-y.txt")) != "Y work" {
		t.Fatal("independent files lost")
	}
	if _, err := os.Stat(filepath.Join(x.project, "remove.txt")); !os.IsNotExist(err) {
		t.Fatal("shared-base deletion not applied")
	}
	tgit(t, x.project, "rev-parse", "refs/heads/local-x")
	if !strings.Contains(readFile(t, filepath.Join(x.project, ".git", "config")), "fixture-token") {
		t.Fatal("local Git config was replaced")
	}
	if tgit(t, x.project, "log", "-1", "--format=%s") != "work in Y" {
		t.Fatal("unpushed commit not imported")
	}
	final := treeHash(t, x.project)
	repeat := mustRun(t, x.s, "import", Request{ID: "return-x", CheckpointID: back.ResultCheckpointID})
	if repeat.ResultProjectID != returned.ResultProjectID || treeHash(t, x.project) != final {
		t.Fatal("lost-ack retry reapplied changes")
	}
	// Switch again with no new edits: checkpoint reuse must still carry X's
	// branch objects and all merged content, not just Y's original bundle.
	again := mustRun(t, x.s, "export", Request{ID: "export-merged", ProjectID: "x"})
	relay(t, x.s, y.s, again.ResultCheckpointID)
	mustRun(t, y.s, "import", Request{ID: "switch-again", CheckpointID: again.ResultCheckpointID})
	if readFile(t, filepath.Join(y.project, "only-x.txt")) != "independent X" {
		t.Fatal("second switch lost X work")
	}
	tgit(t, y.project, "rev-parse", "refs/heads/local-x")
}

func TestConflictingFilesLeaveBothTreesByteForByteUntouched(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, "same.txt"), "base\n")
	c := mustRun(t, x.s, "export", Request{ID: "e", ProjectID: "x"})
	relay(t, x.s, y.s, c.ResultCheckpointID)
	dest := mustRun(t, y.s, "import", Request{ID: "i", CheckpointID: c.ResultCheckpointID, Path: y.project})
	putFile(t, filepath.Join(x.project, "same.txt"), "X\n")
	putFile(t, filepath.Join(y.project, "same.txt"), "Y\n")
	putFile(t, filepath.Join(y.project, "incoming.txt"), "retain in checkpoint")
	c = mustRun(t, y.s, "export", Request{ID: "e2", ProjectID: dest.ResultProjectID})
	relay(t, y.s, x.s, c.ResultCheckpointID)
	xh, yh := treeHash(t, x.project), treeHash(t, y.project)
	o := run(t, x.s, "import", Request{ID: "conflict", CheckpointID: c.ResultCheckpointID})
	if o.Status != "conflicts" || len(o.Conflicts) == 0 || o.RecoveryCheckpointID == "" {
		t.Fatalf("missing conflict/checkpoint: %+v", o)
	}
	if xh != treeHash(t, x.project) || yh != treeHash(t, y.project) {
		t.Fatal("conflict changed a source or destination file")
	}
	if err := x.reg.CheckProjectPath(x.project); err != nil {
		t.Fatal("conflict kept an unnecessary run lock", err)
	}
}

type nativeFixture struct{ root string }

func (p nativeFixture) ExportSession(ctx context.Context, cwd, sid string) (agent.NativeArchive, error) {
	files, err := agent.ArchiveTree(ctx, filepath.Join(p.root, "session", sid), "session/"+sid)
	if err != nil {
		return agent.NativeArchive{}, err
	}
	memory, err := agent.ArchiveTree(ctx, filepath.Join(p.root, "memory"), "memory")
	files = append(files, memory...)
	return agent.NativeArchive{Version: 1, Harness: "fixture", SessionID: sid, Files: files}, err
}
func (p nativeFixture) ImportSessionPath(cwd, sid, name string) (string, error) {
	if !agent.SafeArchiveName(name) {
		return "", invalid("bad name")
	}
	return filepath.Join(p.root, name), nil
}
func addChat(t *testing.T, f *fixture, id, sid string) {
	t.Helper()
	b, _ := json.Marshal(registry.Agent{Record: registry.Record{ID: "agent"}, Name: "Fixture", AgentType: "fixture"})
	if err := f.reg.Put("agents", "agent", b, nil); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(registry.Chat{Record: registry.Record{ID: id}, AgentID: "agent", ProjectID: "x", SessionID: sid, Title: id})
	if err := f.reg.Put("chats", id, b, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetSession("fixture", id, sid); err != nil {
		t.Fatal(err)
	}
}
func TestNativeHistoryToolsCompactionChildrenMemoryAndNewChatsRoundTrip(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	xp, yp := nativeFixture{filepath.Join(x.root, "native")}, nativeFixture{filepath.Join(y.root, "native")}
	x.s.porters["fixture"] = xp
	y.s.porters["fixture"] = yp
	addChat(t, x, "first", "session-one")
	native := fmt.Sprintf("{\"type\":\"session_meta\",\"cwd\":%q,\"future_field\":{\"preserve\":true}}\n{\"type\":\"tool\",\"call_id\":\"tool-1\",\"diff\":\"+created\"}\n{\"type\":\"compacted\",\"context\":\"memory after compaction\",\"image\":\"data:image/png;base64,AQID\"}\n", x.project)
	putFile(t, filepath.Join(xp.root, "session/session-one/main.jsonl"), native)
	putFile(t, filepath.Join(xp.root, "session/session-one/subagents/child.jsonl"), "{\"child\":\"native child context\"}\n")
	putFile(t, filepath.Join(xp.root, "memory/MEMORY.md"), "Remember the original project decision.\n")
	putFile(t, filepath.Join(yp.root, "unrelated-session"), "keep Y conversation")
	putFile(t, filepath.Join(yp.root, "auth.json"), "keep Y login")
	if err := x.store.AddMessage(session.Message{ID: "m1", ChatID: "first", Role: "assistant", Text: "first", CreatedAt: "2026-09-26T00:00:00Z", Parts: []agent.Part{{Type: "thinking", Text: "reasoning"}}}); err != nil {
		t.Fatal(err)
	}
	before := treeHash(t, xp.root)
	c := mustRun(t, x.s, "export", Request{ID: "e", ProjectID: "x"})
	if before != treeHash(t, xp.root) {
		t.Fatal("native source mutated")
	}
	relay(t, x.s, y.s, c.ResultCheckpointID)
	dest := mustRun(t, y.s, "import", Request{ID: "i", CheckpointID: c.ResultCheckpointID, Path: y.project})
	transcript := readFile(t, filepath.Join(yp.root, "session/session-one/main.jsonl"))
	if !strings.Contains(transcript, y.project) || strings.Contains(transcript, x.project) || !strings.Contains(transcript, "future_field") {
		t.Fatal("native context remap lost data", transcript)
	}
	if readFile(t, filepath.Join(yp.root, "unrelated-session")) != "keep Y conversation" || readFile(t, filepath.Join(yp.root, "auth.json")) != "keep Y login" {
		t.Fatal("workspace-local state replaced")
	}
	snap, err := y.reg.Snapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Chats) != 1 || len(y.store.Messages(snap.Chats[0].ID)) != 1 {
		t.Fatal("recorded rich history missing")
	}
	putFile(t, filepath.Join(yp.root, "session/session-one/main.jsonl"), transcript+fmt.Sprintf("{\"cwd\":%q,\"text\":\"Y continuation\"}\n", y.project))
	putFile(t, filepath.Join(yp.root, "memory/MEMORY.md"), "Remember the original project decision.\nAlso remember the work in Y.\n")
	if err = y.store.AddMessage(session.Message{ID: "m2", ChatID: snap.Chats[0].ID, Role: "assistant", Text: "Y continuation", CreatedAt: "2026-09-26T00:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	addChat(t, x, "independent", "session-two")
	putFile(t, filepath.Join(xp.root, "session/session-two/main.jsonl"), fmt.Sprintf("{\"cwd\":%q,\"text\":\"X-only chat\"}\n", x.project))
	c = mustRun(t, y.s, "export", Request{ID: "e2", ProjectID: dest.ResultProjectID})
	relay(t, y.s, x.s, c.ResultCheckpointID)
	mustRun(t, x.s, "import", Request{ID: "return", CheckpointID: c.ResultCheckpointID})
	result := readFile(t, filepath.Join(xp.root, "session/session-one/main.jsonl"))
	if strings.Count(result, "Y continuation") != 1 || strings.Count(result, "memory after compaction") != 1 || !strings.Contains(result, x.project) {
		t.Fatal("duplicated or lost native context")
	}
	if !strings.Contains(readFile(t, filepath.Join(xp.root, "memory/MEMORY.md")), "work in Y") {
		t.Fatal("project memory missing")
	}
	if !strings.Contains(readFile(t, filepath.Join(xp.root, "session/session-one/subagents/child.jsonl")), "child context") {
		t.Fatal("child session missing")
	}
	snap, err = x.reg.Snapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Chats) != 2 || len(x.store.Messages("first")) != 2 {
		t.Fatal("independent chat lost or messages duplicated")
	}
	final := treeHash(t, xp.root)
	mustRun(t, x.s, "import", Request{ID: "return-again", CheckpointID: c.ResultCheckpointID})
	if treeHash(t, xp.root) != final || len(x.store.Messages("first")) != 2 {
		t.Fatal("retry duplicated native/recorded history")
	}
}

func TestInterruptedApplyReplaysJournalAndRejectsExternalOverwrite(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, "a"), "first")
	putFile(t, filepath.Join(x.project, "b"), "second")
	c := mustRun(t, x.s, "export", Request{ID: "e", ProjectID: "x"})
	relay(t, x.s, y.s, c.ResultCheckpointID)
	y.s.afterWrite = func(i int) error {
		if i == 0 {
			return fmt.Errorf("simulated process interruption")
		}
		return nil
	}
	o := run(t, y.s, "import", Request{ID: "i", CheckpointID: c.ResultCheckpointID, Path: y.project})
	if o.Status != "recovery_required" {
		t.Fatalf("not recoverable: %+v", o)
	}
	if !errors.Is(y.reg.CheckProjectPath(y.project), registry.ErrConflict) {
		t.Fatal("partially applied project admitted a new turn")
	}
	y.s.Close()
	o.Status = "applying"
	if err := y.s.save(o); err != nil {
		t.Fatal(err)
	}
	s, err := New(y.reg, y.store, nil, &sync.Mutex{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	y.s = s
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	o, err = s.Wait(ctx, "i")
	if err != nil || o.Status != "succeeded" {
		t.Fatalf("restart recovery: %+v %v", o, err)
	}
	if readFile(t, filepath.Join(y.project, "a")) != "first" || readFile(t, filepath.Join(y.project, "b")) != "second" {
		t.Fatal("incomplete recovery")
	}
	if err = y.reg.CheckProjectPath(y.project); err != nil {
		t.Fatal("lock leaked after recovery", err)
	}
	putFile(t, filepath.Join(x.project, "a"), "new")
	putFile(t, filepath.Join(x.project, "b"), "also new")
	c = mustRun(t, x.s, "export", Request{ID: "e2", ProjectID: "x"})
	relay(t, x.s, y.s, c.ResultCheckpointID)
	y.s.afterWrite = func(i int) error {
		if i == 0 {
			return fmt.Errorf("stop")
		}
		return nil
	}
	o = run(t, y.s, "import", Request{ID: "i2", CheckpointID: c.ResultCheckpointID})
	if o.Status != "recovery_required" {
		t.Fatal(o)
	}
	y.s.Close()
	putFile(t, filepath.Join(y.project, "a"), "external edit after crash")
	s, err = New(y.reg, y.store, nil, &sync.Mutex{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	y.s = s
	if _, err = s.Retry("i2"); err != nil {
		t.Fatal(err)
	}
	o, err = s.Wait(ctx, "i2")
	if err != nil || o.Status != "recovery_required" {
		t.Fatalf("unsafe recovery: %+v %v", o, err)
	}
	if readFile(t, filepath.Join(y.project, "a")) != "external edit after crash" {
		t.Fatal("recovery overwrote an external edit")
	}
}

func TestObjectsRejectCorruptionAndCheckpointTraversal(t *testing.T) {
	f := testService(t)
	data := []byte("verified")
	id := digest(data)
	if err := f.s.PutObject(id, []byte("corrupt")); err == nil {
		t.Fatal("accepted corrupt content")
	}
	if err := f.s.PutObject(id, data); err != nil {
		t.Fatal(err)
	}
	if b, err := f.s.GetObject(id); err != nil || !bytes.Equal(b, data) {
		t.Fatal("object mismatch")
	}
	m := Manifest{Version: 1, ProjectID: strings.Repeat("a", 64), Name: "Project", Entries: map[string]Entry{"files/../../escape": {Kind: "file", Mode: 0600, Size: int64(len(data)), Chunks: []string{id}}}, Chats: map[string]Chat{}, Parents: []string{}}
	c, err := f.s.seal(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.AcceptCheckpoint(context.Background(), c); err == nil {
		t.Fatal("accepted traversal")
	}
}
