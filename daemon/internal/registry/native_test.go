package registry

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

func nativeFixture(t *testing.T) (*Store, *session.Store, NativeInventory) {
	t.Helper()
	st := openTest(t)
	native, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cwd, _ := workspacepath.Canonical(t.TempDir())
	if err := st.Import(Import{Projects: []Project{{Record: Record{ID: "project"}, Name: "Existing folder", Path: cwd}}}); err != nil {
		t.Fatal(err)
	}
	return st, native, NativeInventory{ProjectID: "project", CWD: cwd, AgentType: "codex", AgentName: "OpenAI Codex", Sessions: []agent.NativeSession{
		{ID: "native-one", CWD: cwd, Title: "Native conversation", CreatedAt: "2026-09-20T00:00:00Z", UpdatedAt: "2026-09-25T00:00:00Z"},
	}}
}

func syncNative(t *testing.T, st *Store, native *session.Store, in NativeInventory) Snapshot {
	t.Helper()
	if err := st.SyncNativeSessions(in, native, nil); err != nil {
		t.Fatal(err)
	}
	return snapshot(t, st, nil)
}

func TestNativeInventoryReusesProfileChatAndPersistsOnlyReferences(t *testing.T) {
	st, native, in := nativeFixture(t)
	if err := st.Import(Import{Agents: []Agent{{Record: Record{ID: "my-codex"}, Name: "My Codex", AgentType: "codex"}},
		Chats: []Chat{{Record: Record{ID: "already-known"}, AgentID: "my-codex", ProjectID: "project", Title: "Old prompt"}}}); err != nil {
		t.Fatal(err)
	}
	if err := native.SetSession("codex", "already-known", "thread-one"); err != nil {
		t.Fatal(err)
	}
	in.Sessions[0].Aliases = []string{"thread-one"}
	in.Sessions = append(in.Sessions, agent.NativeSession{ID: "native-two", CWD: in.CWD, Title: "Only in the CLI", CreatedAt: "2026-09-19T00:00:00Z", UpdatedAt: "2026-09-24T00:00:00Z"})
	first := syncNative(t, st, native, in)
	if len(first.Agents) != 1 || first.Agents[0].ID != "my-codex" || len(first.Chats) != 2 {
		t.Fatalf("duplicated profile/chat: %+v", first)
	}
	found := false
	for _, chat := range first.Chats {
		if chat.AgentID != "my-codex" || chat.SessionID == "" || chat.UpdatedAt == "" {
			t.Fatalf("invalid native reference: %+v", chat)
		}
		if len(native.Messages(chat.ID)) != 0 {
			t.Fatal("discovery copied a transcript")
		}
		if chat.ID == "already-known" {
			found = true
			if chat.SessionID != "thread-one" || chat.Title != "Native conversation" {
				t.Fatal(chat)
			}
		}
	}
	if !found {
		t.Fatal("existing app chat ID changed")
	}
	if got := syncNative(t, st, native, in); got.Revision != first.Revision {
		t.Fatal("an identical native inventory changed the revision")
	}
	chat, _, _, _ := st.ChatContext("already-known")
	chat.Title, chat.TitleIsUserSet = "Pinned title", true
	mute := true
	chat.NotificationsMuted = &mute
	body, _ := json.Marshal(chat)
	if err := st.Put("chats", chat.ID, body, &chat.Revision); err != nil {
		t.Fatal(err)
	}
	in.Sessions[0].Title, in.Sessions[0].UpdatedAt = "New native title", "2026-09-26T00:00:00Z"
	syncNative(t, st, native, in)
	chat, _, _, _ = st.ChatContext(chat.ID)
	if chat.Title != "Pinned title" || chat.NotificationsMuted == nil || !*chat.NotificationsMuted || chat.UpdatedAt != in.Sessions[0].UpdatedAt {
		t.Fatal("refresh overwrote preferences or lost external activity")
	}
	// An older client that only knows title must retain native pointers/activity.
	body = []byte(`{"agentId":"my-codex","projectId":"project","title":"Another pinned title","titleIsUserSet":true}`)
	if err := st.Put("chats", chat.ID, body, &chat.Revision); err != nil {
		t.Fatal(err)
	}
	chat, _, _, _ = st.ChatContext(chat.ID)
	if chat.SessionID != "thread-one" || chat.UpdatedAt != in.Sessions[0].UpdatedAt {
		t.Fatal("older client cleared native metadata")
	}
	if err := st.RenameChat(chat.ID, ""); err != nil {
		t.Fatal(err)
	}
	chat, _, _, _ = st.ChatContext(chat.ID)
	if chat.TitleIsUserSet || chat.Title != "New native title" {
		t.Fatal("clearing the app rename did not restore the cached native title")
	}
}

func TestNativeInventoryRemovalReappearanceAndExplicitDelete(t *testing.T) {
	st, native, in := nativeFixture(t)
	first := syncNative(t, st, native, in)
	id := first.Chats[0].ID
	chat := first.Chats[0]
	chat.Title, chat.TitleIsUserSet = "Keep my title", true
	mute := true
	chat.NotificationsMuted = &mute
	body, _ := json.Marshal(chat)
	if err := st.Put("chats", id, body, &chat.Revision); err != nil {
		t.Fatal(err)
	}
	empty := in
	empty.Sessions = nil
	removed := syncNative(t, st, native, empty)
	if len(removed.Chats) != 0 || len(removed.Deleted) != 1 {
		t.Fatalf("external removal not reconciled: %+v", removed)
	}
	if again := syncNative(t, st, native, empty); again.Revision != removed.Revision {
		t.Fatal("unchanged absent session kept changing the revision")
	}
	back := syncNative(t, st, native, in)
	if len(back.Chats) != 1 || back.Chats[0].ID != id || len(back.Deleted) != 0 {
		t.Fatal("returning native session lost stable identity")
	}
	if back.Chats[0].Title != "Keep my title" || back.Chats[0].NotificationsMuted == nil || !*back.Chats[0].NotificationsMuted {
		t.Fatal("native archive/unarchive lost app preferences")
	}
	if err := st.Delete("chats", id, nil, true, native); err != nil {
		t.Fatal(err)
	}
	if _, err := native.DeleteChat(id); err != nil {
		t.Fatal(err)
	}
	if got := syncNative(t, st, native, in); len(got.Chats) != 0 {
		t.Fatal("explicitly deleted conversation resurrected")
	}
}

func TestNativeProjectForgetThenReaddKeepsHarnessHistoryDiscoverable(t *testing.T) {
	st, native, in := nativeFixture(t)
	first := syncNative(t, st, native, in)
	oldID := first.Chats[0].ID
	if err := st.Delete("projects", "project", nil, true, native); err != nil {
		t.Fatal(err)
	}
	if native.Session("codex", oldID) != "native-one" {
		t.Fatal("metadata-only removal touched native reference")
	}
	if err := st.Import(Import{Projects: []Project{{Record: Record{ID: "readded"}, Name: "Same folder", Path: in.CWD}}}); err != nil {
		t.Fatal(err)
	}
	in.ProjectID = "readded"
	got := syncNative(t, st, native, in)
	if len(got.Chats) != 1 || got.Chats[0].ProjectID != "readded" || got.Chats[0].ID == oldID || got.Chats[0].SessionID != "native-one" {
		t.Fatalf("readded folder: %+v", got)
	}
}

func TestExplicitNativeDeleteWhileAbsentStillSuppressesReappearance(t *testing.T) {
	st, native, in := nativeFixture(t)
	first := syncNative(t, st, native, in)
	id := first.Chats[0].ID
	empty := in
	empty.Sessions = nil
	syncNative(t, st, native, empty)
	if err := st.Delete("chats", id, nil, true, native); err != nil {
		t.Fatal(err)
	}
	if got := syncNative(t, st, native, in); len(got.Chats) != 0 {
		t.Fatal("a late explicit delete was lost")
	}
}

func TestNativeInventoryProtectsDraftsRunningTurnsAndPendingForks(t *testing.T) {
	st, native, in := nativeFixture(t)
	first := syncNative(t, st, native, in)
	id, profile := first.Chats[0].ID, first.Chats[0].AgentID
	if _, err := st.ForkChat(id, "fork", native); err != nil {
		t.Fatal(err)
	}
	if err := st.Import(Import{Chats: []Chat{{Record: Record{ID: "draft"}, AgentID: profile, ProjectID: "project", Title: "New chat"}}}); err != nil {
		t.Fatal(err)
	}
	if got := syncNative(t, st, native, in); len(got.Chats) != 3 {
		t.Fatalf("fork duplicated native chat: %+v", got.Chats)
	}
	empty := in
	empty.Sessions = nil
	if err := st.SyncNativeSessions(empty, native, func(chatID string) bool { return chatID == id }); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, st, nil); len(got.Chats) != 3 {
		t.Fatal("pruned a running turn or a draft/fork")
	}
	if got := syncNative(t, st, native, empty); len(got.Chats) != 2 {
		t.Fatal("did not prune absent native source after run finished")
	}
}

func TestDeletingBeforeFirstDiscoveryCannotResurrectNativeChat(t *testing.T) {
	st, native, in := nativeFixture(t)
	if err := st.Import(Import{Agents: []Agent{{Record: Record{ID: "profile"}, Name: "Codex", AgentType: "codex"}},
		Chats: []Chat{{Record: Record{ID: "known"}, AgentID: "profile", ProjectID: "project", Title: "New chat"}}}); err != nil {
		t.Fatal(err)
	}
	if err := native.SetSession("codex", "known", "native-one"); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("chats", "known", nil, true, native); err != nil {
		t.Fatal(err)
	}
	if _, err := native.DeleteChat("known"); err != nil {
		t.Fatal(err)
	}
	if got := syncNative(t, st, native, in); len(got.Chats) != 0 {
		t.Fatal("native session returned with another app ID")
	}
}
