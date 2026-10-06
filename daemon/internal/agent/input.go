package agent

import "errors"

// UserInput is a follow-up owned by the running conversation, not by a connected
// client. Its ID is also the retry receipt. Native transcripts remain authoritative.
type UserInput struct {
	ID          string       `json:"id"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments,omitempty"`
	CreatedAt   string       `json:"createdAt"`
	Status      string       `json:"status"` // queued | accepted | failed | cancelled
	Error       string       `json:"error,omitempty"`
}

// AcknowledgeInput publishes acceptance in native event order before releasing
// the caller. ErrInputClosed proves nothing was submitted and permits a safe
// continuation; an ambiguous transport failure must never be retried automatically.
func AcknowledgeInput(in Inbound, err error, emit Emit) {
	if in.Input != nil && !errors.Is(err, ErrInputClosed) {
		input := *in.Input
		input.Status = "accepted"
		if err != nil {
			input.Status, input.Error = "failed", err.Error()
		}
		emit(Event{Type: EventInput, Input: &input})
	}
	if in.Ack != nil {
		in.Ack <- err
	}
}
