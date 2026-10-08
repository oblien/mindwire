package mindwire

import (
	"context"
	"iter"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// VoiceStatus checks the selected harness without recording or starting a turn.
func (c *Client) VoiceStatus(ctx context.Context, opts ...ScopedOption) (VoiceStatus, error) {
	a, err := c.resolve(opts)
	if err != nil {
		return VoiceStatus{}, err
	}
	if module, ok := a.Adapter.(agent.VoiceModule); ok {
		return module.VoiceStatus(ctx, a.Creds)
	}
	cap := a.Capabilities().Voice
	if cap == nil {
		cap = &agent.VoiceCapability{Mode: "none", Support: agent.SupportNone}
	}
	return VoiceStatus{Capability: *cap, Code: "unsupported", Detail: cap.Detail}, nil
}

// VoiceAudio sends an ordered PCM batch to a run started with Options.Voice.
func (r *Run) VoiceAudio(ctx context.Context, clientID string, sequence int64, audio VoiceAudio) error {
	v, ok := r.core.sup.Voice(r.ID(), clientID)
	if !ok {
		return agent.ErrVoiceClosed
	}
	return v.Send(ctx, sequence, audio)
}

// EndVoice stops the microphone conversation while any coding work continues.
func (r *Run) EndVoice(clientID string) error {
	v, ok := r.core.sup.Voice(r.ID(), clientID)
	if !ok {
		return agent.ErrVoiceClosed
	}
	v.EndInput()
	return nil
}

// VoiceEvents has no audio replay buffer. Subscribe before sending microphone
// audio. Ending iteration releases voice access without cancelling coding work.
func (r *Run) VoiceEvents(ctx context.Context, clientID string) iter.Seq2[VoiceEvent, error] {
	return func(yield func(VoiceEvent, error) bool) {
		v, ok := r.core.sup.Voice(r.ID(), clientID)
		if !ok {
			yield(VoiceEvent{}, agent.ErrVoiceClosed)
			return
		}
		state, events, detach, err := v.Subscribe()
		if err != nil {
			yield(VoiceEvent{}, err)
			return
		}
		defer detach()
		if !yield(state, nil) {
			return
		}
		heartbeat := time.NewTicker(5 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-events:
				if !open || !yield(event, nil) || event.Type == "closed" || event.Type == "error" {
					return
				}
			case <-heartbeat.C:
				if err := v.Touch(); err != nil {
					return
				}
			}
		}
	}
}
