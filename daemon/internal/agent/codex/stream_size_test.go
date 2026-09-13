package codex

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestLargeCodexOutputsSurviveBothStreamsAndHistory(t *testing.T) {
	for _, size := range []int{70 << 10, 1 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			output := strings.Repeat("x", size)
			text, _ := json.Marshal(output)
			events, result, got := feed(t,
				`{"type":"item.completed","item":{"id":"command","type":"command_execution","command":"cat output.txt","aggregated_output":`+string(text)+`,"exit_code":0,"status":"completed"}}`,
				`{"type":"item.completed","item":{"id":"answer","type":"agent_message","text":`+string(text)+`}}`,
				`{"type":"turn.completed"}`)
			if !got || result.IsError || len(result.Text) != size || len(firstOf(events, agent.EventToolResult).Tool.Output) != size {
				t.Fatal("large exec output was truncated")
			}
			_, result, got = protocolTurn(t, `{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","items":[{"id":"answer","type":"agentMessage","text":`+string(text)+`}]}}}`)
			if !got || result.IsError || len(result.Text) != size {
				t.Fatal("large app-server output was truncated")
			}
			messages, err := parseRollout(strings.NewReader(`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"answer","content":[{"type":"Text","text":`+string(text)+`}]}}}`), "chat")
			if err != nil || len(messages) != 1 || len(messages[0].Text) != size {
				t.Fatalf("large native history was truncated: %v", err)
			}
		})
	}
}

func TestOversizedCodexFrameFailsExplicitly(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"answer","type":"agent_message","text":"` + strings.Repeat("x", 16<<20) + `"}}`
	result, got := parseStream(strings.NewReader(line), func(agent.Event) {})
	if got || !result.IsError || !strings.Contains(result.Text, "stream read error") {
		t.Fatalf("oversized frame became a silent success: complete=%v error=%v", got, result.IsError)
	}
	if _, err := parseRollout(strings.NewReader(line), "chat"); err == nil {
		t.Fatal("oversized history must return an error so recorded history remains available")
	}
}
