package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oblien/mindwire/packages/daemon/internal/agent"
)

// Shapes are from OpenCode 1.18.19's generated OpenAPI contract. Exercise the
// complete SSE -> interaction -> HTTP reply path, not only the JSON mapper.
func TestV1CurrentQuestionsPermissionsAndDeltas(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		t.Run(fmt.Sprint(interactive), func(t *testing.T) {
			prompted := make(chan struct{})
			permission, question := make(chan map[string]any, 1), make(chan map[string]any, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/session":
					fmt.Fprint(w, `{"id":"ses_current"}`)
				case "/session/ses_current/prompt_async":
					close(prompted)
					w.WriteHeader(204)
				case "/permission/perm_1/reply", "/question/q_1/reply":
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if r.URL.Path == "/permission/perm_1/reply" {
						permission <- body
					} else {
						question <- body
					}
					fmt.Fprint(w, `true`)
				case "/event":
					w.Header().Set("Content-Type", "text/event-stream")
					send := func(raw string) { fmt.Fprintf(w, "data: %s\n\n", raw); w.(http.Flusher).Flush() }
					send(`{"type":"server.connected","properties":{}}`)
					select {
					case <-prompted:
					case <-r.Context().Done():
						return
					}
					send(`{"type":"permission.asked","properties":{"id":"perm_1","sessionID":"ses_current","permission":"bash","patterns":["ls"],"always":["*"],"metadata":{}}}`)
					select {
					case body := <-permission:
						if body["reply"] != "once" {
							t.Errorf("permission: %v", body)
						}
					case <-r.Context().Done():
						return
					}
					send(`{"type":"question.asked","properties":{"id":"q_1","sessionID":"ses_current","questions":[{"header":"Scope","question":"Which scope?","options":[{"label":"Small","description":"One file"}]}]}}`)
					select {
					case body := <-question:
						b, _ := json.Marshal(body)
						if string(b) != `{"answers":[["Small"]]}` {
							t.Errorf("question: %s", b)
						}
					case <-r.Context().Done():
						return
					}
					send(`{"type":"message.part.updated","properties":{"part":{"id":"part_1","sessionID":"ses_current","messageID":"message_1","type":"text","text":""}}}`)
					send(`{"type":"message.part.delta","properties":{"partID":"part_1","sessionID":"ses_current","messageID":"message_1","field":"text","delta":"Done."}}`)
					send(`{"type":"message.part.updated","properties":{"part":{"id":"part_1","sessionID":"ses_current","messageID":"message_1","type":"text","text":"Done."}}}`)
					send(`{"type":"session.idle","properties":{"sessionID":"ses_current"}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer ts.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			inbound := make(chan agent.Inbound, 4)
			text := ""
			result, got := (server{message: "Review", interactive: interactive}).converse(ctx, ts.URL, inbound, func(ev agent.Event) {
				if ev.Type == agent.EventText {
					text += ev.Text
				}
				if it := ev.Interaction; it != nil {
					meta := map[string]string{}
					for k, v := range it.Meta {
						if s, ok := v.(string); ok {
							meta[k] = s
						}
					}
					inbound <- agent.Inbound{Kind: "response", InteractionID: it.ID, Decision: "allow", Meta: meta, Answers: map[string]agent.QuestionAnswer{"0": {Options: []string{"Small"}}}}
				}
			})
			if !got || result.IsError || result.Text != "Done." || text != "Done." {
				t.Fatalf("result=%+v got=%v text=%q", result, got, text)
			}
		})
	}
}
