package session

import (
	"path/filepath"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestMessageQuestionAnswerStateSurvivesReplayAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	it := agent.Interaction{ID: "form", Kind: "form", ResponseMode: "message", NeedsResponse: true,
		Questions: []agent.Question{{ID: "q", Title: "Color?", AllowOther: true}}}
	if _, err := st.RecordMessageQuestion("chat", "run", it); err != nil {
		t.Fatal(err)
	}
	response := agent.InteractionResponse{InteractionID: "form", Answers: map[string]agent.QuestionAnswer{"q": {Text: "Blue"}}}
	if err := st.CommitInteractionReply(InteractionReply{RunID: "run", Response: response}, "next", Message{ID: "answer", ChatID: "chat", Role: "user", Text: "Blue"}, &Run{ID: "next", ChatID: "chat", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	replayed, err := st.RecordMessageQuestion("chat", "run", it)
	if err != nil || replayed.NeedsResponse || replayed.Response == nil {
		t.Fatalf("replay lost answer: %+v %v", replayed, err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	parts := reopened.OverlayInteractions("chat", []agent.Part{{Type: "interaction", Interaction: &it}})
	if parts[0].Interaction.Response == nil || parts[0].Interaction.NeedsResponse || parts[0].Interaction.RunID != "run" {
		t.Fatal("native history did not receive saved state")
	}
	if _, err := reopened.DeleteChat("chat"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.MessageQuestion("run", "form"); ok {
		t.Fatal("deleted chat retained an actionable question")
	}
}

func TestQuestionPersistenceFailureDoesNotRecordAnAcceptedReply(t *testing.T) {
	st, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	it := agent.Interaction{ID: "form", Kind: "form", ResponseMode: "message", NeedsResponse: true, Questions: []agent.Question{{ID: "q", Title: "Color?"}}}
	if _, err := st.RecordMessageQuestion("chat", "run", it); err != nil {
		t.Fatal(err)
	}
	st.path = filepath.Join(t.TempDir(), "missing", "state.json")
	ref := InteractionReply{RunID: "run", Response: agent.InteractionResponse{InteractionID: "form"}}
	if err := st.CommitInteractionReply(ref, "next", Message{ID: "answer", ChatID: "chat"}, &Run{ID: "next"}); err == nil {
		t.Fatal("failed disk save reported success")
	}
	q, _ := st.MessageQuestion("run", "form")
	if !q.Interaction.NeedsResponse || q.Interaction.Response != nil || len(st.Messages("chat")) != 0 {
		t.Fatal("failed save left an accepted answer in memory")
	}
	if _, exists := st.GetRun("next"); exists {
		t.Fatal("failed save left a phantom run")
	}
}

func TestCancellationClosesOnlyItsQuestionsAtomicallyAndSurvivesReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, _ := Open(path)
	it := agent.Interaction{ID: "form", Kind: "form", ResponseMode: "message", NeedsResponse: true,
		Questions: []agent.Question{{ID: "q", Title: "Color?", AllowOther: true}}}
	for _, id := range []string{"stopped", "other"} {
		if err := st.SaveRun(Run{ID: id, ChatID: id, Status: "running"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.RecordMessageQuestion(id, id, it); err != nil {
			t.Fatal(err)
		}
	}
	st.path = filepath.Join(t.TempDir(), "missing", "state.json")
	if err := st.SaveRun(Run{ID: "stopped", ChatID: "stopped", Status: "cancelled"}); err == nil {
		t.Fatal("failed save reported a committed cancellation")
	}
	run, _ := st.GetRun("stopped")
	if run.Status != "running" || !st.HasPendingMessageQuestion("stopped") {
		t.Fatal("failed cancellation did not roll back the run and questions together")
	}
	st.path = path
	if err := st.SaveRun(Run{ID: "stopped", ChatID: "stopped", Status: "cancelled"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"form", "late-form"} {
		it.ID = id
		replay, err := st.RecordMessageQuestion("stopped", "stopped", it)
		if err != nil || replay.NeedsResponse || replay.Response != nil {
			t.Fatalf("late native event revived or answered a stopped question: %+v %v", replay, err)
		}
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.HasPendingMessageQuestion("stopped") || !reopened.HasPendingMessageQuestion("other") {
		t.Fatal("cancellation lost its scope after restart")
	}
	ref := InteractionReply{RunID: "stopped", Response: agent.InteractionResponse{InteractionID: "form"}}
	if err := reopened.CommitInteractionReply(ref, "resumed", Message{ID: "answer"}, &Run{ID: "resumed"}); err == nil {
		t.Fatal("cancelled question resumed work")
	}
	if _, exists := reopened.GetRun("resumed"); exists {
		t.Fatal("rejected reply created a run")
	}
	parts := reopened.OverlayInteractions("stopped", []agent.Part{{Type: "interaction", Interaction: &it}})
	if parts[0].Interaction.NeedsResponse || len(parts[0].Interaction.Questions) != 1 {
		t.Fatal("history should retain the question as read-only")
	}
}

func TestOldCancelledQuestionsAreRepairedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, _ := Open(path)
	_ = st.SaveRun(Run{ID: "run", ChatID: "chat", Status: "cancelled"})
	it := agent.Interaction{ID: "form", Kind: "form", RunID: "run", ResponseMode: "message", NeedsResponse: true,
		Questions: []agent.Question{{ID: "q", Title: "Color?", AllowOther: true}}}
	// Reproduce the state written by older daemons, without invoking the fixed writer.
	st.s.Interactions = map[string]MessageQuestion{interactionKey("run", "form"): {ChatID: "chat", RunID: "run", Interaction: it}}
	if err := st.save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	q, ok := reopened.MessageQuestion("run", "form")
	if !ok || q.Interaction.NeedsResponse || q.Interaction.Response != nil {
		t.Fatal("old cancelled prompt was not repaired without inventing an answer")
	}
}

func TestCancelledLegacyQuestionIsNotRecoveredAsPending(t *testing.T) {
	st, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	legacy := &agent.Interaction{ID: "native:question:0", Kind: "info", Title: "Color?", Meta: map[string]any{"asyncQuestion": true}}
	_ = st.AddMessage(Message{ID: "reply", ChatID: "chat", Role: "assistant", Parts: []agent.Part{{Type: "interaction", Interaction: legacy}}})
	_ = st.SaveRun(Run{ID: "run", ChatID: "chat", Status: "cancelled", ReplyID: "reply"})
	form := &agent.Interaction{ID: "native:questions", Kind: "form", RunID: "run", ResponseMode: "message", NeedsResponse: true,
		Questions: []agent.Question{{ID: "q", Title: "Color?", AllowOther: true}}}
	parts := st.OverlayInteractions("chat", []agent.Part{{Type: "interaction", Interaction: form}})
	if parts[0].Interaction.NeedsResponse || st.HasPendingMessageQuestion("run") {
		t.Fatal("legacy migration reopened a stopped question")
	}
}

func TestLatestLegacyQuestionCanBeRecoveredButOldQuestionsDoNotReopen(t *testing.T) {
	for _, latest := range []bool{true, false} {
		path := filepath.Join(t.TempDir(), "state.json")
		st, _ := Open(path)
		legacy := &agent.Interaction{ID: "native:question:0", Kind: "info", Title: "Which color?", Meta: map[string]any{"asyncQuestion": true}}
		_ = st.AddMessage(Message{ID: "reply", ChatID: "chat", Role: "assistant", Parts: []agent.Part{{Type: "interaction", Interaction: legacy}}})
		_ = st.SaveRun(Run{ID: "run", ChatID: "chat", Status: "done", ReplyID: "reply"})
		if !latest {
			_ = st.SaveRun(Run{ID: "later", ChatID: "chat", Status: "done"})
		}
		if id := st.Messages("chat")[0].Parts[0].Interaction.RunID; id != "run" {
			t.Fatalf("lost legacy reply owner: %q", id)
		}
		form := &agent.Interaction{ID: "native:questions", Kind: "form", RunID: "run", NeedsResponse: true, ResponseMode: "message", Questions: []agent.Question{{ID: "q", Title: "Which color?", Options: []agent.Action{{ID: "Blue", Label: "Blue"}}}}}
		parts := st.OverlayInteractions("chat", []agent.Part{{Type: "interaction", Interaction: form}})
		if parts[0].Interaction.NeedsResponse != latest {
			t.Fatalf("latest=%v recovered=%+v", latest, parts[0].Interaction)
		}
		reopened, _ := Open(path)
		q, ok := reopened.MessageQuestion("run", "native:questions")
		if ok != latest {
			t.Fatalf("latest=%v persisted=%v", latest, ok)
		}
		if latest && (!q.Interaction.NeedsResponse || !reopened.Messages("chat")[0].Parts[0].Interaction.IsMessageQuestion()) {
			t.Fatal("legacy form recovery did not persist")
		}
	}
}
