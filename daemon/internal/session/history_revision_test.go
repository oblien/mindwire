package session

import (
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestHistoryRevisionTracksContentAndInteractionMutationsButNotUnrelatedWork(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	previous := store.HistoryRevision("chat")
	check := func(name string, mutate func() error) {
		t.Helper()
		if err := mutate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		next := store.HistoryRevision("chat")
		if next == previous {
			t.Fatalf("%s did not invalidate history", name)
		}
		previous = next
	}
	check("native session", func() error { return store.SetSession("claude", "chat", "native") })
	check("native directory", func() error { return store.SetChatCWD("chat", "/workspace") })
	check("message", func() error { return store.AddMessage(Message{ID: "a", ChatID: "chat", Text: "One"}) })
	check("same-ID replacement", func() error { return store.SaveTurnMessages([]Message{{ID: "a", ChatID: "chat", Text: "Two"}}) })
	check("turn start", func() error {
		return store.SaveTurnStart(Run{ID: "run", ChatID: "chat", Status: "running"}, nil, "", "")
	})
	check("queued input", func() error {
		_, err := store.SaveRunInput("run", agent.UserInput{ID: "input", Status: "queued"}, "digest")
		return err
	})
	check("accepted input", func() error {
		return store.UpdateRunInput("run", agent.UserInput{ID: "input", Status: "accepted", Text: "Continue"})
	})
	check("question", func() error {
		_, err := store.RecordMessageQuestion("chat", "run", agent.Interaction{ID: "question", NeedsResponse: true})
		return err
	})
	check("cancellation", func() error { return store.SaveRun(Run{ID: "run", ChatID: "chat", Status: "cancelled"}) })
	_ = store.Set("codex:model", "fixture")
	_ = store.AddMessage(Message{ID: "other", ChatID: "other", Text: "Nothing changed here"})
	_ = store.RestoreChatSessions([]ChatSession{{ChatID: "chat", Agent: "claude", SID: "native", CWD: "/workspace"}})
	if store.HistoryRevision("chat") != previous {
		t.Fatal("unchanged discovery/configuration changed the chat token")
	}
	reopened, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.HistoryRevision("chat") == previous {
		t.Fatal("restart must establish a new cache epoch")
	}
}
