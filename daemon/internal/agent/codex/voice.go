package codex

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func (a adapter) VoiceStatus(ctx context.Context, store agent.CredStore) (agent.VoiceStatus, error) {
	status := agent.VoiceStatus{Capability: *a.Capabilities().Voice}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	server := newAppServer(agent.TurnInput{Config: agent.ReadSettings(a, store), Env: a.Auth(store).EnvForRun()}, materialized{})
	rpc, err := startMetadataRPCWithEnv(ctx, server.command, server.env)
	if err != nil {
		return status, err
	}
	defer rpc.close()
	var catalog struct {
		Voices struct {
			V1 []string `json:"v1"`
			V2 []string `json:"v2"`
		} `json:"voices"`
	}
	if err := rpc.call(ctx, "thread/realtime/listVoices", map[string]any{}, &catalog); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "method not found") || strings.Contains(err.Error(), "-32601") {
			status.Code, status.Detail = "update_required", "Update Codex to a version with the native voice interface."
		} else {
			status.Code, status.Detail = "unavailable", "Could not check Codex voice. Reconnect and try again."
		}
		return status, nil
	}
	var config struct {
		Config struct {
			Backend  string `json:"experimental_realtime_ws_base_url"`
			Realtime struct {
				Version string `json:"version"`
			} `json:"realtime"`
		} `json:"config"`
	}
	_ = rpc.call(ctx, "config/read", map[string]any{"includeLayers": false}, &config)
	// No guessed cross-version voice list. An unspecified native version keeps
	// its own default speaker; explicit native versions can expose their choices.
	switch config.Config.Realtime.Version {
	case "v1":
		status.Voices = catalog.Voices.V1
	case "v2":
		status.Voices = catalog.Voices.V2
	}
	account, err := rpc.account(ctx)
	if err != nil {
		return status, err
	}
	if account == nil && strings.TrimSpace(config.Config.Backend) == "" {
		// Custom coding providers can report no account even when Codex has a
		// saved native voice login. Read that account through Codex itself; this
		// separate metadata process never changes the coding provider or login.
		rpc.close()
		native, err := startAuthRPC(ctx)
		if err != nil {
			return status, err
		}
		defer native.close()
		account, err = native.account(ctx)
		if err != nil {
			return status, err
		}
		if account == nil {
			status.Code, status.Detail = "voice_account_required", "Codex voice needs a voice-enabled account or a configured realtime backend. The current coding-provider connection does not provide voice access."
			return status, nil
		}
	}
	status.Available = true // native startup remains authoritative for plan/rollout access
	status.Detail = "Audio goes directly to Codex's native voice service. Your coding model stays selected."
	return status, nil
}

// Realtime notifications are separate from normal text/tool events. Only native
// transcripts are text; microphone and speaker PCM never become file references.
func nativeVoiceEvent(method string, raw json.RawMessage) (agent.VoiceEvent, bool) {
	if !strings.HasPrefix(method, "thread/realtime/") {
		return agent.VoiceEvent{}, false
	}
	var p struct {
		Text    string `json:"text"`
		Delta   string `json:"delta"`
		Role    string `json:"role"`
		ItemID  string `json:"itemId"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
		Audio   struct {
			Data       []byte `json:"data"`
			SampleRate int    `json:"sampleRate"`
			Channels   int    `json:"numChannels"`
			ItemID     string `json:"itemId"`
		} `json:"audio"`
		Item struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Text string `json:"text"`
			Role string `json:"role"`
		} `json:"item"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return agent.VoiceEvent{Type: "error", Text: "Codex returned invalid voice data."}, true
	}
	ev := agent.VoiceEvent{Text: agent.FirstNonEmpty(p.Text, p.Delta), Role: p.Role, ItemID: p.ItemID}
	switch method {
	case "thread/realtime/started":
		ev.Type = "ready"
	case "thread/realtime/outputAudio/delta":
		ev.Type = "audio"
		if p.Audio.Channels < 1 || p.Audio.Channels > 2 || len(p.Audio.Data) > 192000 || len(p.Audio.Data)%(2*p.Audio.Channels) != 0 || p.Audio.SampleRate < 8000 || p.Audio.SampleRate > 48000 {
			return agent.VoiceEvent{Type: "error", Text: "Codex returned unsupported voice audio."}, true
		}
		ev.ItemID = agent.FirstNonEmpty(p.Audio.ItemID, ev.ItemID)
		ev.Audio = &agent.VoiceAudio{Data: p.Audio.Data, SampleRate: p.Audio.SampleRate, Channels: p.Audio.Channels}
	case "thread/realtime/transcript/delta", "thread/realtime/item/transcript/delta":
		ev.Type = "transcript"
	case "thread/realtime/transcript/done":
		ev.Type, ev.Final = "transcript", true
	case "thread/realtime/item/completed":
		if p.Item.Type != "transcriptSegment" {
			return agent.VoiceEvent{}, false
		}
		ev.Type, ev.Text, ev.Role, ev.ItemID, ev.Final = "transcript", p.Item.Text, p.Item.Role, p.Item.ID, true
	case "thread/realtime/error":
		ev.Type, ev.Text = "error", agent.FirstNonEmpty(p.Message, p.Reason, "Codex voice could not connect. Check voice access for this account.")
	case "thread/realtime/closed":
		ev.Type, ev.Text = "closed", p.Reason
	case "thread/realtime/itemAdded":
		if p.Item.Type != "input_audio_buffer.speech_started" && p.Item.Type != "response.cancelled" {
			return agent.VoiceEvent{}, false
		}
		ev.Type = "listening"
	default:
		return agent.VoiceEvent{}, false
	}
	return ev, true
}
