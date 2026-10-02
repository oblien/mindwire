package session

import (
	"os"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestForkReceiptAndBoundarySurviveRestartUntilDistinctSessionIsSaved(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetSession("codex", "source", "native-source"); err != nil {
		t.Fatal(err)
	}
	point := agent.ForkPoint{BeforeMessageID: "user-two", ResumeAt: "turn-one"}
	if err := st.ForkChat("source", "branch", ForkOptions{Agent: "codex", Point: point}); err != nil {
		t.Fatal(err)
	}
	st, err := Open(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.PendingFork("codex", "branch"); got == nil || *got != point {
		t.Fatal("pending boundary was lost")
	}
	if err := st.SetSession("codex", "branch", "native-source"); err == nil {
		t.Fatal("source identity accepted for a fork")
	}
	if st.PendingFork("codex", "branch") == nil {
		t.Fatal("failure consumed pending fork")
	}
	if err := st.SetSession("codex", "branch", "native-branch"); err != nil {
		t.Fatal(err)
	}
	st, err = Open(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if st.PendingFork("codex", "branch") != nil || !st.ForkMatches("source", "branch", "user-two") || st.ForkMatches("source", "branch", "another-user") {
		t.Fatal("completion lost the receipt or kept the pending marker")
	}
	if st.Session("codex", "source") != "native-source" {
		t.Fatal("source changed")
	}
}

func TestForkWritesRollBackInMemoryWhenPersistenceFails(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetSession("codex", "source", "native-source"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(st.path+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if err := st.ForkChat("source", "branch"); err == nil {
		t.Fatal("failed disk write reported success")
	}
	if st.ChatExists("branch") || st.PendingFork("codex", "branch") != nil {
		t.Fatal("failed fork leaked into memory")
	}
	if err := os.Remove(st.path + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := st.ForkChat("source", "branch"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(st.path+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSession("codex", "branch", "new-native"); err == nil {
		t.Fatal("failed native ID write reported success")
	}
	if st.Session("codex", "branch") != "native-source" || st.PendingFork("codex", "branch") == nil {
		t.Fatal("failed commit disarmed fork")
	}
}

func TestPortablePendingForkKeepsItsNativeBoundary(t *testing.T) {
	st := openTestStore(t)
	_ = st.SetSession("claude-code", "source", "native")
	point := agent.ForkPoint{BeforeMessageID: "user-two", ResumeAt: "assistant-one"}
	if err := st.ForkChat("source", "branch", ForkOptions{Agent: "claude-code", Point: point}); err != nil {
		t.Fatal(err)
	}
	data, err := st.ExportChat("claude-code", "branch")
	if err != nil {
		t.Fatal(err)
	}
	other := openTestStore(t)
	if err := other.ImportChats([]ChatImport{{ChatID: "imported", Agent: "claude-code", SessionID: "native", Data: data}}); err != nil {
		t.Fatal(err)
	}
	if p := other.PendingFork("claude-code", "imported"); p == nil || *p != point {
		t.Fatal("import lost the fork boundary")
	}
}
