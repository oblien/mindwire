package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func (a *API) voiceStatus(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	cap := ag.Capabilities().Voice
	if cap == nil {
		cap = &agent.VoiceCapability{Mode: "none", Support: agent.SupportNone}
	}
	status := agent.VoiceStatus{Capability: *cap, Code: "unsupported", Detail: cap.Detail}
	if module, ok := ag.Adapter.(agent.VoiceModule); ok && cap.Remote {
		var err error
		status, err = module.VoiceStatus(r.Context(), ag.Creds)
		if err != nil {
			status = agent.VoiceStatus{Capability: *cap, Code: "unavailable", Detail: "Could not check the harness voice connection. Reconnect or update the harness, then retry."}
		}
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) voiceInput(w http.ResponseWriter, r *http.Request) {
	// A half-second PCM batch is 32 KiB after base64. Reject attachment-sized
	// bodies here before decoding or allocating a live media packet.
	r.Body = http.MaxBytesReader(w, r.Body, 40<<10)
	var req struct {
		ClientID string            `json:"clientId"`
		Action   string            `json:"action"`
		Sequence int64             `json:"sequence"`
		Audio    *agent.VoiceAudio `json:"audio"`
	}
	if decode(w, r, &req) != nil {
		badRequest(w, "Invalid voice request.")
		return
	}
	v, ok := a.sup.Voice(r.PathValue("id"), req.ClientID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": agent.ErrVoiceClosed.Error()})
		return
	}
	var err error
	switch req.Action {
	case "stop":
		v.EndInput()
	case "heartbeat":
		err = v.Touch()
	case "audio":
		if req.Audio == nil {
			badRequest(w, "Voice audio is required.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		err = v.Send(ctx, req.Sequence, *req.Audio)
	default:
		badRequest(w, "Unknown voice action.")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) voiceStream(w http.ResponseWriter, r *http.Request) {
	v, ok := a.sup.Voice(r.PathValue("id"), r.URL.Query().Get("clientId"))
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": agent.ErrVoiceClosed.Error()})
		return
	}
	state, events, unsubscribe, err := v.Subscribe()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	write := func(event agent.VoiceEvent) bool {
		data, err := json.Marshal(event)
		if err != nil {
			return false
		}
		_ = controller.SetWriteDeadline(time.Now().Add(8 * time.Second))
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	if !write(state) {
		return
	}
	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open || !write(event) {
				return
			}
			if event.Type == "closed" || event.Type == "error" {
				return
			}
		case <-heartbeat.C:
			if !write(agent.VoiceEvent{Type: "heartbeat"}) {
				return
			}
		}
	}
}
