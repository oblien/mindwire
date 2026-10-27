package cursor

import (
	"encoding/json"
	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
	"strings"
	"testing"
)

func TestNativeQuestionAndPlanResponses(t *testing.T) {
	m := acp.Message{ID: json.RawMessage(`"ask-1"`), Method: "_cursor/ask_question", Params: json.RawMessage(`{"title":"Choose","questions":[{"id":"q-17","prompt":"Database?","options":[{"id":"pg-native","label":"Postgres"}],"allowMultiple":false}]}`)}
	it := interaction(m)
	if it == nil || it.ID != `"ask-1"` || it.Questions[0].ID != "q-17" || it.Questions[0].AllowOther {
		t.Fatalf("question: %+v", it)
	}
	result, err := reply(m, agent.Inbound{Answers: map[string]agent.QuestionAnswer{"q-17": {Options: []string{"pg-native"}}}})
	data, _ := json.Marshal(result)
	if err != nil || !strings.Contains(string(data), `"selectedOptionIds":["pg-native"]`) || !strings.Contains(string(data), `"outcome":"answered"`) {
		t.Fatalf("question response %s %v", data, err)
	}
	if _, err := reply(m, agent.Inbound{Answers: map[string]agent.QuestionAnswer{"q-17": {Options: []string{"Postgres"}}}}); err == nil {
		t.Fatal("label mistaken for native option ID")
	}
	m.Method = "cursor/create_plan"
	m.Params = json.RawMessage(`{"name":"Migration","overview":"Upgrade","plan":"1. Back up\n2. Migrate"}`)
	if it := interaction(m); it == nil || it.Kind != "plan" || !strings.Contains(it.Detail, "Back up") {
		t.Fatal("plan lost")
	}
	result, err = reply(m, agent.Inbound{Decision: "reject", Text: "Keep the old schema"})
	data, _ = json.Marshal(result)
	if err != nil || !strings.Contains(string(data), `"outcome":"rejected"`) {
		t.Fatalf("plan rejection: %s %v", data, err)
	}
}
