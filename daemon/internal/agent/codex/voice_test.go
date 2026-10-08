package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestNativeVoiceCanStopDuringHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()
	defer clientR.Close()
	defer clientW.Close()
	defer serverR.Close()
	defer serverW.Close()
	voice := agent.NewVoiceBridge("voice-handshake-client")
	defer voice.Close()
	go func() {
		// The native process never responds to initialize. Ending voice must
		// release its admission without waiting for a coding turn to exist.
		scan := bufio.NewScanner(clientR)
		if scan.Scan() {
			voice.EndInput()
		}
	}()
	a := appServer{voice: voice}
	r, ok := a.converse(ctx, clientW, serverR, make(chan agent.Inbound), func(agent.Event) {})
	if ctx.Err() != nil || !ok || r.IsError {
		t.Fatalf("voice startup did not stop cleanly: %+v, %v", r, ctx.Err())
	}
}

func TestNativeVoiceHistoryKeepsSpokenAndCodingReplies(t *testing.T) {
	rows := []string{
		`{"timestamp":"2026-10-07T12:00:00Z","type":"realtime_item","payload":{"type":"transcript_segment","id":"voice-user","realtime_session_id":"voice-session","role":"user","text":"Make the change."}}`,
		`{"timestamp":"2026-10-07T12:00:01Z","type":"realtime_item","payload":{"type":"transcript_segment","id":"voice-answer","realtime_session_id":"voice-session","role":"assistant","text":"Working on it."}}`,
		`{"timestamp":"2026-10-07T12:00:01Z","type":"realtime_item","payload":{"type":"transcript_segment","id":"voice-answer","realtime_session_id":"voice-session","role":"assistant","text":"Working on it."}}`,
		`{"timestamp":"2026-10-07T12:00:02Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"code-answer","content":[{"type":"output_text","text":"Code finished."}]}}}`,
	}
	messages, err := parseRollout(strings.NewReader(strings.Join(rows, "\n")), "chat")
	if err != nil || len(messages) != 3 || messages[0].Text != "Make the change." || messages[1].Text != "Working on it." || messages[2].Text != "Code finished." {
		t.Fatalf("native voice timeline was lost or duplicated: %+v, %v", messages, err)
	}
	if messages[0].CanFork || messages[0].ForkPoint != nil {
		t.Fatal("a realtime item is not a native coding-turn fork boundary")
	}
	merged := agent.MergeRecordedHistory(messages, messages)
	if len(merged) != len(messages) {
		t.Fatal("stream reconciliation duplicated native voice")
	}
}

// Exercise the real adapter state machine: native voice RPCs, ordered PCM,
// normal coding events, and ending voice both before and after coding completes.
func TestNativeVoiceLifecycle(t *testing.T) {
	for _, stopDuringWork := range []bool{true, false} {
		name := "after_task"
		if stopDuringWork {
			name = "during_task"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			clientR, clientW := io.Pipe()
			serverR, serverW := io.Pipe()
			defer clientR.Close()
			defer clientW.Close()
			defer serverR.Close()
			defer serverW.Close()
			voice := agent.NewVoiceBridge("native-voice-client")
			defer voice.Close()
			_, media, unsubscribe, err := voice.Subscribe()
			if err != nil {
				t.Fatal(err)
			}
			defer unsubscribe()
			audioSeen := make(chan agent.VoiceAudio, 1)
			stopSeen := make(chan struct{})
			finishWork := make(chan struct{})
			workObserved := make(chan struct{}, 1)
			continued := make(chan struct{}, 1)
			serverError := make(chan string, 1)
			go func() {
				scan := bufio.NewScanner(clientR)
				write := func(value any) { _ = json.NewEncoder(serverW).Encode(value) }
				notify := func(method string, params any) { write(map[string]any{"method": method, "params": params}) }
				complete := func() {
					notify("turn/completed", map[string]any{"threadId": "voice-thread", "turn": map[string]any{
						"id": "coding-1", "status": "completed", "items": []any{map[string]any{"id": "answer-1", "type": "agentMessage", "text": "Coding finished."}},
					}})
				}
				for scan.Scan() {
					var req rpcIn
					_ = json.Unmarshal(scan.Bytes(), &req)
					reply := func(result any) { write(map[string]any{"id": req.ID, "result": result}) }
					switch req.Method {
					case "initialize":
						reply(map[string]any{})
					case "initialized":
					case "thread/start":
						reply(map[string]any{"thread": map[string]any{"id": "voice-thread"}})
					case "thread/realtime/start":
						var p map[string]any
						_ = json.Unmarshal(req.Params, &p)
						if p["outputModality"] != "audio" || p["model"] != nil {
							serverError <- "voice must use its own configured model and audio output"
						}
						reply(map[string]any{})
						notify("thread/realtime/started", map[string]any{"threadId": "voice-thread", "version": "v3"})
					case "thread/realtime/appendAudio":
						var p struct {
							Audio struct {
								Data       []byte `json:"data"`
								SampleRate int    `json:"sampleRate"`
								Channels   int    `json:"numChannels"`
							} `json:"audio"`
						}
						_ = json.Unmarshal(req.Params, &p)
						audioSeen <- agent.VoiceAudio{Data: p.Audio.Data, SampleRate: p.Audio.SampleRate, Channels: p.Audio.Channels}
						reply(map[string]any{})
						notify("thread/realtime/item/completed", map[string]any{"threadId": "voice-thread", "item": map[string]any{"id": "speech-1", "type": "transcriptSegment", "role": "user", "text": "Please make the change."}})
						notify("turn/started", map[string]any{"threadId": "voice-thread", "turn": map[string]any{"id": "coding-1"}})
						notify("item/started", map[string]any{"threadId": "voice-thread", "item": map[string]any{"id": "work-1", "type": "commandExecution", "command": "echo working", "status": "inProgress"}})
						if !stopDuringWork {
							complete()
						}
					case "thread/realtime/stop":
						reply(map[string]any{})
						notify("thread/realtime/closed", map[string]any{"threadId": "voice-thread"})
						close(stopSeen)
						if stopDuringWork {
							select {
							case <-finishWork:
								complete()
							case <-ctx.Done():
								return
							}
						}
					case "turn/interrupt", "turn/start":
						serverError <- "voice incorrectly interrupted or started a typed turn"
						return
					}
				}
			}()
			result := make(chan agent.TurnResult, 1)
			col := &collector{}
			go func() {
				a := appServer{sandbox: "danger-full-access", approval: "on-request", voice: voice, voiceOptions: &agent.VoiceOptions{ClientID: voice.ClientID}}
				r, _ := a.converse(ctx, clientW, serverR, make(chan agent.Inbound), func(ev agent.Event) {
					col.emit(ev)
					if ev.Type == agent.EventToolUse {
						select {
						case workObserved <- struct{}{}:
						default:
						}
					}
					if ev.Type == agent.EventContinuation {
						select {
						case continued <- struct{}{}:
						default:
						}
					}
				})
				result <- r
			}()
			select {
			case event := <-media:
				if event.Type != "ready" {
					t.Fatalf("unexpected voice state: %v", event)
				}
			case <-ctx.Done():
				t.Fatal("voice did not start")
			}
			if err := voice.Send(ctx, 1, agent.VoiceAudio{Data: []byte{1, 0, 2, 0}, SampleRate: 24000, Channels: 1}); err != nil {
				t.Fatal(err)
			}
			select {
			case a := <-audioSeen:
				if string(a.Data) != string([]byte{1, 0, 2, 0}) || a.SampleRate != 24000 || a.Channels != 1 {
					t.Fatal("PCM changed")
				}
			case <-ctx.Done():
				t.Fatal("audio missing")
			}
			if stopDuringWork {
				select {
				case <-workObserved:
				case <-ctx.Done():
					t.Fatal("coding work missing")
				}
			} else {
				select {
				case <-continued:
				case <-ctx.Done():
					t.Fatal("voice ended at coding completion")
				}
			}
			voice.EndInput()
			select {
			case <-stopSeen:
			case <-ctx.Done():
				t.Fatal("native voice stop missing")
			}
			if stopDuringWork {
				select {
				case <-result:
					t.Fatal("ending voice killed active coding work")
				default:
				}
				close(finishWork)
			}
			select {
			case r := <-result:
				if r.IsError || r.Text != "Coding finished." {
					t.Fatalf("unexpected result: %+v", r)
				}
			case <-ctx.Done():
				t.Fatal("voice run did not finish")
			}
			select {
			case err := <-serverError:
				t.Fatal(err)
			default:
			}
			user := firstOfType(col.snapshot(), agent.EventInput)
			if user == nil || user.Input.Text != "Please make the change." {
				t.Fatal("native voice transcript missing")
			}
		})
	}
}
