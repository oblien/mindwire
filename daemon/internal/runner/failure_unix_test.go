//go:build unix

package runner

import (
	"bufio"
	"context"
	"io"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/driver"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

func TestNativeProcessStopAndExternalKillKeepDistinctOutcomes(t *testing.T) {
	for _, stop := range []bool{true, false} {
		name := "external kill"
		if stop {
			name = "user Stop"
		}
		t.Run(name, func(t *testing.T) {
			st, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			hub := stream.New()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pids := make(chan int, 1)
			ctx = proc.WithReporter(ctx, func(pid int) { pids <- pid })
			ready := make(chan struct{})
			ad := forkAdapter{stubAdapter: &stubAdapter{}, fn: func(ctx context.Context, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
				return (driver.CLI{Command: "printf 'saved-session\\nPartial work\\n'; exec sleep 60",
					Parse: func(stdout io.Reader, emit agent.Emit) (agent.TurnResult, bool) {
						scan := bufio.NewScanner(stdout)
						if scan.Scan() {
							emit(agent.Event{Type: agent.EventSession, SessionID: scan.Text()})
						}
						if scan.Scan() {
							emit(agent.Event{Type: agent.EventText, Text: scan.Text()})
						}
						close(ready)
						for scan.Scan() {
						}
						return agent.TurnResult{}, false
					}}).Run(ctx, in, emit)
			}}
			r := New(st, ad, stubAuth{}, namespacedCreds{vals: map[string]string{}}, hub, t.TempDir())
			type outcome struct {
				result agent.TurnResult
				parts  []agent.Part
			}
			finished := make(chan outcome, 1)
			go func() {
				result, parts := r.RunTurn(ctx, Turn{ChatID: "chat", RunID: "run", Message: "work"})
				finished <- outcome{result, parts}
			}()
			select {
			case <-ready:
			case <-time.After(10 * time.Second):
				t.Fatal("fixture did not start")
			}
			pid := <-pids
			if stop {
				cancel()
			} else if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
				t.Fatal(err)
			}
			var got outcome
			select {
			case got = <-finished:
			case <-time.After(10 * time.Second):
				t.Fatal("process did not exit")
			}
			if st.Session(ad.ID(), "chat") != "saved-session" || len(got.parts) == 0 || got.parts[0].Text != "Partial work" {
				t.Fatal("termination lost the resumable session or partial output")
			}
			if got.result.Cancelled != stop || got.result.IsError == stop {
				t.Fatalf("wrong termination state: %+v", got.result)
			}
			if !stop && (!strings.Contains(got.result.Text, "terminated on the workspace") || !strings.Contains(got.result.Text, "continue this chat")) {
				t.Fatalf("unhelpful process failure: %+v", got.result)
			}
			events, _, _, unsubscribe := hub.Subscribe("run")
			defer unsubscribe()
			for _, event := range events {
				if event.Type == agent.EventError && (stop || event.Error == "signal: killed") {
					t.Fatalf("misleading streamed process error: %+v", event)
				}
			}
		})
	}
}
