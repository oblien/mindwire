package codex

import "github.com/oblien/mindwire/daemon/internal/agent"

func mergeRecordedHistory(native, recorded []agent.Message) []agent.Message {
	forms := map[string]agent.Interaction{}
	for _, message := range native {
		for _, part := range message.Parts {
			if part.Interaction != nil && part.Interaction.IsMessageQuestion() {
				forms[part.Interaction.ID] = *part.Interaction
			}
		}
	}
	if len(forms) > 0 {
		recorded = append([]agent.Message(nil), recorded...)
		for i := range recorded {
			recorded[i].Parts = agent.UpgradeAsyncQuestionParts(recorded[i].Parts, forms)
		}
	}
	return agent.MergeRecordedHistory(agent.NormalizeSurfaceHistory(native), recorded)
}

func sameHistoryPart(a, b agent.Part) bool { return agent.SameHistoryPart(a, b) }
