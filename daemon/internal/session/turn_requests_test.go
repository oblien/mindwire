package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTurnAdmissionRollsBackWhenPersistenceFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	st, err := Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "one", ChatID: "chat", Status: "running"}
	message := Message{ID: "message", ChatID: "chat", Role: "user", Text: "fixture"}
	if st.SaveTurnStart(run, &message, "request", "digest") == nil {
		t.Fatal("missing state directory accepted a turn")
	}
	if _, found := st.GetRun(run.ID); found || len(st.Messages("chat")) != 0 {
		t.Fatal("failed admission left partial history")
	}
	if _, found, err := st.TurnRequest("request", "digest"); found || err != nil {
		t.Fatal("failed admission left a receipt")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTurnStart(run, &message, "request", "digest"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := st.TurnRequest("request", "digest"); !found || err != nil {
		t.Fatal("retry after persistence recovery failed")
	}
	_, _ = st.DeleteChat("chat")
	if _, found, err := st.TurnRequest("request", "digest"); found || err == nil {
		t.Fatal("deleted run receipt must not repeat the operation")
	}
}
