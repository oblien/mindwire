package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestRejectedResumeReapsAppServerWithoutWaitingForTurnTimeout(t *testing.T) {
	for _, mode := range []string{"rpc", "notification"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			a := appServer{command: "'" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "' -test.run=^TestRejectedResumeHelper$",
				env: map[string]string{"MINDWIRE_REJECTED_RESUME_HELPER": mode}, resumeID: "busy-thread"}
			result, _ := a.Run(ctx, agent.TurnInput{}, func(agent.Event) {})
			if ctx.Err() != nil {
				t.Fatal("resume rejection left the process and run stuck until timeout")
			}
			if !result.IsError || !strings.Contains(result.Text, "open in another client") {
				t.Fatalf("lost native conflict: %+v", result)
			}
		})
	}
}

func TestRejectedResumeHelper(t *testing.T) {
	mode := os.Getenv("MINDWIRE_REJECTED_RESUME_HELPER")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request rpcIn
		_ = json.Unmarshal(scanner.Bytes(), &request)
		if request.Method == "initialize" {
			fmt.Printf("{\"id\":%s,\"result\":{}}\n", request.ID)
		}
		if request.Method == "thread/resume" {
			if mode == "rpc" {
				fmt.Printf("{\"id\":%s,\"error\":{\"code\":-32000,\"message\":\"thread is already in use by another client\"}}\n", request.ID)
			} else {
				fmt.Printf("{\"id\":%s,\"result\":{\"thread\":{\"id\":\"busy-thread\"}}}\n", request.ID)
			}
		}
		if request.Method == "turn/start" && mode == "notification" {
			fmt.Println(`{"method":"error","params":{"threadId":"busy-thread","error":{"message":"session is already in use by another process"},"willRetry":false}}`)
		}
	}
	// Mimic an app-server which keeps runtime tasks alive after stdin closes.
	for {
		time.Sleep(time.Hour)
	}
}
