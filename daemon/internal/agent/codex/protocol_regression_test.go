package codex

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestExecItemErrorMessageIsDurableFeedback(t *testing.T) {
	for _, recover := range []bool{false, true} {
		lines := []string{`{"type":"item.completed","item":{"id":"problem","type":"error","message":"Search failed: permission denied."}}`}
		if recover {
			lines = append(lines, `{"type":"item.completed","item":{"id":"answer","type":"agent_message","text":"Used another source."}}`)
		}
		lines = append(lines, `{"type":"turn.completed"}`)
		events, result, got := feed(t, lines...)
		if !got || result.IsError == recover {
			t.Fatalf("result=%+v, recovered=%v", result, recover)
		}
		problem := firstOf(events, agent.EventInteraction)
		if problem == nil || problem.Interaction.Kind != "error" || problem.Interaction.Detail != "Search failed: permission denied." {
			t.Fatalf("item message lost: %+v", events)
		}
	}
}

func TestMalformedAndEmptyTurnsCannotSucceedSilently(t *testing.T) {
	for _, frame := range []string{
		`{"method":"item/agentMessage/delta","params":`,
		`{"method":"item/completed","params":{"item":{"id":42,"type":"agentMessage","text":"Broken item"}}}`,
		`{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","items":[]}}}`,
	} {
		_, result, _ := protocolTurn(t, frame)
		if !result.IsError || result.Text == "" {
			t.Fatalf("silent app-server failure for %s: %+v", frame, result)
		}
	}
	for _, frame := range []string{
		`{"type":"item.completed","item":`,
		`{"type":"item.completed","item":{"id":42,"type":"agent_message"}}`,
		`{"type":"turn.completed"}`,
	} {
		_, result, _ := feed(t, frame)
		if !result.IsError || result.Text == "" {
			t.Fatalf("silent exec failure for %s: %+v", frame, result)
		}
	}
}

func TestCompactionNotificationPairsAndFinalSnapshots(t *testing.T) {
	modern := `{"method":"item/completed","params":{"item":{"id":"compact","type":"contextCompaction"}}}`
	legacy := `{"method":"thread/compacted","params":{"threadId":"thread-1","turnId":"turn-1"}}`
	terminal := `{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","items":[{"id":"compact","type":"contextCompaction"},{"id":"answer","type":"agentMessage","text":"Continued."}]}}}`
	for _, frames := range [][]string{{terminal}, {modern, legacy, terminal}, {legacy, modern, terminal}, {modern, modern, terminal}} {
		events, result, got := protocolTurn(t, frames...)
		if !got || result.IsError || countType(events, agent.EventCompaction) != 1 {
			t.Fatalf("compaction duplicated/lost: %+v, %+v", result, events)
		}
	}
}

func TestToolOutputUsesFinalSnapshotsAndKeepsOmittedOutput(t *testing.T) {
	col, state := &collector{}, newStreamState()
	emitItem(phaseStarted, json.RawMessage(`{"id":"command","type":"commandExecution","command":"go test","status":"inProgress"}`), col.emit, state)
	state.emitDelta("item/commandExecution/outputDelta", json.RawMessage(`{"itemId":"command","delta":"Running tests"}`), col.emit)
	emitItem(phaseCompleted, json.RawMessage(`{"id":"command","type":"commandExecution","command":"go test","status":"completed","aggregatedOutput":null}`), col.emit, state)
	if result := firstOf(col.snapshot(), agent.EventToolResult); result == nil || result.Tool.Output != "Running tests" {
		t.Fatal("null final output erased streamed stdout")
	}
	final := json.RawMessage(`{"id":"command","type":"commandExecution","command":"go test","status":"failed","aggregatedOutput":"FAIL","exitCode":1}`)
	emitItem(phaseCompleted, final, col.emit, state)
	emitItem(phaseCompleted, final, col.emit, state)
	var results []agent.ToolEvent
	for _, event := range col.snapshot() {
		if event.Type == agent.EventToolResult {
			results = append(results, *event.Tool)
		}
	}
	if len(results) != 2 || results[1].Output != "FAIL" || !results[1].IsError || results[1].Action.Shell.Stdout != "FAIL" {
		t.Fatalf("tool correction lost: %+v", results)
	}
	count := len(col.snapshot())
	state.emitDelta("item/commandExecution/outputDelta", json.RawMessage(`{"itemId":"command","delta":"late"}`), col.emit)
	if len(col.snapshot()) != count {
		t.Fatal("late delta changed a completed tool")
	}

	state.emitDelta("item/fileChange/outputDelta", json.RawMessage(`{"itemId":"patch","delta":"Patch could not be applied."}`), col.emit)
	emitItem(phaseCompleted, json.RawMessage(`{"id":"patch","type":"fileChange","status":"failed","changes":[]}`), col.emit, state)
	last := col.snapshot()[len(col.snapshot())-1]
	if last.Tool == nil || !last.Tool.IsError || last.Tool.Output != "Patch could not be applied." {
		t.Fatalf("legacy patch failure lost: %+v", last)
	}
}

func TestWholeFileChangesUseSharedDiffFormat(t *testing.T) {
	for _, kind := range []string{"add", "delete"} {
		content := "@@ This is file content, not a patch.\n+Keep this literal plus.\n"
		raw, _ := json.Marshal(map[string]any{"id": "file", "type": "fileChange", "status": "completed",
			"changes": []any{map[string]any{"path": "notes.txt", "kind": map[string]any{"type": kind}, "diff": content}}})
		events := &collector{}
		emitItem(phaseCompleted, raw, events.emit, newStreamState())
		file := firstOf(events.snapshot(), agent.EventToolResult).Tool.Action.Files[0]
		if kind == "add" && (file.NewText != content || !strings.Contains(file.Diff, "+@@ This is file content")) {
			t.Fatalf("new file displayed as patch context: %+v", file)
		}
		if kind == "delete" && (file.OldText != content || !strings.Contains(file.Diff, "-@@ This is file content")) {
			t.Fatalf("deleted file displayed as patch context: %+v", file)
		}
	}
}

func TestReasoningSummariesReplaceRawPreviewsAndAsyncQuestionsStayStructured(t *testing.T) {
	col, state := &collector{}, newStreamState()
	state.emitDelta("item/reasoning/textDelta", json.RawMessage(`{"itemId":"reason","contentIndex":0,"delta":"Checking the source."}`), col.emit)
	state.emitDelta("item/reasoning/summaryPartAdded", json.RawMessage(`{"itemId":"reason","summaryIndex":0}`), col.emit)
	state.emitDelta("item/reasoning/summaryTextDelta", json.RawMessage(`{"itemId":"reason","summaryIndex":0,"delta":"Reviewed the parser."}`), col.emit)
	emitItem(phaseCompleted, json.RawMessage(`{"id":"reason","type":"reasoning","summary":["Reviewed the parser."],"content":["Checking the source."]}`), col.emit, state)
	var text string
	for _, event := range col.snapshot() {
		if event.Type == agent.EventThinking {
			if event.Delta {
				text += event.Text
			} else {
				text = event.Text
			}
		}
	}
	if text != "Reviewed the parser." {
		t.Fatalf("reasoning preview was duplicated: %q", text)
	}
	question := json.RawMessage(`{"id":"questions","type":"agentMessage","text":"Choose a format.","delivery":"async","questions":[{"title":"Which format?","options":["PDF","Text"]}]}`)
	emitItem(phaseCompleted, question, col.emit, state)
	emitItem(phaseCompleted, question, col.emit, state)
	inter := firstOf(col.snapshot(), agent.EventInteraction)
	if inter == nil || inter.Interaction.Title != "Which format?" || !inter.Interaction.IsMessageQuestion() || !inter.Interaction.NeedsResponse || inter.Interaction.Blocking == nil || *inter.Interaction.Blocking || countType(col.snapshot(), agent.EventInteraction) != 1 {
		t.Fatalf("async question lost choices, was duplicated or became blocking: %+v", inter)
	}
	if questions := inter.Interaction.Questions; len(questions) != 1 || len(questions[0].Options) != 2 || questions[0].Options[1].Label != "Text" || !questions[0].AllowOther {
		t.Fatalf("lost native choices: %+v", questions)
	}
	if countType(col.snapshot(), agent.EventText) != 0 {
		t.Fatal("question text duplicated the structured form")
	}
}

func TestNativeErrorAliasesAndMCPContent(t *testing.T) {
	message := errorText(json.RawMessage(`{"message":"Request failed","additional_details":"Deployment missing","codex_error_info":{"http_connection_failed":{"http_status_code":404}},"misalignment":{"detailed_explanation":"Public explanation"}}`), "")
	for _, want := range []string{"Request failed", "Deployment missing", "HTTP 404", "Public explanation"} {
		if !strings.Contains(message, want) {
			t.Errorf("message=%q missing %q", message, want)
		}
	}
	if got := errorText(json.RawMessage(`{"error":{"error":"Expired key"}}`), "fallback"); got != "Expired key" {
		t.Errorf("nested error=%q", got)
	}
	if got := errorText(json.RawMessage(`{"codexErrorInfo":"unauthorized"}`), "fallback"); got != "Authentication failed." {
		t.Errorf("error code fallback=%q", got)
	}
	output, failed := renderOutput(json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"Lookup failed"},{"type":"resource","resource":{"uri":"resource://log","text":"More details"}},{"type":"resource","resource":{"uri":"resource://image","blob":"OPAQUE_IMAGE"}},{"type":"resource_link","name":"Documentation","uri":"https://learn.chatgpt.com/docs/app-server"}],"structuredContent":{"code":"denied"}}`))
	for _, want := range []string{"Lookup failed", "More details", "resource://image", "Documentation", `"code":"denied"`} {
		if !strings.Contains(output, want) {
			t.Errorf("MCP output=%q missing %q", output, want)
		}
	}
	if !failed || strings.Contains(output, "OPAQUE_IMAGE") {
		t.Fatalf("MCP error/binary handling=%q, failed=%v", output, failed)
	}
}

func TestSameThreadOtherTurnCannotFinishTheRequestedTurn(t *testing.T) {
	_, result, got := protocolTurn(t,
		`{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"other-turn","status":"failed","error":{"message":"Unrelated failure"},"items":[]}}}`,
		`{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[{"id":"answer","type":"agentMessage","text":"Requested turn complete."}]}}}`)
	if !got || result.IsError || result.Text != "Requested turn complete." {
		t.Fatalf("wrong turn won: %+v", result)
	}
}

func TestEarlyInputWaitsForTurnAndReportsRejectedSteering(t *testing.T) {
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()
	t.Cleanup(func() { clientR.Close(); clientW.Close(); serverR.Close(); serverW.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	inbound := make(chan agent.Inbound)
	go func() {
		defer serverW.Close()
		decode, encode := json.NewDecoder(clientR), json.NewEncoder(serverW)
		var request rpcIn
		for _, body := range []string{`{}`, ``, `{"thread":{"id":"thread-1"}}`, `{"turn":{"id":"turn-1","status":"inProgress","items":[]}}`} {
			if decode.Decode(&request) != nil {
				return
			}
			// Unbuffered send proves ingress has received the message before the turn ACK.
			if request.Method == "turn/start" {
				inbound <- agent.Inbound{Kind: "input", Text: "More context"}
			}
			if body != "" {
				_ = encode.Encode(map[string]any{"id": request.ID, "result": json.RawMessage(body)})
			}
		}
		if decode.Decode(&request) != nil {
			return
		}
		if request.Method != "turn/steer" {
			return
		}
		_ = encode.Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": -32000, "message": "This review cannot accept input.", "data": map[string]any{"codexErrorInfo": map[string]any{"activeTurnNotSteerable": map[string]any{"turnKind": "review"}}}}})
		_, _ = io.WriteString(serverW, `{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","items":[{"id":"answer","type":"agentMessage","text":"Review finished."}]}}}`+"\n")
	}()
	col := &collector{}
	result, got := (appServer{message: "Review", approval: "never"}).converse(ctx, clientW, serverR, inbound, col.emit)
	problem := firstOf(col.snapshot(), agent.EventInteraction)
	if !got || result.IsError || problem == nil || problem.Interaction.Kind != "error" || !strings.Contains(problem.Interaction.Detail, "cannot accept input") {
		t.Fatalf("rejected steer disappeared: result=%+v got=%v events=%+v", result, got, col.snapshot())
	}
}

func TestAuthoritativeEmptyAnswerDoesNotRestoreDraft(t *testing.T) {
	events, result, got := protocolTurn(t,
		`{"method":"item/agentMessage/delta","params":{"itemId":"answer","delta":"Draft answer"}}`,
		`{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","items":[{"id":"answer","type":"agentMessage","text":"","phase":"final_answer"}]}}}`)
	if !got || !result.IsError || strings.Contains(result.Text, "Draft answer") {
		t.Fatalf("discarded draft became final: %+v", result)
	}
	var text string
	for _, event := range events {
		if event.Type == agent.EventText {
			if event.Delta {
				text += event.Text
			} else {
				text = event.Text
			}
		}
	}
	if text != "" {
		t.Fatalf("draft not cleared: %q", text)
	}
	_, execResult, execGot := feed(t,
		`{"type":"item.updated","item":{"id":"answer","type":"agent_message","text":"Draft answer"}}`,
		`{"type":"item.completed","item":{"id":"answer","type":"agent_message","text":""}}`,
		`{"type":"turn.completed"}`)
	if !execGot || !execResult.IsError || strings.Contains(execResult.Text, "Draft answer") {
		t.Fatalf("exec restored its draft: %+v", execResult)
	}
}

func TestUnsupportedServerRequestGetsAnErrorAndDurableFeedback(t *testing.T) {
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()
	t.Cleanup(func() { clientR.Close(); clientW.Close(); serverR.Close(); serverW.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response := make(chan rpcIn, 1)
	go func() {
		defer serverW.Close()
		decode, encode := json.NewDecoder(clientR), json.NewEncoder(serverW)
		var request rpcIn
		for _, body := range []string{`{}`, ``, `{"thread":{"id":"thread-1"}}`, `{"turn":{"id":"turn-1","status":"inProgress","items":[]}}`} {
			if decode.Decode(&request) != nil {
				return
			}
			if body != "" {
				_ = encode.Encode(map[string]any{"id": request.ID, "result": json.RawMessage(body)})
			}
		}
		_ = encode.Encode(map[string]any{"id": "unsupported", "method": "item/tool/call", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "tool": "unregistered"}})
		if decode.Decode(&request) != nil {
			return
		}
		response <- request
		_, _ = io.WriteString(serverW, `{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","items":[{"id":"answer","type":"agentMessage","text":"Used a supported tool."}]}}}`+"\n")
	}()
	col := &collector{}
	result, got := (appServer{message: "Run", approval: "never"}).converse(ctx, clientW, serverR, nil, col.emit)
	if !got || result.IsError {
		t.Fatalf("unsupported request hung the turn: %+v got=%v", result, got)
	}
	select {
	case reply := <-response:
		if string(reply.ID) != `"unsupported"` || reply.Error == nil || reply.Error.Code != -32601 {
			t.Fatalf("invalid RPC response: %+v", reply)
		}
	default:
		t.Fatal("server never received an explicit rejection")
	}
	feedback := firstOf(col.snapshot(), agent.EventInteraction)
	if feedback == nil || feedback.Interaction.Kind != "error" || !strings.Contains(feedback.Interaction.Detail, "item/tool/call") || feedback.Interaction.NeedsResponse {
		t.Fatalf("unsupported request hidden from chat: %+v", col.snapshot())
	}
}
