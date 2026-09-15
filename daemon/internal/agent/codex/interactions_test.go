package codex

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestCapturedCodexQuestionFormRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/questions.request.json")
	if err != nil {
		t.Fatal(err)
	}
	var request rpcIn
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	prompts := serverRequestPrompts(request)
	if len(prompts) != 1 || len(prompts[0].interaction.Questions) != 2 {
		t.Fatalf("lost grouped questions: %+v", prompts)
	}
	it := prompts[0].interaction
	answers := map[string]agent.QuestionAnswer{}
	for _, q := range it.Questions {
		if q.Options[0].Description == "" || !q.AllowOther {
			t.Fatalf("lost option semantics: %+v", q)
		}
		answers[q.ID] = agent.QuestionAnswer{Options: []string{q.Options[0].ID}, Text: "Keep the demo small"}
	}
	reply, _ := json.Marshal(decisionResult(prompts[0], agent.Inbound{Answers: answers}))
	var response struct {
		Answers map[string]struct {
			Answers []string `json:"answers"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(reply, &response); err != nil {
		t.Fatal(err)
	}
	for id, answer := range answers {
		if !reflect.DeepEqual(response.Answers[id].Answers, []string{answer.Options[0], answer.Text}) {
			t.Fatalf("native answer lost feedback: %s", reply)
		}
	}
}

func TestCodexApprovalChoicesKeepNativeScopeAndRules(t *testing.T) {
	request := rpcIn{ID: json.RawMessage(`"approval"`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"command":"npm test","availableDecisions":["accept","acceptForSession",{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["npm","test"]}},"decline","cancel"]}`)}
	it, ap := serverRequestInteraction(request)
	if len(it.Options) != 5 || it.Options[2].Description != "npm test" {
		t.Fatalf("approval options: %+v", it)
	}
	for decision, want := range map[string]string{"allow": "accept", "allow_session": "acceptForSession", "deny": "decline", "cancel": "cancel"} {
		got := decisionResult(ap, agent.Inbound{Decision: decision}).(map[string]any)["decision"]
		if got != want {
			t.Fatalf("%s encoded as %v", decision, got)
		}
	}
	encoded, _ := json.Marshal(decisionResult(ap, agent.Inbound{Decision: "allow_rule"}))
	if string(encoded) != `{"decision":{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["npm","test"]}}}` {
		t.Fatal(string(encoded))
	}
	request.Params = json.RawMessage(`{"command":"npm test","availableDecisions":["decline","cancel"]}`)
	it, _ = serverRequestInteraction(request)
	if len(it.Options) != 2 || it.Options[0].ID != "deny" {
		t.Fatal("invented an approval the harness did not offer")
	}
}

func TestCodexUnknownApprovalFailsClosed(t *testing.T) {
	for _, family := range []string{famV2Decision, famReviewDecision, famPermissions} {
		encoded, _ := json.Marshal(decisionResult(approval{family: family, permissions: json.RawMessage(`{"network":{"enabled":true}}`)}, agent.Inbound{Decision: "alow"}))
		var value map[string]any
		_ = json.Unmarshal(encoded, &value)
		if value["decision"] == "accept" || value["decision"] == "approved" {
			t.Fatalf("typo granted permission: %s", encoded)
		}
		if permissions, ok := value["permissions"].(map[string]any); ok && len(permissions) > 0 {
			t.Fatalf("typo granted access: %s", encoded)
		}
	}
}

func TestNativeSettingEnumDoesNotLeakObjectPropertyChoices(t *testing.T) {
	got := stringEnum(json.RawMessage(`{"oneOf":[{"type":"string","enum":["untrusted","on-request","never"]},{"type":"object","properties":{"mode":{"enum":["unexpected"]}}}]}`))
	if !reflect.DeepEqual(got, []string{"untrusted", "on-request", "never"}) {
		t.Fatalf("choices: %v", got)
	}
}
