package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Runs the installed Codex against a loopback voice backend and private native
// home. It sends synthetic PCM only; no account, microphone or paid inference.
func TestNativeVoiceLocalProtocol(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 for the installed Codex voice protocol")
	}
	for _, requireNativeAuth := range []bool{true, false} {
		t.Run(fmt.Sprintf("provider_native_auth=%v", requireNativeAuth), func(t *testing.T) {
			nativeVoiceLocalProtocol(t, requireNativeAuth)
		})
	}
}

func nativeVoiceLocalProtocol(t *testing.T, requireNativeAuth bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	frames := make(chan map[string]json.RawMessage, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		write := func(v any) { data, _ := json.Marshal(v); _ = conn.Write(ctx, websocket.MessageText, data) }
		write(map[string]any{"type": "session.created", "session": map[string]any{"id": "voice-fixture", "model": "voice-fixture"}})
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var frame map[string]json.RawMessage
			if json.Unmarshal(data, &frame) != nil {
				continue
			}
			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}
			var kind string
			_ = json.Unmarshal(frame["type"], &kind)
			if kind == "session.update" {
				write(map[string]any{"type": "session.updated", "session": map[string]any{"id": "voice-fixture"}})
			}
			if kind == "input_audio_buffer.append" {
				// Codex's native voice backend has its own conversation protocol.
				write(map[string]any{"type": "conversation.output_audio.delta", "item_id": "voice-reply",
					"delta": base64.StdEncoding.EncodeToString([]byte{2, 0, 3, 0}), "sample_rate": 24000, "num_channels": 1, "samples_per_channel": 2})
			}
		}
	}))
	defer server.Close()
	nativeHome := t.TempDir()
	t.Setenv("CODEX_HOME", nativeHome)
	model := audioModelCatalog(t, nativeHome, false)
	if err := os.WriteFile(filepath.Join(nativeHome, "auth.json"), []byte(`{"OPENAI_API_KEY":"voice-fixture-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`model = %q
model_catalog_json = %q
model_provider = "local"
experimental_realtime_ws_base_url = %q
experimental_realtime_ws_model = "voice-fixture"
experimental_realtime_ws_backend_prompt = "Local voice protocol fixture. Do not call tools."
experimental_realtime_ws_startup_context = "Private loopback fixture."
[realtime]
version = "v1"
transport = "websocket"
[model_providers.local]
name = "Local voice fixture"
base_url = %q
wire_api = "responses"
requires_openai_auth = %v
supports_websockets = false
`, model, filepath.Join(nativeHome, "audio-model.json"), strings.Replace(server.URL, "http://", "ws://", 1), server.URL, requireNativeAuth)
	if err := os.WriteFile(filepath.Join(nativeHome, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	bridge := agent.NewVoiceBridge("native-loopback-voice-client")
	defer bridge.Close()
	_, events, unsubscribe, err := bridge.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	result := make(chan agent.TurnResult, 1)
	go func() {
		in := agent.TurnInput{CWD: t.TempDir(), Voice: bridge, Inbound: make(chan agent.Inbound),
			Options: agent.TurnOptions{Voice: &agent.VoiceOptions{ClientID: bridge.ClientID}}}
		r, _ := newAppServer(in, materialized{}).Run(ctx, in, func(agent.Event) {})
		result <- r
	}()
	ready := false
	for !ready {
		select {
		case ev := <-events:
			if ev.Type == "ready" {
				ready = true
			}
			if ev.Type == "error" {
				t.Fatal(ev.Text)
			}
		case r := <-result:
			t.Fatalf("native voice exited before ready: %+v", r)
		case <-ctx.Done():
			t.Fatal("native voice startup timed out")
		}
	}
	// Wait until the backend has applied its native session settings.
	configured := false
	for !configured {
		select {
		case frame := <-frames:
			var kind string
			_ = json.Unmarshal(frame["type"], &kind)
			t.Log("native voice backend frame", kind)
			configured = kind == "session.update"
		case ev := <-events:
			if ev.Type == "error" {
				t.Fatal(ev.Text)
			}
		case r := <-result:
			t.Fatalf("native voice exited: %+v", r)
		case <-ctx.Done():
			t.Fatal("native voice did not configure its backend")
		}
	}
	pcm := make([]byte, 9600)
	for i := 0; i < len(pcm); i += 2 {
		pcm[i] = 7
	}
	if err := bridge.Send(ctx, 1, agent.VoiceAudio{Data: pcm, SampleRate: 24000, Channels: 1}); err != nil {
		t.Fatal(err)
	}
	inputReceived, outputReceived := false, false
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !inputReceived || !outputReceived {
		select {
		case frame := <-frames:
			var kind string
			_ = json.Unmarshal(frame["type"], &kind)
			if kind != "input_audio_buffer.append" {
				continue
			}
			var received []byte
			_ = json.Unmarshal(frame["audio"], &received)
			if string(received) != string(pcm) {
				t.Fatalf("native backend received %d PCM bytes, expected %d", len(received), len(pcm))
			}
			inputReceived = true
		case ev := <-events:
			if ev.Type == "error" {
				t.Fatal(ev.Text)
			}
			if ev.Type == "audio" {
				if ev.Audio == nil || string(ev.Audio.Data) != string([]byte{2, 0, 3, 0}) || ev.Audio.SampleRate != 24000 || ev.Audio.Channels != 1 {
					t.Fatalf("speaker audio was changed: %+v", ev)
				}
				outputReceived = true
			}
		case r := <-result:
			t.Fatalf("native voice ended before audio reached its backend: %+v", r)
		case <-deadline.C:
			t.Fatalf("native PCM delivery incomplete: microphone=%v speaker=%v", inputReceived, outputReceived)
		case <-ctx.Done():
			t.Fatal("native voice audio did not reach its backend")
		}
	}
	bridge.EndInput()
	select {
	case r := <-result:
		if r.IsError {
			t.Fatal(r.Text)
		}
	case <-ctx.Done():
		t.Fatal("native voice did not stop")
	}
}
