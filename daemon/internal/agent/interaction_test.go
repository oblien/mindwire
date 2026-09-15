package agent

import "testing"

func TestInteractionRequiresCompleteAndOfferedAnswers(t *testing.T) {
	it := Interaction{Kind: "form", Questions: []Question{
		{ID: "theme", Options: []Action{{ID: "dark", Label: "Dark"}}, AllowOther: true},
		{ID: "features", MultiSelect: true, Options: []Action{{ID: "search", Label: "Search"}, {ID: "export", Label: "Export"}}},
	}}
	for _, answers := range []map[string]QuestionAnswer{
		{"theme": {Options: []string{"dark"}}},
		{"theme": {Options: []string{"unknown"}}, "features": {Options: []string{"search"}}},
		{"theme": {Options: []string{"dark", "dark"}}, "features": {Options: []string{"search"}}},
		{"theme": {Text: "system"}, "features": {Options: []string{"search"}}, "extra": {Text: "ignored?"}},
	} {
		if _, err := it.ValidateResponse(InteractionResponse{Answers: answers}); err == nil {
			t.Fatalf("accepted incomplete/invalid form: %+v", answers)
		}
	}
	reply, err := it.ValidateResponse(InteractionResponse{Answers: map[string]QuestionAnswer{
		"theme":    {Options: []string{"dark"}, Text: "Follow the system at night"},
		"features": {Options: []string{"search", "export"}},
	}})
	if err != nil || len(reply.Answers["features"].Options) != 2 || reply.Answers["theme"].Text == "" {
		t.Fatalf("lost selections/feedback: %+v %v", reply, err)
	}
}

func TestApprovalCannotDefaultToAllow(t *testing.T) {
	it := Interaction{Kind: "approval", Options: []Action{{ID: "allow"}, {ID: "deny"}}}
	for _, decision := range []string{"", "allow_all", "accept", "typo"} {
		if _, err := it.ValidateResponse(InteractionResponse{Decision: decision}); err == nil {
			t.Fatalf("accepted unoffered approval %q", decision)
		}
	}
	if _, err := it.ValidateResponse(InteractionResponse{Decision: "deny", Text: "Use a read-only command"}); err != nil {
		t.Fatal(err)
	}
}

func TestSingleQuestionAcceptsLegacyLabelWithoutDuplicatingFeedback(t *testing.T) {
	it := Interaction{Kind: "choice", Questions: []Question{{ID: "theme", Options: []Action{{ID: "dark", Label: "Dark"}}}}}
	reply, err := it.ValidateResponse(InteractionResponse{Text: "Dark"})
	if err != nil || len(reply.Answers["theme"].Options) != 1 || reply.Answers["theme"].Options[0] != "dark" || reply.Answers["theme"].Text != "" {
		t.Fatalf("legacy choice: %+v %v", reply, err)
	}
}
