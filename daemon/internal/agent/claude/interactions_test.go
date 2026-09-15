package claude

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestQuestionHistoryHasOneReadOnlyFormWithoutOrphanResult(t *testing.T) {
	raw, err := os.ReadFile("testdata/questions.request.json")
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Request struct {
			Input json.RawMessage `json:"input"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	message, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "question", "name": "AskUserQuestion", "input": request.Request.Input}}})
	m := agent.Message{Role: "assistant", Parts: messageParts(message)}
	mergeToolResults(&m, []agent.Part{{Type: "tool", Tool: &agent.ToolPart{ID: "question", Output: "User answered the questions"}}})
	if len(m.Parts) != 1 || m.Parts[0].Interaction == nil || m.Parts[0].Interaction.NeedsResponse || len(m.Parts[0].Interaction.Questions) != 2 {
		t.Fatalf("history duplicated or reactivated a question: %+v", m.Parts)
	}
}

func TestCapturedClaudeQuestionUsesControlResponseForAllAnswers(t *testing.T) {
	raw, err := os.ReadFile("testdata/questions.request.json")
	if err != nil {
		t.Fatal(err)
	}
	var request streamEnvelope
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	it := approvalInteraction(request.RequestID, request.Request)
	if it == nil || it.Kind != "form" || len(it.Questions) != 2 || !it.Questions[1].MultiSelect {
		t.Fatalf("lost form: %+v", it)
	}
	answers := map[string]agent.QuestionAnswer{}
	for _, q := range it.Questions {
		if q.Options[0].Description == "" {
			t.Fatal("lost descriptions")
		}
		answer := agent.QuestionAnswer{Options: []string{q.Options[0].ID}, Text: "Demo only"}
		if q.MultiSelect {
			answer.Options = append(answer.Options, q.Options[1].ID)
		}
		answers[q.ID] = answer
	}
	meta := map[string]string{}
	for key, value := range it.Meta {
		if text, ok := value.(string); ok {
			meta[key] = text
		}
	}
	encoded, ok := encodeInbound(agent.Inbound{Kind: "response", InteractionID: it.ID, Answers: answers, Interaction: it, Meta: meta})
	if !ok {
		t.Fatal("no native response")
	}
	var response struct {
		Type     string `json:"type"`
		Response struct {
			ID       string `json:"request_id"`
			Response struct {
				Behavior string `json:"behavior"`
				Input    struct {
					Questions []any             `json:"questions"`
					Answers   map[string]string `json:"answers"`
				} `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(encoded, &response) != nil || response.Type != "control_response" || response.Response.ID != request.RequestID || response.Response.Response.Behavior != "allow" || len(response.Response.Response.Input.Questions) != 2 {
		t.Fatalf("wrong control reply: %s", encoded)
	}
	for _, q := range it.Questions {
		value := response.Response.Response.Input.Answers[q.Title]
		if !strings.Contains(value, "Demo only") || !strings.Contains(value, q.Options[0].Label) {
			t.Fatalf("lost answer: %s", encoded)
		}
		if q.MultiSelect && !strings.Contains(value, q.Options[1].Label) {
			t.Fatalf("lost multi-select: %s", encoded)
		}
	}
}

func TestClaudePermissionSuggestionsAndModeRejection(t *testing.T) {
	it := approvalInteraction("approval", json.RawMessage(`{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"npm test"},"permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test"}],"behavior":"allow","destination":"session"}]}`))
	meta := map[string]string{}
	for k, v := range it.Meta {
		if value, ok := v.(string); ok {
			meta[k] = value
		}
	}
	encoded, _ := encodePermission(agent.Inbound{InteractionID: it.ID, Decision: "allow_suggestion_0", Meta: meta})
	if !strings.Contains(string(encoded), `"updatedPermissions":[{`) || !strings.Contains(string(encoded), `"command":"npm test"`) {
		t.Fatal(string(encoded))
	}
	s := newControlSession()
	ack := make(chan error, 1)
	line, _ := s.encode(agent.Inbound{Kind: "set_permission_mode", Text: "auto", Ack: ack})
	var request struct {
		ID string `json:"request_id"`
	}
	_ = json.Unmarshal(line, &request)
	body, _ := json.Marshal(map[string]any{"request_id": request.ID, "subtype": "error", "error": "Auto mode unavailable"})
	s.response(body, func(agent.Event) {})
	if err := <-ack; err == nil || err.Error() != "Auto mode unavailable" {
		t.Fatalf("rejected mode reported as applied: %v", err)
	}
}
