package codex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestAudioOnlyAndMixedTurnsUseNativeAudio(t *testing.T) {
	audio := agent.Attachment{Name: "voice.m4a", Mime: "audio/mp4", Data: []byte("recording")}
	notes := agent.Attachment{Name: "notes.txt", Path: "/repo/notes.txt"}
	for _, attachments := range [][]agent.Attachment{{audio}, {audio, notes}, {notes, audio}} {
		files, suffix, cleanup, err := materialize(agent.TurnInput{Options: agent.TurnOptions{Attachments: attachments}})
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		if len(files.audioAttachments) != 1 || !strings.HasSuffix(files.audioAttachments[0].Path, ".m4a") {
			t.Fatalf("incorrect native audio routing: %+v", files)
		}
		data, err := os.ReadFile(files.audioAttachments[0].Path)
		if err != nil || string(data) != "recording" {
			t.Fatal("audio bytes changed", err)
		}
		server := newAppServer(agent.TurnInput{}, files)
		server.nativeAudio = true
		items := server.turnParams("thread")["input"].([]any)
		if len(attachments) == 1 {
			if len(items) != 1 || items[0].(map[string]any)["type"] != "localAudio" {
				t.Fatalf("audio-only turn must not contain an empty text prompt: %+v", items)
			}
		} else if len(items) != 2 || server.inputText() != "\n\nAttached files:\n- notes.txt (/repo/notes.txt)" {
			t.Fatalf("mixed native audio must retain the ordinary file once: %+v", items)
		}
		server.nativeAudio = false
		items = server.turnParams("thread")["input"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["type"] != "text" || server.inputText() != suffix ||
			strings.Count(suffix, "Attached files:") != 1 {
			t.Fatalf("a text-only model must receive one ordered file block: %+v", items)
		}
		last := -1
		for _, attachment := range attachments {
			index := strings.Index(suffix, attachment.Name)
			if index <= last {
				t.Fatalf("fallback lost original attachment order: %q", suffix)
			}
			last = index
		}
		cleanup()
		if _, err := os.Stat(files.audioAttachments[0].Path); !os.IsNotExist(err) {
			t.Fatal("temporary audio was not removed")
		}
	}
}

func TestLargeAudioRolloutPreservesRecordingWithoutTruncatingHistory(t *testing.T) {
	// Twelve MiB becomes a native line slightly larger than the old 16 MiB cap.
	encoded := strings.Repeat("AQID", 4<<20)
	raw := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_audio","audio_url":"data:audio/mp4;base64,` + encoded + `"}]}}`
	history, err := parseRollout(strings.NewReader(raw), "audio-chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || len(history[0].Attachments) != 1 || len(history[0].Attachments[0].Data) != 12<<20 {
		t.Fatal("a supported audio upload disappeared from native history")
	}
}

// Real CLI, private native state, local model endpoint. No account or inference is used.
// Also checks M4A when MINDWIRE_AUDIO_FIXTURE points at a sample from the iOS encoder.
func TestAppServerLocalAudio(t *testing.T) {
	if os.Getenv("CODEX_LOCAL") != "1" {
		t.Skip("set CODEX_LOCAL=1 for native audio transport checks")
	}
	fixtures := []agent.Attachment{{Name: "voice.wav", Mime: "audio/wav", Data: audioTestWAV()}}
	if path := os.Getenv("MINDWIRE_AUDIO_FIXTURE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, agent.Attachment{Name: "voice.m4a", Mime: "audio/mp4", Data: data})
	}
	for _, audio := range fixtures {
		for _, nativeAudio := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/native=%v", audio.Name, nativeAudio), func(t *testing.T) {
				nativeHome := t.TempDir()
				t.Setenv("CODEX_HOME", nativeHome)
				model := audioModelCatalog(t, nativeHome, nativeAudio)
				var mu sync.Mutex
				var bodies [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/responses") {
						http.NotFound(w, r)
						return
					}
					var body json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					mu.Lock()
					bodies = append(bodies, append([]byte(nil), body...))
					index := len(bodies)
					mu.Unlock()
					writeLocalReply(w, index, "Audio received")
				}))
				defer server.Close()
				config := fmt.Sprintf("model = %q\nmodel_catalog_json = %q\nmodel_provider = \"local\"\n[model_providers.local]\nname = \"Local audio fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\n", model, filepath.Join(nativeHome, "audio-model.json"), server.URL)
				if err := os.WriteFile(filepath.Join(nativeHome, "config.toml"), []byte(config), 0600); err != nil {
					t.Fatal(err)
				}
				input := agent.TurnInput{CWD: t.TempDir(),
					Env:     map[string]string{"CODEX_API_KEY": "audio-fixture-key", "OPENAI_BASE_URL": server.URL},
					Options: agent.TurnOptions{Attachments: []agent.Attachment{audio}}}
				for turn := 0; turn < 2; turn++ {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					result, err := (adapter{}).RunStream(ctx, input, func(agent.Event) {})
					cancel()
					if err != nil || result.IsError || result.Text != "Audio received" {
						t.Fatalf("native audio turn %d: %+v %v", turn, result, err)
					}
					input.SessionID = result.SessionID
				}
				mu.Lock()
				defer mu.Unlock()
				if len(bodies) != 2 {
					t.Fatalf("got %d requests, want one fresh and one resume", len(bodies))
				}
				for _, body := range bodies {
					if nativeAudio && (!bytes.Contains(body, []byte(`"input_audio"`)) || !bytes.Contains(body, []byte(base64.StdEncoding.EncodeToString(audio.Data)))) {
						var request map[string]any
						_ = json.Unmarshal(body, &request)
						t.Fatalf("recording never reached native model audio input (input kinds: %s)", audioInputKinds(request["input"]))
					}
					if !nativeAudio && (bytes.Contains(body, []byte(`"input_audio"`)) || !bytes.Contains(body, []byte("Attached files:")) || !bytes.Contains(body, []byte(audio.Name))) {
						t.Fatal("text-only model must receive the audio as a readable file, not silently omitted media")
					}
				}
				if nativeAudio {
					history, err := (adapter{}).History(agent.HistoryQuery{SessionID: input.SessionID, CWD: input.CWD})
					if err != nil {
						t.Fatal(err)
					}
					users := 0
					for _, message := range history {
						if message.Role != "user" {
							continue
						}
						users++
						if message.Text != "" || len(message.Attachments) != 1 || !bytes.Equal(message.Attachments[0].Data, audio.Data) {
							t.Fatalf("native history lost the audio or exposed transport wrappers: text=%q attachments=%d", message.Text, len(message.Attachments))
						}
					}
					if users != 2 {
						t.Fatalf("native history has %d audio messages, want fresh and resumed input", users)
					}
				}
			})
		}
	}
}

// Only this private local test model advertises audio. Production capabilities
// always come from the installed CLI's unmodified catalog for the selected model.
func audioModelCatalog(t *testing.T, home string, audio bool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "codex", "debug", "models").Output()
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	models, ok := catalog["models"].([]any)
	if !ok || len(models) == 0 {
		t.Fatal("no native model metadata")
	}
	model := models[0].(map[string]any)
	const name = "mindwire-audio-fixture"
	model["slug"], model["display_name"], model["visibility"] = name, name, "list"
	model["input_modalities"] = []string{"text", "image"}
	if audio {
		model["input_modalities"] = []string{"text", "image", "audio"}
	}
	catalog["models"] = []any{model}
	data, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "audio-model.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func audioInputKinds(v any) string {
	var kinds []string
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if kind, ok := value["type"].(string); ok {
				kinds = append(kinds, kind)
			}
			for _, child := range value {
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	visit(v)
	return strings.Join(kinds, ", ")
}

func TestAudioSteeringKeepsFilesUntilConsumedAndUsesOneModelLookup(t *testing.T) {
	for _, native := range []bool{true, false} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			clientR, clientW := io.Pipe()
			serverR, serverW := io.Pipe()
			defer clientR.Close()
			defer clientW.Close()
			defer serverR.Close()
			defer serverW.Close()
			audio := agent.Attachment{Name: "voice.m4a", Mime: "audio/mp4", Data: []byte("original audio")}
			files, _, cleanup, err := materialize(agent.TurnInput{Options: agent.TurnOptions{Attachments: []agent.Attachment{audio}}})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			inbound := make(chan agent.Inbound, 1)
			ready, consumed := make(chan struct{}), make(chan struct{})
			ack := make(chan error, 1)
			result := make(chan error, 1)
			var events collector
			var steeredPath string
			go func() {
				decoder, encoder := json.NewDecoder(clientR), json.NewEncoder(serverW)
				lookups := 0
				for {
					var request rpcIn
					if err := decoder.Decode(&request); err != nil {
						result <- err
						return
					}
					var response any = map[string]any{}
					switch request.Method {
					case "initialized":
						continue
					case "thread/start":
						response = map[string]any{"thread": map[string]any{"id": "thread"}, "model": "fixture"}
					case "model/list":
						lookups++
						modalities := []string{"text"}
						if native {
							modalities = append(modalities, "audio")
						}
						response = map[string]any{"data": []any{map[string]any{"id": "fixture", "model": "fixture", "inputModalities": modalities}}}
					case "turn/start", "turn/steer":
						var params struct {
							Input []struct{ Type, Text, Path string } `json:"input"`
						}
						if err := json.Unmarshal(request.Params, &params); err != nil {
							result <- err
							return
						}
						if len(params.Input) != 1 {
							result <- fmt.Errorf("unexpected input count %d", len(params.Input))
							return
						}
						block := params.Input[0]
						if native && block.Type != "localAudio" || !native && (block.Type != "text" || !strings.Contains(block.Text, "Attached files:")) {
							result <- fmt.Errorf("wrong routing for native audio=%v: %s", native, block.Type)
							return
						}
						if request.Method == "turn/start" {
							_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"turn": map[string]any{"id": "turn", "status": "inProgress"}}})
							_ = encoder.Encode(map[string]any{"method": "item/started", "params": map[string]any{"item": map[string]any{"id": "initial", "type": "userMessage", "content": params.Input}}})
							close(ready)
							continue
						}
						if native {
							steeredPath = block.Path
							bytes, err := os.ReadFile(block.Path)
							if err != nil || string(bytes) != "original audio" {
								result <- fmt.Errorf("steered audio disappeared before consumption: %v", err)
								return
							}
						}
						_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"turnId": "turn"}})
						select {
						case <-consumed:
						case <-ctx.Done():
							result <- ctx.Err()
							return
						}
						for _, phase := range []string{"started", "completed"} {
							// Use the actual native field casing when echoing the accepted input.
							var input map[string]json.RawMessage
							_ = json.Unmarshal(request.Params, &input)
							_ = encoder.Encode(map[string]any{"method": "item/" + phase, "params": map[string]any{"item": map[string]any{"id": "follow", "type": "userMessage", "content": input["input"]}}})
						}
						_ = encoder.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "turn", "status": "completed", "items": []any{}}}})
						if lookups != 1 {
							result <- fmt.Errorf("queried model %d times", lookups)
						} else {
							result <- nil
						}
						return
					}
					if err := encoder.Encode(map[string]any{"id": request.ID, "result": response}); err != nil {
						result <- err
						return
					}
				}
			}()
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				newAppServer(agent.TurnInput{}, files).converse(ctx, clientW, serverR, inbound, events.emit)
			}()
			select {
			case <-ready:
			case err := <-result:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			input := agent.UserInput{ID: "voice-follow", Status: "queued", Attachments: []agent.Attachment{audio}}
			inbound <- agent.Inbound{Kind: "input", Input: &input, Ack: ack}
			select {
			case err := <-ack:
				if err != nil {
					t.Fatal(err)
				}
			case err := <-result:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			for _, event := range events.snapshot() {
				if event.Input != nil {
					t.Fatal("RPC receipt consumed queued audio before native echo")
				}
			}
			close(consumed)
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			accepted := 0
			for _, event := range events.snapshot() {
				if event.Input != nil && event.Input.ID == input.ID && event.Input.Status == "accepted" {
					accepted++
					if len(event.Input.Attachments) != 1 || !bytes.Equal(event.Input.Attachments[0].Data, audio.Data) {
						t.Fatal("accepted input lost original recording")
					}
				}
			}
			if accepted != 1 {
				t.Fatalf("audio accepted %d times, want exactly once", accepted)
			}
			if steeredPath != "" {
				if _, err := os.Stat(steeredPath); !os.IsNotExist(err) {
					t.Fatal("steered temporary audio survived the turn")
				}
			}
		})
	}
}

func audioTestWAV() []byte {
	var b bytes.Buffer
	const length = 16_000 * 2
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+length))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	for _, value := range []uint16{1, 1} {
		_ = binary.Write(&b, binary.LittleEndian, value)
	}
	for _, value := range []uint32{16_000, 32_000} {
		_ = binary.Write(&b, binary.LittleEndian, value)
	}
	for _, value := range []uint16{2, 16} {
		_ = binary.Write(&b, binary.LittleEndian, value)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(length))
	b.Write(make([]byte, length))
	return b.Bytes()
}
