package codex

import "github.com/oblien/mindwire/daemon/internal/agent"

func mergeRecordedHistory(native, recorded []agent.Message) []agent.Message {
	return agent.MergeRecordedHistory(agent.NormalizeSurfaceHistory(native), recorded)
}

func sameHistoryPart(a, b agent.Part) bool { return agent.SameHistoryPart(a, b) }
