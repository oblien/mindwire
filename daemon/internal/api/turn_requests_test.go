package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

func TestTurnRequestReceiptsSurviveRetriesCompletionAndRestart(t *testing.T) {
	for _, mode := range []string{"turn", "resolve"} {
		t.Run(mode, func(t *testing.T) {
			fake := resolveFakeAdapter{id: t.Name()}
			agent.Register(fake)
			statePath, cwd := filepath.Join(t.TempDir(), "state.json"), t.TempDir()
			open := func() (http.Handler, *session.Store, *orchestrator.Supervisor) {
				st, err := session.Open(statePath)
				if err != nil {
					t.Fatal(err)
				}
				hub := stream.New()
				sup := orchestrator.New(st, hub, notify.Fanout(nil), cwd, fake.id)
				mux := http.NewServeMux()
				New(st, hub, sup).Register(mux)
				return mux, st, sup
			}
			h, st, sup := open()
			body := `{"requestId":"lost-ack","chatId":"chat","message":"one operation","mode":"` + mode + `"}`
			var wg sync.WaitGroup
			ids := make(chan string, 12)
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					response := serve(t, h, "POST", "/turns", body)
					var run session.Run
					if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &run) != nil {
						t.Errorf("retry failed: %d %s", response.Code, response.Body.String())
						return
					}
					ids <- run.ID
				}()
			}
			wg.Wait()
			close(ids)
			sup.Wait()
			var original string
			for id := range ids {
				if original == "" {
					original = id
				}
				if id != original {
					t.Fatal("retry created a second run")
				}
			}
			if len(st.Messages("chat")) != 2 {
				t.Fatal("retry duplicated history")
			}
			h, _, sup = open()
			response := serve(t, h, "POST", "/turns", body)
			var restored session.Run
			if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &restored) != nil || restored.ID != original || restored.Status != "done" {
				t.Fatalf("completed receipt lost after restart: %d %s", response.Code, response.Body.String())
			}
			changed := serve(t, h, "POST", "/turns", strings.Replace(body, "one operation", "different operation", 1))
			if changed.Code != 409 || !strings.Contains(changed.Body.String(), "requestId") {
				t.Fatal("request ID reuse did not reject a changed operation")
			}
			invalid := serve(t, h, "POST", "/turns", strings.Replace(body, "lost-ack", "invalid id", 1))
			if invalid.Code != 400 {
				t.Fatal("invalid request ID accepted")
			}
			sup.Wait()
		})
	}
}
