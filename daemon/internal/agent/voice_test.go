package agent

import (
	"context"
	"testing"
	"time"
)

func TestVoiceBridgeOrderedOnceAndBounded(t *testing.T) {
	v := NewVoiceBridge("voice-test-client")
	defer v.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	audio := VoiceAudio{SampleRate: 24000, Channels: 1, Data: []byte{1, 0, 2, 0}}
	first := make(chan error, 1)
	go func() { first <- v.Send(ctx, 1, audio) }()
	command := <-v.Commands
	duplicate := make(chan error, 1)
	go func() { duplicate <- v.Send(ctx, 1, audio) }()
	command.Ack <- nil
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-duplicate; err != nil {
		t.Fatal(err)
	}
	if len(v.Commands) != 0 {
		t.Fatal("retry repeated microphone audio")
	}
	if err := v.Send(ctx, 3, audio); err == nil {
		t.Fatal("accepted missing audio batch")
	}
	changed := audio
	changed.Data = []byte{2, 0}
	if err := v.Send(ctx, 1, changed); err == nil {
		t.Fatal("accepted reused sequence with different audio")
	}
	if err := v.Send(ctx, 2, VoiceAudio{SampleRate: 24000, Channels: 1, Data: make([]byte, 24002)}); err == nil {
		t.Fatal("accepted unbounded audio")
	}
	_, stream, detach, err := v.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	for i := 0; i < 25; i++ {
		v.Publish(VoiceEvent{Type: "audio", Audio: &audio})
	}
	select {
	case <-v.Stop:
	default:
		t.Fatal("slow voice listener was not stopped")
	}
	for range stream {
	} // drained, then closed; no retained audio replay
	if err := v.Send(ctx, 2, audio); err == nil {
		t.Fatal("audio accepted after voice stopped")
	}
}
