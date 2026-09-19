package codex

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Codex CLI 0.155.0: only the random native item ID is replaced in this capture.
func TestCapturedAsyncFormMatchesLiveAndLegacyRolloutWithoutDuplicateText(t *testing.T) {
	raw, err := os.ReadFile("testdata/questions.async.json")
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		ID        string          `json:"id"`
		Questions []asyncQuestion `json:"questions"`
	}
	if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatal(err)
	}
	var events []agent.Event
	state := newStreamState()
	for _, phase := range []itemPhase{phaseStarted, phaseCompleted, phaseCompleted} {
		emitItem(phase, raw, func(e agent.Event) { events = append(events, e) }, state)
	}
	if len(events) != 1 || events[0].Interaction == nil || !events[0].Interaction.IsMessageQuestion() {
		t.Fatalf("duplicate raw text/form: %+v", events)
	}
	form := events[0].Interaction
	if len(form.Questions) != 2 || len(form.Questions[0].Options) != 2 || *form.Blocking {
		t.Fatalf("lost native questions: %+v", form)
	}
	args, _ := json.Marshal(map[string]any{"questions": captured.Questions})
	line, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{
		"type": "function_call", "name": "functions.request_user_input_async", "call_id": captured.ID, "arguments": string(args),
	}})
	history, err := parseRollout(strings.NewReader(string(line)), "chat")
	if err != nil || len(history) != 1 || len(history[0].Parts) != 1 || !reflect.DeepEqual(history[0].Parts[0].Interaction, form) {
		t.Fatalf("history/live mismatch: %+v %v", history, err)
	}
}

func TestLiveAsyncAnswerDoesNotDuplicateTheTurnPrefixInHistory(t *testing.T) {
	question := asyncQuestionInteraction("native-call", []asyncQuestion{{Title: "Which color?", Options: []string{"Blue", "Green"}}})
	prefix := []agent.Part{
		{Type: "text", ID: "native-intro", Text: "I will ask now."},
		{Type: "interaction", Interaction: question},
		{Type: "thinking", ID: "native-reasoning", Text: "Comparing formats"},
		{Type: "text", ID: "native-progress", Text: "Markdown supports formatting."},
	}
	answer := agent.Part{Type: "text", ID: "native-answer", Text: "You selected Green."}
	user := agent.Message{ID: "user", Role: "user", Text: "Ask a question"}
	steer := agent.Message{ID: "steer", Role: "user", Text: "Which color?\nGreen"}
	native := []agent.Message{user, {ID: "before", Role: "assistant", Parts: prefix}, steer, {ID: "after", Role: "assistant", Parts: []agent.Part{answer}}}
	recorded := []agent.Message{user, steer, {ID: "recorded", Role: "assistant", Parts: append(append([]agent.Part{}, prefix...), answer)}}
	merged := mergeRecordedHistory(native, recorded)
	if len(merged) != 4 || len(merged[1].Parts) != len(prefix) || len(merged[3].Parts) != 1 || merged[3].Parts[0].Text != answer.Text {
		t.Fatalf("steering duplicated output from before the answer: %+v", merged)
	}
	if again := mergeRecordedHistory(merged, recorded); !reflect.DeepEqual(again, merged) {
		t.Fatal("reopening changed the reconciled transcript")
	}
	if len(recorded[2].Parts) != len(prefix)+1 {
		t.Fatal("merge mutated the saved run snapshot")
	}
}

func TestLegacyQuestionNoticesUpgradeFromNativeOptionsWithoutDuplicateProse(t *testing.T) {
	form := asyncQuestionInteraction("native", []asyncQuestion{{Title: "Which color?", Options: []string{"Blue", "Green"}}, {Title: "Which format?", Options: []string{"Text", "Markdown"}}})
	user := agent.Message{ID: "user", Role: "user", Text: "Ask"}
	native := []agent.Message{user, {ID: "reply", Role: "assistant", Parts: []agent.Part{{Type: "interaction", Interaction: form}}}}
	var legacy []agent.Part
	legacy = append(legacy, agent.Part{Type: "text", ID: "native", Text: "Which color? Blue or Green; which format? Text or Markdown"})
	for i, q := range form.Questions {
		id := "native:question:0"
		if i == 1 {
			id = "native:question:1"
		}
		legacy = append(legacy, agent.Part{Type: "interaction", Interaction: &agent.Interaction{ID: id, Kind: "info", Title: q.Title, RunID: "recorded-run", Meta: map[string]any{"asyncQuestion": true}}})
	}
	recorded := []agent.Message{user, {ID: "recorded", Role: "assistant", Parts: legacy}}
	merged := mergeRecordedHistory(native, recorded)
	if len(merged) != 2 || len(merged[1].Parts) != 1 || merged[1].Parts[0].Interaction.RunID != "recorded-run" || len(merged[1].Parts[0].Interaction.Questions) != 2 {
		t.Fatalf("legacy rows duplicated or lost native choices: %+v", merged)
	}
}
