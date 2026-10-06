package session

import (
	"reflect"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestActiveHistoryLeavesConsumedFollowupsInTheRunSnapshot(t *testing.T) {
	run := Run{ID: "run", CreatedAt: "2026-10-06T10:00:00Z", Inputs: []agent.UserInput{{ID: "follow", Status: "accepted"}}}
	prior := agent.Message{ID: "native-prior", Role: "assistant", Text: "Prior turn", CreatedAt: "2026-10-06T09:00:00Z"}
	initial := Message{ID: "initial", Role: "user", Text: "Start", CreatedAt: "2026-10-06T10:00:00Z"}
	follow := Message{ID: "input-follow", Role: "user", Text: "Follow up", CreatedAt: "2026-10-06T10:00:01Z"}
	recorded := []Message{Message(prior), initial, follow,
		{ID: "reply", Role: "assistant", Text: "Live work", CreatedAt: "2026-10-06T10:00:05Z"}}
	native := []agent.Message{prior, agent.Message(initial),
		{ID: "native-follow", Role: "user", Text: follow.Text, CreatedAt: "2026-10-06T10:00:04Z"}}
	for _, source := range [][]agent.Message{native, {prior, agent.Message(initial), agent.Message(follow)}} {
		got := HistoryBeforeRun(source, recorded, run)
		if !reflect.DeepEqual(got, []agent.Message{prior, agent.Message(initial)}) {
			t.Fatalf("active history duplicates snapshot-owned messages: %+v", got)
		}
	}
}
