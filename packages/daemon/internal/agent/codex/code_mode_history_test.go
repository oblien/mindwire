package codex

import (
	"strings"
	"testing"
)

func TestCodeModeHistoryKeepsDiscoveryAndInnerMCPWithoutDuplicateMessages(t *testing.T) {
	for _, completedUser := range []bool{false, true} {
		lines := []string{`{"type":"event_msg","payload":{"type":"user_message","message":"Connect Slack"}}`}
		if completedUser {
			lines = append(lines, `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u","content":[{"type":"text","text":"Connect Slack"}]}}}`)
		}
		lines = append(lines,
			`{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec","call_id":"discover","input":"text(ALL_TOOLS.filter(t => /chat/.test(t.name)));"}}`,
			`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"discover","output":[{"type":"input_text","text":"Found chat tools"}]}}`,
			`{"type":"response_item","payload":{"type":"custom_tool_call","name":"functions.exec","call_id":"invoke","input":"text(await tools.mcp__openship__get_chat_apps({}));"}}`,
			`{"type":"response_item","payload":{"type":"function_call","name":"mcp__openship__get_chat_apps","call_id":"inner","arguments":"{}"}}`,
			`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","id":"inner","server":"openship","tool":"get_chat_apps","arguments":{},"status":"completed","result":{"content":[{"type":"text","text":"No connections"}]}}}}`,
			`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"invoke","output":[{"type":"input_text","text":"No connections"}]}}`,
			`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Ready to connect."}]}}`,
			`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"a","content":[{"type":"output_text","text":"Ready to connect."}]}}}`,
		)
		messages, err := parseRollout(strings.NewReader(strings.Join(lines, "\n")), "chat")
		if err != nil || len(messages) != 2 {
			t.Fatalf("messages: %+v, error: %v", messages, err)
		}
		if messages[1].Text != "Ready to connect." {
			t.Fatalf("duplicated or lost reply: %q", messages[1].Text)
		}
		calls := map[string]int{}
		for _, p := range messages[1].Parts {
			if p.Tool == nil {
				continue
			}
			calls[p.Tool.ID]++
			if p.Tool.Status != "completed" || p.Tool.Output == "" {
				t.Fatalf("lost output for %s: %+v", p.Tool.ID, p.Tool)
			}
		}
		for _, id := range []string{"discover", "invoke", "inner"} {
			if calls[id] != 1 {
				t.Fatalf("call %s appeared %d times: %+v", id, calls[id], messages[1].Parts)
			}
		}
	}
}
