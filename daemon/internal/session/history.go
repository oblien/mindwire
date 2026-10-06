package session

import (
	"github.com/oblien/mindwire/daemon/internal/agent"
	"time"
)

// An active assistant turn belongs only to the run stream. Keep the native prefix
// (including forked history and its pagination IDs), then the accepted user input.
// Filtering also excludes a reply saved just before the run's terminal status write.
func HistoryBeforeRun(native []agent.Message, recorded []Message, run Run) []agent.Message {
	start, err := time.Parse(time.RFC3339Nano, run.CreatedAt)
	if err != nil {
		return native
	}
	committed := make([]agent.Message, 0, len(native))
	liveInputs := make(map[string]bool, len(run.Inputs))
	for _, input := range run.Inputs {
		liveInputs["input-"+input.ID] = true
	}
	for _, message := range native {
		at, err := time.Parse(time.RFC3339Nano, message.CreatedAt)
		if err == nil && !at.Before(start) {
			break
		}
		committed = append(committed, message)
	}
	for _, message := range recorded {
		at, err := time.Parse(time.RFC3339Nano, message.CreatedAt)
		if message.Role == "user" && err == nil && !at.Before(start) && !liveInputs[message.ID] {
			committed = append(committed, agent.Message(message))
		}
	}
	return committed
}
