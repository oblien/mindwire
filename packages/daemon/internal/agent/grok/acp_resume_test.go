package grok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/acp"
	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

func TestACPResumeDrainsHistoryBeforePrompt(t *testing.T) {
	for _, resumeLatest := range []bool{false, true} {
		for _, chunks := range []int{1, 256} {
			t.Run(fmt.Sprintf("latest=%v/chunks=%d", resumeLatest, chunks), func(t *testing.T) {
				inputR, inputW := io.Pipe()
				outputR, outputW := io.Pipe()
				t.Cleanup(func() { inputR.Close(); inputW.Close(); outputR.Close(); outputW.Close() })
				serverDone := make(chan error, 1)
				go func() {
					defer outputW.Close()
					dec, enc := json.NewDecoder(inputR), json.NewEncoder(outputW)
					methods := []string{"initialize", "session/load", "session/prompt"}
					if resumeLatest {
						methods[1] = "session/resume"
					}
					for i, method := range methods {
						var req acp.Message
						if err := dec.Decode(&req); err != nil {
							serverDone <- err
							return
						}
						if req.Method != method {
							serverDone <- fmt.Errorf("method = %s, want %s", req.Method, method)
							return
						}
						count, text := 0, ""
						if i == 1 {
							count, text = chunks, "Previous answer. "
						} else if i == 2 {
							count, text = 1, "New answer."
						}
						for range count {
							if err := enc.Encode(map[string]any{"method": "session/update", "params": map[string]any{
								"sessionId": "native-existing", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"text": text}},
							}}); err != nil {
								serverDone <- err
								return
							}
						}
						response := map[string]any{"sessionId": "native-existing"}
						if i == 2 {
							response["stopReason"] = "end_turn"
						}
						if err := enc.Encode(map[string]any{"id": req.ID, "result": response}); err != nil {
							serverDone <- err
							return
						}
					}
					serverDone <- nil
				}()
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				in := agent.TurnInput{SessionID: "native-existing", CWD: t.TempDir(), Message: "Continue"}
				if resumeLatest {
					in.SessionID, in.Options.ContinueLatest = "", true
				}
				client := acp.NewClient(inputW, outputR)
				defer client.Close()
				var streamed string
				result, err := acpRun(ctx, client, in, func(e agent.Event) {
					if e.Type == agent.EventText {
						streamed += e.Text
					}
				})
				if err != nil || result.SessionID != "native-existing" || result.Text != "New answer." || streamed != result.Text {
					t.Fatalf("resume = %+v, streamed=%q, error=%v", result, streamed, err)
				}
				if err := <-serverDone; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
