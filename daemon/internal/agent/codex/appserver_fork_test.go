package codex

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestAppServerForkVerifiesTheNativeCutoffBeforePublishingSessionOrSendingPrompt(t *testing.T) {
	for _, scenario := range []string{"accepted", "ignored cutoff", "original identity", "conflicting notification"} {
		t.Run(scenario, func(t *testing.T) {
			clientR, clientW := io.Pipe()
			serverR, serverW := io.Pipe()
			defer clientR.Close()
			defer clientW.Close()
			defer serverR.Close()
			defer serverW.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			requests := make(chan []rpcIn, 1)
			go func() {
				defer serverW.Close()
				var seen []rpcIn
				defer func() { requests <- seen }()
				decoder, encoder := json.NewDecoder(clientR), json.NewEncoder(serverW)
				for {
					var request rpcIn
					if decoder.Decode(&request) != nil {
						return
					}
					seen = append(seen, request)
					switch request.Method {
					case "initialize":
						_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{}})
					case "thread/fork":
						id := "branch"
						turns := []map[string]string{{"id": "kept-turn"}}
						if scenario == "original identity" {
							id = "source"
						}
						if scenario == "ignored cutoff" {
							turns = append(turns, map[string]string{"id": "discarded-turn"})
						}
						notificationID := id
						if scenario == "conflicting notification" {
							notificationID = "source"
						}
						_ = encoder.Encode(map[string]any{"method": "thread/started", "params": map[string]any{
							"thread": map[string]any{"id": notificationID, "sessionId": "source"},
						}})
						_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{
							"thread": map[string]any{"id": id, "sessionId": "source", "turns": turns},
						}})
					case "turn/start":
						_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{
							"turn": map[string]any{"id": "new-turn", "status": "completed", "items": []any{
								map[string]any{"id": "answer", "type": "agentMessage", "text": "Done."},
							}},
						}})
						return
					}
				}
			}()
			col := &collector{}
			a := appServer{resumeID: "source", fork: true, forkAt: "kept-turn", message: "Edited", approval: "on-request", sandbox: "workspace-write"}
			res, got := a.converse(ctx, clientW, serverR, nil, col.emit)
			_ = clientW.Close()
			var seen []rpcIn
			select {
			case seen = <-requests:
			case <-ctx.Done():
				t.Fatal("protocol fixture did not finish")
			}
			forks, sends := 0, 0
			for _, request := range seen {
				if request.Method == "thread/fork" {
					forks++
					var params map[string]any
					_ = json.Unmarshal(request.Params, &params)
					if params["threadId"] != "source" || params["lastTurnId"] != "kept-turn" || params["excludeTurns"] != false {
						t.Fatalf("incorrect native fork request: %s", request.Params)
					}
				}
				if request.Method == "turn/start" {
					sends++
					var params map[string]any
					_ = json.Unmarshal(request.Params, &params)
					if params["threadId"] != "branch" {
						t.Fatalf("prompt was redirected to the original session: %s", request.Params)
					}
				}
			}
			if forks != 1 {
				t.Fatalf("expected exactly one native fork, got %d", forks)
			}
			if scenario == "accepted" {
				if !got || res.IsError || res.SessionID != "branch" || sends != 1 || countType(col.snapshot(), agent.EventSession) != 1 {
					t.Fatalf("native fork failed: %+v, sends=%d", res, sends)
				}
			} else if !res.IsError || sends != 0 || firstOfType(col.snapshot(), agent.EventSession) != nil {
				t.Fatalf("unsafe fork was committed: %+v, sends=%d", res, sends)
			}
		})
	}
}
