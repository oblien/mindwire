package projectsync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspaceexec"
)

func TestSyncReservationsGateCommandsFilesAndChatDeletion(t *testing.T) {
	f := testService(t)
	addProject(t, f, "x")
	addChat(t, f, "chat", "native")
	putFile(t, filepath.Join(f.project, "file.txt"), "unchanged")
	execution := workspaceexec.New(f.project)
	execution.SetMutationGuard(f.s.gate, f.reg.CheckSyncPath)
	if err := f.reg.ReserveSync("locked", f.project); err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]func() error{
		"write":  func() error { return execution.Write(filepath.Join(f.project, "file.txt"), "overwritten") },
		"delete": func() error { return execution.Delete(filepath.Join(f.project, "file.txt")) },
		"command": func() error {
			_, err := execution.Exec(context.Background(), workspaceexec.ExecRequest{Argv: []string{"sh", "-c", "printf changed > file.txt"}}, nil)
			return err
		},
		"terminal": func() error {
			_, err := execution.Terminals.Open(workspaceexec.TerminalRequest{ID: "terminal-fixture", Columns: 80, Rows: 24})
			return err
		},
		"chat":  func() error { return f.reg.Delete("chats", "chat", nil, true) },
		"agent": func() error { return f.reg.Delete("agents", "agent", nil, true) },
	} {
		if err := action(); !errors.Is(err, registry.ErrConflict) {
			t.Fatalf("%s bypassed sync reservation: %v", name, err)
		}
	}
	if readFile(t, filepath.Join(f.project, "file.txt")) != "unchanged" {
		t.Fatal("reserved files mutated")
	}
	if err := f.reg.ReleaseSync("locked"); err != nil {
		t.Fatal(err)
	}
	if err := execution.Write(filepath.Join(f.project, "file.txt"), "allowed"); err != nil {
		t.Fatal(err)
	}
}

func TestReplicaIdentityCannotBeInventedOrChangedByClientCaches(t *testing.T) {
	f := testService(t)
	if err := os.MkdirAll(f.project, 0700); err != nil {
		t.Fatal(err)
	}
	p := registry.Project{Record: registry.Record{ID: "x"}, Name: "Example", Path: f.project, SyncID: strings.Repeat("a", 64)}
	b, _ := json.Marshal(p)
	if err := f.reg.Put("projects", "x", b, nil); err != nil {
		t.Fatal(err)
	}
	row, err := f.reg.Project("x")
	if err != nil || row.SyncID != "" {
		t.Fatal("client invented checkpoint ancestry", err)
	}
	if err = f.reg.EnsureSyncIdentity("x"); err != nil {
		t.Fatal(err)
	}
	row, err = f.reg.Project("x")
	if err != nil {
		t.Fatal(err)
	}
	identity, revision := row.SyncID, row.Revision
	row.SyncID = strings.Repeat("b", 64)
	row.Name = "Renamed"
	b, _ = json.Marshal(row)
	if err = f.reg.Put("projects", "x", b, &revision); err != nil {
		t.Fatal(err)
	}
	row, err = f.reg.Project("x")
	if err != nil || row.SyncID != identity || row.Name != "Renamed" {
		t.Fatal("client replaced logical identity", err)
	}
	other := registry.Project{Record: registry.Record{ID: "legacy"}, Name: "Legacy", Path: filepath.Join(f.root, "legacy"), SyncID: identity}
	if err = f.reg.Import(registry.Import{Projects: []registry.Project{other}}); err != nil {
		t.Fatal(err)
	}
	row, err = f.reg.Project("legacy")
	if err != nil || row.SyncID != "" {
		t.Fatal("legacy import invented a replica relationship", err)
	}
}

func TestDivergentNativeContinuationsPreserveBothHistories(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	xp, yp := nativeFixture{filepath.Join(x.root, "native")}, nativeFixture{filepath.Join(y.root, "native")}
	x.s.porters["fixture"], y.s.porters["fixture"] = xp, yp
	addChat(t, x, "chat", "shared")
	path := filepath.Join("session", "shared", "main.jsonl")
	base := "{\"text\":\"shared context\"}\n"
	putFile(t, filepath.Join(xp.root, path), base)
	c := mustRun(t, x.s, "export", Request{ID: "e", ProjectID: "x"})
	relay(t, x.s, y.s, c.ResultCheckpointID)
	dest := mustRun(t, y.s, "import", Request{ID: "i", CheckpointID: c.ResultCheckpointID, Path: y.project})
	putFile(t, filepath.Join(xp.root, path), base+"{\"text\":\"X continuation\"}\n")
	putFile(t, filepath.Join(yp.root, path), base+"{\"text\":\"Y continuation\"}\n")
	putFile(t, filepath.Join(y.project, "new-file"), "keep in incoming checkpoint")
	c = mustRun(t, y.s, "export", Request{ID: "back", ProjectID: dest.ResultProjectID})
	relay(t, y.s, x.s, c.ResultCheckpointID)
	xh, yh := treeHash(t, xp.root), treeHash(t, yp.root)
	o := run(t, x.s, "import", Request{ID: "diverged", CheckpointID: c.ResultCheckpointID})
	if o.Status != "conflicts" || len(o.Conflicts) != 1 || o.Conflicts[0].Kind != "conversation" {
		t.Fatalf("unsafe conversation merge: %+v", o)
	}
	if treeHash(t, xp.root) != xh || treeHash(t, yp.root) != yh {
		t.Fatal("a divergent native timeline was changed")
	}
	if _, err := os.Stat(filepath.Join(x.project, "new-file")); !os.IsNotExist(err) {
		t.Fatal("conversation conflict partially applied project files")
	}
}

func TestPendingForkDoesNotStealItsParentsNativeIdentity(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	xp, yp := nativeFixture{filepath.Join(x.root, "native")}, nativeFixture{filepath.Join(y.root, "native")}
	x.s.porters["fixture"], y.s.porters["fixture"] = xp, yp
	addChat(t, x, "parent", "shared")
	if err := x.store.ForkChat("parent", "fork"); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(registry.Chat{Record: registry.Record{ID: "fork"}, AgentID: "agent", ProjectID: "x", SessionID: "shared", Title: "Pending fork"})
	if err := x.reg.Put("chats", "fork", b, nil); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(xp.root, "session/shared/main.jsonl"), "{\"text\":\"parent history\"}\n")
	c := mustRun(t, x.s, "export", Request{ID: "e", ProjectID: "x"})
	relay(t, x.s, y.s, c.ResultCheckpointID)
	o := mustRun(t, y.s, "import", Request{ID: "i", CheckpointID: c.ResultCheckpointID, Path: y.project})
	snap, err := y.reg.Snapshot(nil)
	if err != nil || len(snap.Chats) != 2 {
		t.Fatal("fork membership missing", err)
	}
	var fork string
	for _, ref := range y.store.ChatSessions() {
		if ref.ForkPending {
			fork = ref.ChatID
		}
	}
	if fork == "" {
		t.Fatal("fork-on-next-turn was lost")
	}
	if err = y.reg.Delete("chats", fork, nil, true, y.store); err != nil {
		t.Fatal(err)
	}
	if err = y.reg.SyncNativeSessions(registry.NativeInventory{ProjectID: o.ResultProjectID, CWD: y.project, AgentType: "fixture", AgentName: "Fixture", Sessions: []agent.NativeSession{{ID: "shared", CWD: y.project, Title: "parent", CreatedAt: now(), UpdatedAt: now()}}}, y.store, nil); err != nil {
		t.Fatal(err)
	}
	snap, err = y.reg.Snapshot(nil)
	if err != nil || len(snap.Chats) != 1 || snap.Chats[0].ID == fork {
		t.Fatal("deleting an unused fork hid its parent", err)
	}
}

func TestRestartAfterCommitDoesNotReplayFilesOverNewWork(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, "file"), "initial")
	c := mustRun(t, x.s, "export", Request{ID: "e", ProjectID: "x"})
	relay(t, x.s, y.s, c.ResultCheckpointID)
	o := mustRun(t, y.s, "import", Request{ID: "i", CheckpointID: c.ResultCheckpointID, Path: y.project})
	y.s.Close()
	// Emulate losing the status write after the registry transaction committed.
	o.Status = "applying"
	if err := y.s.save(o); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(y.project, "file"), "new work after acknowledged commit")
	s, err := New(y.reg, y.store, nil, &sync.Mutex{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	y.s = s
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	recovered, err := s.Wait(ctx, o.ID)
	if err != nil || recovered.Status != "succeeded" {
		t.Fatalf("commit recovery: %+v %v", recovered, err)
	}
	if readFile(t, filepath.Join(y.project, "file")) != "new work after acknowledged commit" {
		t.Fatal("recovery overwrote newer work")
	}
}

func TestUnicodeAliasesAndCorruptGitFailBeforeApplying(t *testing.T) {
	f := testService(t)
	id := strings.Repeat("a", 64)
	m := Manifest{Version: 1, ProjectID: id, Name: "Example", Parents: []string{}, Entries: map[string]Entry{
		"files/caf\u00e9": {Kind: "file", Mode: 0644}, "files/cafe\u0301": {Kind: "file", Mode: 0644},
	}, Chats: map[string]Chat{}}
	if err := f.s.validateManifest(m); err == nil {
		t.Fatal("normalization aliases accepted")
	}
	m.Entries = map[string]Entry{"files/new": {Kind: "file", Mode: 0644}}
	bad, err := f.s.blob(context.Background(), strings.NewReader("this is not a Git bundle"))
	if err != nil {
		t.Fatal(err)
	}
	m.Git = &Git{Head: "refs/heads/main", Refs: map[string]string{"refs/heads/main": strings.Repeat("1", 40)}, Index: map[string]Entry{}, Bundle: &bad}
	c, err := f.s.seal(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(f.project, "local"), "preserve")
	before := treeHash(t, f.project)
	o := run(t, f.s, "import", Request{ID: "bad-git", CheckpointID: c.ID, Path: f.project})
	if o.Status != "failed" || treeHash(t, f.project) != before {
		t.Fatalf("invalid bundle mutated the destination: %+v", o)
	}
}

func TestSpecialGitStateIsNeverSilentlyDropped(t *testing.T) {
	for _, kind := range []string{"stash", "assume-unchanged", "intent-to-add"} {
		t.Run(kind, func(t *testing.T) {
			f := testService(t)
			addProject(t, f, "x")
			tgit(t, f.project, "init", "-b", "main")
			putFile(t, filepath.Join(f.project, "file"), "committed")
			tgit(t, f.project, "add", ".")
			tgit(t, f.project, "commit", "-m", "base")
			switch kind {
			case "stash":
				putFile(t, filepath.Join(f.project, "file"), "saved work")
				tgit(t, f.project, "stash", "push", "-m", "keep this code")
			case "assume-unchanged":
				tgit(t, f.project, "update-index", "--assume-unchanged", "file")
			case "intent-to-add":
				putFile(t, filepath.Join(f.project, "new"), "unstaged intent")
				tgit(t, f.project, "add", "-N", "new")
			}
			before := treeHash(t, f.project)
			o := run(t, f.s, "export", Request{ID: "export", ProjectID: "x"})
			if o.Status != "failed" || before != treeHash(t, f.project) {
				t.Fatalf("special Git state was omitted or changed: %+v", o)
			}
		})
	}
}

func TestFolderTypeChangeConflictsBeforeAnyWrite(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	putFile(t, filepath.Join(x.project, "folder", "item"), "saved")
	exported := mustRun(t, x.s, "export", Request{ID: "e", ProjectID: "x"})
	relay(t, x.s, y.s, exported.ResultCheckpointID)
	dest := mustRun(t, y.s, "import", Request{ID: "i", CheckpointID: exported.ResultCheckpointID, Path: y.project})
	if err := os.Remove(filepath.Join(y.project, "folder", "item")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(y.project, "folder")); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(y.project, "folder"), "now a file")
	putFile(t, filepath.Join(y.project, "another"), "incoming")
	exported = mustRun(t, y.s, "export", Request{ID: "back", ProjectID: dest.ResultProjectID})
	relay(t, y.s, x.s, exported.ResultCheckpointID)
	before := treeHash(t, x.project)
	result := run(t, x.s, "import", Request{ID: "return", CheckpointID: exported.ResultCheckpointID})
	if result.Status != "conflicts" || before != treeHash(t, x.project) {
		t.Fatalf("type change partially applied: %+v", result)
	}
}

func TestImageArtifactsAndExternalAttachmentsRoundTrip(t *testing.T) {
	x, y := testService(t), testService(t)
	addProject(t, x, "x")
	xp, yp := nativeFixture{filepath.Join(x.root, "native")}, nativeFixture{filepath.Join(y.root, "native")}
	x.s.porters["fixture"], y.s.porters["fixture"] = xp, yp
	addChat(t, x, "chat", "one")
	putFile(t, filepath.Join(xp.root, "session/one/main.jsonl"), "{\"native\":true}\n")
	attachment := filepath.Join(x.root, "external-image.png")
	putFile(t, attachment, "image content")
	if err := x.store.AddMessage(session.Message{ID: "image-message", ChatID: "chat", Role: "user", Text: "inspect", CreatedAt: now(), Attachments: []agent.Attachment{{Path: attachment, Mime: "image/png"}}}); err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef0123456789abcdef"
	row := artifact.Record{ID: id, ChatID: "chat", Mime: "image/png", Bytes: 3, Width: 1, Height: 1}
	putFile(t, filepath.Join(x.reg.Directory(), "artifacts", id+".png"), "png")
	if err := x.reg.SurfacePut("artifact", id, row); err != nil {
		t.Fatal(err)
	}
	exported := mustRun(t, x.s, "export", Request{ID: "e", ProjectID: "x"})
	relay(t, x.s, y.s, exported.ResultCheckpointID)
	dest := mustRun(t, y.s, "import", Request{ID: "i", CheckpointID: exported.ResultCheckpointID, Path: y.project})
	snap, err := y.reg.Snapshot(nil)
	if err != nil || len(snap.Chats) != 1 {
		t.Fatal(err)
	}
	messages := y.store.Messages(snap.Chats[0].ID)
	if len(messages) != 1 || string(messages[0].Attachments[0].Data) != "image content" || messages[0].Attachments[0].Path != "" {
		t.Fatal("attachment lost on destination")
	}
	var saved artifact.Record
	if err = y.reg.SurfaceGet("artifact", id, &saved); err != nil || saved.ChatID != snap.Chats[0].ID {
		t.Fatal("artifact points to old local ID", err)
	}
	if readFile(t, filepath.Join(y.reg.Directory(), "artifacts", id+".png")) != "png" {
		t.Fatal("artifact missing")
	}
	before, _ := json.Marshal(x.store.Messages("chat"))
	exported = mustRun(t, y.s, "export", Request{ID: "back", ProjectID: dest.ResultProjectID})
	relay(t, y.s, x.s, exported.ResultCheckpointID)
	mustRun(t, x.s, "import", Request{ID: "return", CheckpointID: exported.ResultCheckpointID})
	if len(x.store.Messages("chat")) != 1 || len(before) == 0 {
		t.Fatal("attachment return duplicated history")
	}
	if err = x.reg.SurfaceGet("artifact", id, &saved); err != nil || saved.ChatID != "chat" {
		t.Fatal("artifact return changed ownership", err)
	}
	if readFile(t, attachment) != "image content" {
		t.Fatal("external source attachment mutated")
	}
}
