package agent

import "fmt"

const ChatForkVersion = 1

// ForkPoint identifies a native history boundary immediately BEFORE a human
// message. ResumeAt is an opaque harness cursor (a turn ID or a message UUID).
// It is prepared from native history, never accepted from a client's options.
type ForkPoint struct {
	BeforeMessageID string `json:"beforeMessageId,omitempty"`
	ResumeAt        string `json:"resumeAt,omitempty"`
	Fresh           bool   `json:"fresh,omitempty"`
}

func PrepareFork(adapter Adapter, query HistoryQuery, before string) (ForkPoint, error) {
	if !adapter.Capabilities().Fork {
		return ForkPoint{}, fmt.Errorf("this harness does not support conversation forks")
	}
	if before == "" {
		return ForkPoint{}, nil
	}
	messages, err := adapter.History(query)
	if err != nil {
		return ForkPoint{}, err
	}
	for _, message := range messages {
		if message.ID != before {
			continue
		}
		if message.Role != "user" || message.ForkPoint == nil {
			return ForkPoint{}, fmt.Errorf("this message has no safe native fork boundary; choose the first message of its turn")
		}
		point := *message.ForkPoint
		point.BeforeMessageID = before
		return point, nil
	}
	return ForkPoint{}, fmt.Errorf("message is no longer in this conversation; refresh its history")
}

// ForkHistory prevents a deferred fork from showing the source's discarded
// suffix. Recorded input belongs to the NEW chat and is merged by the caller
// after this cut. A missing boundary fails closed instead of showing the future.
func ForkHistory(messages []Message, point *ForkPoint) ([]Message, error) {
	if point == nil || point.BeforeMessageID == "" {
		return messages, nil
	}
	for i, message := range messages {
		if message.ID == point.BeforeMessageID {
			return messages[:i], nil
		}
	}
	return nil, fmt.Errorf("the fork's original message is no longer available")
}
