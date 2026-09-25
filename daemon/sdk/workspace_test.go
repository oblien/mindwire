package mindwire

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceDiscoversAndReadsNativeCLIConversations(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("CODEX_HOME", t.TempDir())
	path := filepath.Join(home, "projects", strings.ReplaceAll(cwd, "/", "-"), "external-claude.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	user, _ := json.Marshal(map[string]any{"type": "user", "uuid": "native-user", "sessionId": "external-claude", "cwd": cwd, "timestamp": "2026-09-24T00:00:00Z", "message": map[string]any{"role": "user", "content": "Started outside Mindwire"}})
	if err := os.WriteFile(path, append(user, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{StatePath: filepath.Join(t.TempDir(), "state.json"), CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	full, err := c.WithAgent("codex").Workspace.Projects.Put("project", WorkspaceProject{Name: "Existing folder", Path: cwd}, nil)
	if err != nil || len(full.Chats) != 1 || len(full.Agents) != 1 || full.Agents[0].AgentType != "claude-code" {
		t.Fatalf("native metadata: %+v %v", full, err)
	}
	chat := full.Chats[0]
	if chat.SessionID != "external-claude" || chat.UpdatedAt == "" {
		t.Fatal(chat)
	}
	messages, err := c.Messages(chat.ID, MessagesOptions{})
	if err != nil || len(messages) != 1 || messages[0].Text != "Started outside Mindwire" {
		t.Fatalf("native history: %+v %v", messages, err)
	}
	chats, err := c.ListChats(context.Background(), ChatListOptions{ProjectID: "project", Refresh: true})
	if err != nil || len(chats) != 1 || chats[0].ChatID != chat.ID {
		t.Fatalf("native list: %+v %v", chats, err)
	}
	if len(c.core.store.Messages(chat.ID)) != 0 {
		t.Fatal("SDK copied a native transcript into daemon state")
	}
	delta, err := c.Workspace.Changes(full.Revision, full.WorkspaceID, WorkspaceSyncOptions{Refresh: true})
	if err != nil || len(delta.Chats) != 0 {
		t.Fatalf("unchanged native metadata duplicated: %+v %v", delta, err)
	}
}

func TestWorkspaceUsesSharedRegistryAndProtectsActiveChats(t *testing.T) {
	c := newFakeClient(t, nil)
	if _, err := c.Workspace.Agents.Put("profile", WorkspaceAgent{Name: "Test agent", AgentType: "fake"}, nil); err != nil {
		t.Fatal(err)
	}
	projectPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Workspace.Projects.Put("project", WorkspaceProject{Name: "App", Path: projectPath}, nil); err != nil {
		t.Fatal(err)
	}
	created, err := c.Workspace.Chats.Put("chat", WorkspaceChat{AgentID: "profile", ProjectID: "project", Title: "Draft"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := c.WithAgent("codex").Workspace.Snapshot()
	if err != nil || !reflect.DeepEqual(other, created) {
		t.Fatalf("harness view changed registry: %+v, %v", other, err)
	}
	fork, err := c.ForkChat("chat", "fork")
	if err != nil || fork.Agent != "fake" || fork.Title != "Draft" {
		t.Fatalf("empty fork: %+v %v", fork, err)
	}

	run, err := c.Turn(context.Background(), TurnRequest{ChatID: "chat", Message: "hang", CWD: "/wrong-client-path"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for c.core.store.ChatCWD("chat") == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if c.core.store.ChatCWD("chat") != projectPath {
		t.Fatalf("turn directory = %q, want %q", c.core.store.ChatCWD("chat"), projectPath)
	}
	revision := created.Projects[0].Revision
	_, err = c.Workspace.Projects.Delete("project", &revision)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("active project removed: %v", err)
	}
	_, err = c.Workspace.RemoveProjectFiles("project", ProjectRemoveRequest{OperationID: "remove-running", ExpectedRevision: revision})
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("active project files removed: %v", err)
	}
	if _, err := os.Stat(projectPath); err != nil {
		t.Fatal("running project files were touched", err)
	}
	c.core.sup.Cancel(run.ID())
	c.core.sup.Wait()

	if _, err := c.RenameChat("chat", "Pinned from SDK"); err != nil {
		t.Fatal(err)
	}
	delta, err := c.Workspace.Changes(created.Revision, created.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, chat := range delta.Chats {
		if chat.ID == "chat" {
			found = chat.Title == "Pinned from SDK" && chat.TitleIsUserSet
		}
	}
	if !found {
		t.Fatalf("native rename missing in registry delta: %+v", delta)
	}
	deleted, err := c.Workspace.Projects.Delete("project", &revision)
	if err != nil || len(deleted.Chats) != 0 {
		t.Fatalf("cascade: %+v %v", deleted, err)
	}
	if len(c.Chats()) != 0 {
		t.Fatal("native chat list resurrected removed membership")
	}
	_, err = c.Turn(context.Background(), TurnRequest{ChatID: "chat", Message: "must not run"})
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusGone {
		t.Fatalf("deleted chat ran: %v", err)
	}
	_, err = c.Compact(context.Background(), CompactRequest{ChatID: "chat"})
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusGone {
		t.Fatalf("deleted chat compacted: %v", err)
	}
	_, err = c.Resolve(context.Background(), ResolveRequest{ChatID: "chat", Message: "must not run"})
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusGone {
		t.Fatalf("deleted chat resolved: %v", err)
	}
	after, err := c.Workspace.Import(created.Import)
	if err != nil || len(after.Projects) != 0 || len(after.Chats) != 0 {
		t.Fatalf("stale import resurrected data: %+v %v", after, err)
	}
}

func TestWorkspaceNotificationMutesApplyThroughEmbeddedSDK(t *testing.T) {
	notes := &capturingNotifier{}
	c := newFakeClient(t, notes)
	muted := true
	if _, err := c.Workspace.Agents.Put("profile", WorkspaceAgent{Name: "Muted agent", AgentType: "fake", NotificationsMuted: &muted}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Workspace.Projects.Put("project", WorkspaceProject{Name: "App", Path: t.TempDir()}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Workspace.Chats.Put("chat", WorkspaceChat{AgentID: "profile", ProjectID: "project", Title: "Chat"}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for index, wantNotes := range []int{0, 1} {
		if index == 1 {
			snapshot, err := c.Workspace.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			profile := snapshot.Agents[0]
			muted = false
			profile.NotificationsMuted = &muted
			if _, err := c.Workspace.Agents.Put(profile.ID, profile, &profile.Revision); err != nil {
				t.Fatal(err)
			}
		}
		run, err := c.Turn(ctx, TurnRequest{ChatID: "chat", Message: "script"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := run.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		if len(notes.all()) != wantNotes {
			t.Fatalf("notifications=%d, want %d", len(notes.all()), wantNotes)
		}
	}
	if c.Health().NotificationPreferencesVersion != 1 {
		t.Fatal("notification preference capability missing")
	}
}

func TestWorkspaceProjectOperationsShareTheEmbeddedService(t *testing.T) {
	c := newFakeClient(t, nil)
	path := filepath.Join(t.TempDir(), "app")
	o, err := c.Workspace.CreateProject(ProjectRequest{ProjectSpec: ProjectSpec{ID: "app", Source: "create", Name: "App", Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for current, err := range c.Workspace.Operations.Watch(ctx, o.ID) {
		if err != nil {
			t.Fatal(err)
		}
		o = current
	}
	if o.Status != "succeeded" {
		t.Fatalf("%+v", o)
	}
	snapshot, err := c.Workspace.Snapshot()
	if err != nil || len(snapshot.Projects) != 1 {
		t.Fatalf("%+v %v", snapshot, err)
	}
	p := snapshot.Projects[0]
	request := ProjectRemoveRequest{OperationID: "remove", ExpectedRevision: p.Revision}
	o, err = c.WithAgent("codex").Workspace.RemoveProjectFiles(p.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	for current, err := range c.Workspace.Operations.Watch(ctx, o.ID) {
		if err != nil {
			t.Fatal(err)
		}
		o = current
	}
	if o.Status != "succeeded" {
		t.Fatalf("%+v", o)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("folder remains: %v", err)
	}
	again, err := c.Workspace.RemoveProjectFiles(p.ID, request)
	if err != nil || again.Sequence != o.Sequence {
		t.Fatalf("replay %+v %v", again, err)
	}
}
