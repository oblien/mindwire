package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Voice describes the harness interface, independently of a coding model's audio
// attachment modalities. Local-only dictation is not a remote microphone API.
type VoiceCapability struct {
	Mode    string  `json:"mode"` // conversation, dictation, or none
	Support Support `json:"support"`
	Remote  bool    `json:"remote"`
	Detail  string  `json:"detail,omitempty"`
}

type VoiceStatus struct {
	Capability VoiceCapability `json:"capability"`
	Available  bool            `json:"available"`
	Code       string          `json:"code,omitempty"`
	Detail     string          `json:"detail,omitempty"`
	Voices     []string        `json:"voices,omitempty"`
}

// VoiceModule probes the selected harness on demand. This must not start a voice
// session, record audio, change authentication, or call the coding model.
type VoiceModule interface {
	VoiceStatus(context.Context, CredStore) (VoiceStatus, error)
}

type VoiceOptions struct {
	ClientID string `json:"clientId"`
	Voice    string `json:"voice,omitempty"`
}

// PCM16 little-endian, mono, 24 kHz. Native adapters resample only when their
// protocol requires it. Audio bytes never enter the durable transcript/event hub.
type VoiceAudio struct {
	Data       []byte `json:"data"`
	SampleRate int    `json:"sampleRate"`
	Channels   int    `json:"channels"`
}

type VoiceEvent struct {
	Type   string      `json:"type"` // ready, audio, transcript, listening, error, closed
	Audio  *VoiceAudio `json:"audio,omitempty"`
	Text   string      `json:"text,omitempty"`
	Role   string      `json:"role,omitempty"`
	ItemID string      `json:"itemId,omitempty"`
	Final  bool        `json:"final,omitempty"`
}

type VoiceCommand struct {
	Audio *VoiceAudio
	Ack   chan error
}

var ErrVoiceClosed = errors.New("This voice conversation has ended. Start voice again to reconnect.")

// VoiceBridge is a bounded, ephemeral media channel owned by one live run. A
// disconnect ends microphone access, not the coding task already in progress.
// No idle daemon polling, replay of spoken commands, or audio files are involved.
type VoiceBridge struct {
	ClientID string
	Commands chan VoiceCommand
	Stop     <-chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	closed   bool
	state    VoiceEvent
	sub      chan VoiceEvent
	seq      int64
	digest   [32]byte
	pending  *voiceReceipt
	lease    *time.Timer
}

type voiceReceipt struct {
	done chan struct{}
	err  error
}

func NewVoiceBridge(clientID string) *VoiceBridge {
	stop := make(chan struct{})
	v := &VoiceBridge{ClientID: clientID, Commands: make(chan VoiceCommand, 1), stop: stop, Stop: stop,
		state: VoiceEvent{Type: "connecting"}}
	// A client which loses the POST response must not leave an orphan microphone
	// session. Once attached, heartbeats/audio renew this short media lease.
	v.lease = time.AfterFunc(30*time.Second, v.EndInput)
	return v
}

func (v *VoiceBridge) EndInput() { v.stopOnce.Do(func() { close(v.stop) }) }

func (v *VoiceBridge) InputOpen() bool {
	select {
	case <-v.Stop:
		return false
	default:
		return true
	}
}

func (v *VoiceBridge) Touch() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return ErrVoiceClosed
	}
	select {
	case <-v.Stop:
		return ErrVoiceClosed
	default:
	}
	v.lease.Reset(20 * time.Second)
	return nil
}

func (v *VoiceBridge) Publish(event VoiceEvent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return
	}
	if event.Type == "ready" || event.Type == "error" || event.Type == "closed" {
		v.state = event
	}
	if v.sub == nil {
		return
	} // media has no replay buffer
	select {
	case v.sub <- event:
	default:
		// A slow client must stop instead of hearing stale audio or dropping words.
		close(v.sub)
		v.sub = nil
		v.EndInput()
	}
}

func (v *VoiceBridge) Subscribe() (VoiceEvent, <-chan VoiceEvent, func(), error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed || !v.InputOpen() {
		return v.state, nil, func() {}, ErrVoiceClosed
	}
	if v.sub != nil {
		return VoiceEvent{}, nil, func() {}, errors.New("Voice is already open on another connection.")
	}
	ch := make(chan VoiceEvent, 24)
	v.sub = ch
	v.lease.Reset(20 * time.Second)
	return v.state, ch, func() {
		v.mu.Lock()
		if v.sub == ch {
			close(ch)
			v.sub = nil
		}
		v.mu.Unlock()
		v.EndInput()
	}, nil
}

func (v *VoiceBridge) Close() {
	v.EndInput()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return
	}
	v.closed = true
	v.lease.Stop()
	if v.sub != nil {
		close(v.sub)
		v.sub = nil
	}
}

// Send accepts strictly ordered microphone batches. The receipt is recorded
// before enqueueing, so a lost HTTP acknowledgement cannot duplicate speech.
func (v *VoiceBridge) Send(ctx context.Context, sequence int64, audio VoiceAudio) error {
	if audio.SampleRate != 24000 || audio.Channels != 1 || len(audio.Data) == 0 || len(audio.Data)%2 != 0 || len(audio.Data) > 24000 {
		return errors.New("Voice requires PCM16 mono audio at 24 kHz, at most 500 ms per batch.")
	}
	digest := sha256.Sum256(audio.Data)
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return ErrVoiceClosed
	}
	select {
	case <-v.Stop:
		v.mu.Unlock()
		return ErrVoiceClosed
	default:
	}
	if sequence == v.seq && sequence > 0 {
		if digest != v.digest {
			v.mu.Unlock()
			return errors.New("Voice sequence was reused for different audio.")
		}
		pending := v.pending
		v.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pending.done:
		}
		return pending.err
	}
	if sequence != v.seq+1 {
		expected := v.seq + 1
		v.mu.Unlock()
		return fmt.Errorf("Voice audio arrived out of order; expected batch %d.", expected)
	}
	if v.pending != nil {
		select {
		case <-v.pending.done:
		default:
			v.mu.Unlock()
			return errors.New("The previous voice batch is still being delivered.")
		}
	}
	ack := make(chan error, 1)
	v.seq, v.digest, v.pending = sequence, digest, &voiceReceipt{done: make(chan struct{})}
	pending := v.pending
	v.lease.Reset(20 * time.Second)
	v.mu.Unlock()
	command := VoiceCommand{Audio: &audio, Ack: ack}
	var err error
	select {
	case v.Commands <- command:
		select {
		case err = <-ack:
		case <-v.Stop:
			err = ErrVoiceClosed
		case <-ctx.Done():
			err = ctx.Err()
		}
	case <-v.Stop:
		err = ErrVoiceClosed
	case <-ctx.Done():
		err = ctx.Err()
	}
	v.mu.Lock()
	pending.err = err
	close(pending.done)
	v.mu.Unlock()
	if err != nil {
		v.EndInput()
	}
	return err
}
