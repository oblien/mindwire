package grok

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

func TestNativeQuestionAnswerRetainsLabelsAndNotes(t *testing.T) {
	req := acp.Message{ID: json.RawMessage(`"request"`), Method: "_x.ai/ask_user_question", Params: json.RawMessage(`{"params":{"questions":[{"id":"scope","question":"Which scope?","options":[{"id":"small","label":"Small change"}]}]}}`)}
	it := grokInteraction(req)
	if it == nil || it.Questions[0].ID != "scope" {
		t.Fatal("lost native question")
	}
	response, err := grokReply(req, agent.Inbound{Answers: map[string]agent.QuestionAnswer{"scope": {Options: []string{"small"}, Text: "Keep the public API"}}})
	raw, _ := json.Marshal(response)
	if err != nil || !strings.Contains(string(raw), `"Which scope?":["Small change"]`) || !strings.Contains(string(raw), `"notes":"Keep the public API"`) {
		t.Fatalf("native answer: %s %v", raw, err)
	}
}

func TestNativePromptCompletionsAndBackgroundIdentity(t *testing.T) {
	for _, method := range []string{"x.ai/session/prompt_complete", "_x.ai/session/prompt_complete"} {
		id, raw := grokCompletion(acp.Message{Method: method, Params: json.RawMessage(`{"sessionId":"s","promptId":"p"}`)})
		if id != "p" || !strings.Contains(string(raw), "end_turn") {
			t.Fatalf("completion %s %s", id, raw)
		}
	}
	id, raw := grokCompletion(acp.Message{Method: "_x.ai/session/update", Params: json.RawMessage(`{"sessionId":"s","update":{"sessionUpdate":"turn_completed","prompt_id":"p","stop_reason":"rate_limit"}}`)})
	if id != "p" || !strings.Contains(string(raw), "rate_limit") {
		t.Fatal("lost native failure")
	}
	id, _ = grokCompletion(acp.Message{Method: "_x.ai/session/update", Params: json.RawMessage(`{"sessionId":"s","update":{"sessionUpdate":"turn_completed","prompt_id":"task-completed-job","stop_reason":"end_turn"}}`)})
	if id != "" {
		t.Fatal("background wake completed foreground")
	}
	task := grokTask(acp.Message{Method: "_x.ai/task_completed", Params: json.RawMessage(`{"sessionId":"s","update":{"task_snapshot":{"task_id":"job","exit_code":1,"output":"build failed"}}}`)}, "s")
	if task == nil || task.ID != "job" || task.Status != "failed" {
		t.Fatalf("task %+v", task)
	}
}

func TestSupervisedACPOverridesStoredPermissionDefaults(t *testing.T) {
	args := strings.Join(acpCommand(agent.TurnInput{Config: map[string]string{keyPermission: "ask"}}), " ")
	if !strings.Contains(args, "--permission-mode default agent") || strings.Contains(args, "--always-approve") {
		t.Fatalf("unsafe supervised command %s", args)
	}
}
