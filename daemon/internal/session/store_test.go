package session

import (
	"path/filepath"
	"sort"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

// TestTitleRoundtrip: a user title persists, is returned by Title/Chats, and an empty title clears it.
func TestTitleRoundtrip(t *testing.T) {
	st := openTestStore(t)
	if err := st.AddMessage(Message{ID: "m1", ChatID: "c1", Role: "user", Text: "hello world", CreatedAt: "t1"}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	if got := st.Title("c1"); got != "" {
		t.Fatalf("Title before set = %q, want empty", got)
	}
	if err := st.SetTitle("c1", "My Renamed Chat"); err != nil {
		t.Fatalf("set title: %v", err)
	}
	if got := st.Title("c1"); got != "My Renamed Chat" {
		t.Fatalf("Title = %q, want %q", got, "My Renamed Chat")
	}
	// A user title wins over the derived first-message snippet in the listing.
	if chats := st.Chats(); len(chats) != 1 || chats[0].Title != "My Renamed Chat" {
		t.Fatalf("Chats title = %+v, want user title to win", chats)
	}
	if sum := st.ChatSummaryFor("c1"); sum.Title != "My Renamed Chat" {
		t.Fatalf("ChatSummaryFor title = %q, want user title to win", sum.Title)
	}

	// Empty title clears the rename, reverting to the derived snippet.
	if err := st.SetTitle("c1", ""); err != nil {
		t.Fatalf("clear title: %v", err)
	}
	if got := st.Title("c1"); got != "" {
		t.Fatalf("Title after clear = %q, want empty", got)
	}
	if chats := st.Chats(); chats[0].Title != "hello world" {
		t.Fatalf("Chats title after clear = %q, want derived snippet", chats[0].Title)
	}
}

// TestTitlePersistsAcrossOpen: a rename survives a store reopen (it is written to disk).
func TestTitlePersistsAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetTitle("c1", "Persisted"); err != nil {
		t.Fatalf("set title: %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.Title("c1"); got != "Persisted" {
		t.Fatalf("Title after reopen = %q, want %q", got, "Persisted")
	}
}

// TestDeleteChat: a true delete purges every map keyed to the chat and returns one SessionRef per
// (agent, session) it mapped to, carrying the chat's cwd so the API can drive native deletion.
func TestDeleteChat(t *testing.T) {
	st := openTestStore(t)
	// Two agents on the same chat, plus a bystander chat that must survive untouched.
	if err := st.SetSession("claude", "c1", "sid-claude"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := st.SetSession("codex", "c1", "sid-codex"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := st.SetSession("claude", "c2", "sid-other"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := st.SetChatCWD("c1", "/work/proj"); err != nil {
		t.Fatalf("set cwd: %v", err)
	}
	if err := st.SetTitle("c1", "Doomed"); err != nil {
		t.Fatalf("set title: %v", err)
	}
	if err := st.AddMessage(Message{ID: "m1", ChatID: "c1", Role: "user", Text: "hi", CreatedAt: "t1"}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	if err := st.AddMessage(Message{ID: "m2", ChatID: "c2", Role: "user", Text: "keep", CreatedAt: "t2"}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	if err := st.SaveRun(Run{ID: "r1", ChatID: "c1", Agent: "claude", Status: "done"}); err != nil {
		t.Fatalf("save run: %v", err)
	}
	if err := st.SaveRun(Run{ID: "r2", ChatID: "c2", Agent: "claude", Status: "done"}); err != nil {
		t.Fatalf("save run: %v", err)
	}

	refs, err := st.DeleteChat("c1")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Two refs, one per agent, each carrying the shared cwd. Sort for a stable comparison.
	sort.Slice(refs, func(i, j int) bool { return refs[i].Agent < refs[j].Agent })
	want := []SessionRef{
		{Agent: "claude", SID: "sid-claude", CWD: "/work/proj"},
		{Agent: "codex", SID: "sid-codex", CWD: "/work/proj"},
	}
	if len(refs) != 2 || refs[0] != want[0] || refs[1] != want[1] {
		t.Fatalf("refs = %+v, want %+v", refs, want)
	}

	// Every c1 map entry is gone.
	if st.Session("claude", "c1") != "" || st.Session("codex", "c1") != "" {
		t.Fatalf("sessions for c1 survived delete")
	}
	if st.ChatCWD("c1") != "" {
		t.Fatalf("cwd for c1 survived delete")
	}
	if st.Title("c1") != "" {
		t.Fatalf("title for c1 survived delete")
	}
	if len(st.Messages("c1")) != 0 {
		t.Fatalf("messages for c1 survived delete")
	}
	if _, ok := st.LatestRun("c1"); ok {
		t.Fatalf("run for c1 survived delete")
	}

	// The bystander chat is untouched.
	if st.Session("claude", "c2") != "sid-other" {
		t.Fatalf("bystander session c2 was disturbed")
	}
	if len(st.Messages("c2")) != 1 {
		t.Fatalf("bystander messages c2 were disturbed")
	}
	if _, ok := st.LatestRun("c2"); !ok {
		t.Fatalf("bystander run c2 was disturbed")
	}
}

// TestDeleteChatIdempotent: deleting an unknown chat is not an error and returns no refs.
func TestDeleteChatIdempotent(t *testing.T) {
	st := openTestStore(t)
	refs, err := st.DeleteChat("ghost")
	if err != nil {
		t.Fatalf("delete unknown: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("refs = %+v, want none", refs)
	}
}

// TestForkChat: a fork seeds the session mapping for every agent, records a pending-fork marker per
// agent, copies the cwd and title, and does NOT copy messages (native history is shared until branch).
func TestForkChat(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetSession("claude", "src", "sid-claude"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := st.SetSession("codex", "src", "sid-codex"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := st.SetChatCWD("src", "/work/proj"); err != nil {
		t.Fatalf("set cwd: %v", err)
	}
	if err := st.SetTitle("src", "Original"); err != nil {
		t.Fatalf("set title: %v", err)
	}
	if err := st.AddMessage(Message{ID: "m1", ChatID: "src", Role: "user", Text: "hi", CreatedAt: "t1"}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	if err := st.ForkChat("src", "dst"); err != nil {
		t.Fatalf("fork: %v", err)
	}

	// Session mapping seeded from the source for both agents.
	if st.Session("claude", "dst") != "sid-claude" {
		t.Fatalf("claude session not seeded: %q", st.Session("claude", "dst"))
	}
	if st.Session("codex", "dst") != "sid-codex" {
		t.Fatalf("codex session not seeded: %q", st.Session("codex", "dst"))
	}
	// cwd + title copied.
	if st.ChatCWD("dst") != "/work/proj" {
		t.Fatalf("cwd not copied: %q", st.ChatCWD("dst"))
	}
	if st.Title("dst") != "Original" {
		t.Fatalf("title not copied: %q", st.Title("dst"))
	}
	// Messages NOT copied (fork shares native history until it branches).
	if len(st.Messages("dst")) != 0 {
		t.Fatalf("messages were copied into the fork: %+v", st.Messages("dst"))
	}
	// A pending-fork marker exists for each agent, consumed exactly once.
	if !st.TakeForkPending("claude", "dst") {
		t.Fatalf("claude fork-pending marker missing")
	}
	if st.TakeForkPending("claude", "dst") {
		t.Fatalf("claude fork-pending marker not cleared after first take")
	}
	if !st.TakeForkPending("codex", "dst") {
		t.Fatalf("codex fork-pending marker missing")
	}
}

// TestForkChatRejects: empty target, self-fork, unknown source, and an existing target all error.
func TestForkChatRejects(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetSession("claude", "src", "sid"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if err := st.SetSession("claude", "taken", "sid2"); err != nil {
		t.Fatalf("set session: %v", err)
	}

	if err := st.ForkChat("src", ""); err == nil {
		t.Fatalf("empty target: want error")
	}
	if err := st.ForkChat("src", "src"); err == nil {
		t.Fatalf("self-fork: want error")
	}
	if err := st.ForkChat("ghost", "dst"); err == nil {
		t.Fatalf("unknown source: want error")
	}
	if err := st.ForkChat("src", "taken"); err == nil {
		t.Fatalf("existing target: want error")
	}
}
