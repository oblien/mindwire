package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

type protocolExpectation struct {
	Disposition, Text, Tool, OutputContains, Action, Kind, Title, Detail string
	Event                                                                agent.EventType
	Output                                                               *string
	Paths, Statuses                                                      []string
}

type protocolFixture struct {
	Wire json.RawMessage
	Want protocolExpectation
}

type protocolFixtureSet struct {
	Version              string
	Items, Notifications []protocolFixture
	Errors               []struct {
		Name     string
		Wire     json.RawMessage
		Contains []string
	}
}

func readProtocolFixtures(t *testing.T) protocolFixtureSet {
	t.Helper()
	raw, err := os.ReadFile("testdata/protocol-0.154.0.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures protocolFixtureSet
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	// The fixture file is formatted for review; the protocol carries one compact JSON
	// object per line. Marshal RawMessage to remove formatting before sending frames.
	for _, list := range [][]protocolFixture{fixtures.Items, fixtures.Notifications} {
		for i := range list {
			list[i].Wire, _ = json.Marshal(list[i].Wire)
		}
	}
	for i := range fixtures.Errors {
		fixtures.Errors[i].Wire, _ = json.Marshal(fixtures.Errors[i].Wire)
	}
	return fixtures
}

func assertProtocolOutput(t *testing.T, events []agent.Event, want protocolExpectation) {
	t.Helper()
	if want.Disposition == "input" {
		if len(events) != 0 {
			t.Fatalf("input echoed as assistant activity: %+v", events)
		}
		return
	}
	event := firstOf(events, want.Event)
	if event == nil {
		t.Fatalf("missing %s: %+v", want.Event, events)
	}
	if want.Text != "" && event.Text != want.Text {
		t.Errorf("text=%q, want %q", event.Text, want.Text)
	}
	if want.Tool != "" {
		if event.Tool == nil || event.Tool.Name != want.Tool {
			t.Fatalf("tool=%+v, want %s", event.Tool, want.Tool)
		}
		tool := event.Tool
		if want.Output != nil && tool.Output != *want.Output {
			t.Errorf("output=%q, want %q", tool.Output, *want.Output)
		}
		if want.OutputContains != "" && !strings.Contains(tool.Output, want.OutputContains) {
			t.Errorf("output=%q, missing %q", tool.Output, want.OutputContains)
		}
		if tool.IsError {
			t.Errorf("successful tool flagged as failed: %+v", tool)
		}
		if strings.Contains(tool.Output, "OPAQUE_") {
			t.Errorf("binary/encrypted data rendered as text: %q", tool.Output)
		}
		if want.Action != "" && (tool.Action == nil || string(tool.Action.Kind) != want.Action) {
			t.Fatalf("action=%+v, want %s", tool.Action, want.Action)
		}
		if len(want.Paths) > 0 {
			var paths []string
			for _, file := range tool.Action.Files {
				paths = append(paths, file.Path)
				if file.Diff == "" {
					t.Error("file diff was lost")
				}
			}
			if !reflect.DeepEqual(paths, want.Paths) {
				t.Errorf("paths=%v, want %v", paths, want.Paths)
			}
		}
	}
	if want.Kind != "" {
		inter := event.Interaction
		if inter == nil || inter.Kind != want.Kind {
			t.Fatalf("interaction=%+v, want %s", inter, want.Kind)
		}
		if inter.Detail != want.Detail {
			t.Errorf("detail=%q, want %q", inter.Detail, want.Detail)
		}
		if want.Title != "" && inter.Title != want.Title {
			t.Errorf("title=%q, want %q", inter.Title, want.Title)
		}
		if inter.NeedsResponse {
			t.Error("passive feedback turned into an approval/request")
		}
		if len(want.Statuses) > 0 {
			var statuses []string
			for _, row := range inter.Items {
				statuses = append(statuses, row.Status)
			}
			if !reflect.DeepEqual(statuses, want.Statuses) {
				t.Errorf("todo statuses=%v, want %v", statuses, want.Statuses)
			}
		}
	}
}

// These fixtures are separately validated against the CLI-generated schema by
// testdata/validate_protocol.py. Every current ThreadItem has an explicit disposition.
func TestProtocol154EveryThreadItem(t *testing.T) {
	fixtures := readProtocolFixtures(t)
	if fixtures.Version != "0.154.0" || len(fixtures.Items) != 19 {
		t.Fatal("incorrect schema inventory")
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures.Items {
		var item asItem
		if err := json.Unmarshal(fixture.Wire, &item); err != nil {
			t.Fatal(err)
		}
		if seen[item.Type] {
			t.Fatalf("duplicate type %q", item.Type)
		}
		seen[item.Type] = true
		t.Run(item.Type, func(t *testing.T) {
			st, col := newStreamState(), &collector{}
			emitItem(phaseCompleted, fixture.Wire, col.emit, st)
			assertProtocolOutput(t, col.snapshot(), fixture.Want)
			count := len(col.snapshot())
			emitItem(phaseCompleted, fixture.Wire, col.emit, st)
			if len(col.snapshot()) != count {
				t.Fatal("repeated final snapshot duplicated a component")
			}
			if st.decodeError != "" {
				t.Fatal(st.decodeError)
			}
		})
	}
}

func TestProtocol154FinalTurnContainsEveryItem(t *testing.T) {
	fixtures := readProtocolFixtures(t)
	var items []json.RawMessage
	for _, fixture := range fixtures.Items {
		items = append(items, fixture.Wire)
	}
	terminal, _ := json.Marshal(map[string]any{"method": "turn/completed", "params": map[string]any{
		"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed", "error": nil, "items": items}}})
	events, result, got := protocolTurn(t, string(terminal))
	if !got || result.IsError || result.Text != "The project is ready." {
		t.Fatalf("result=%+v got=%v", result, got)
	}
	for _, fixture := range fixtures.Items {
		var item asItem
		_ = json.Unmarshal(fixture.Wire, &item)
		t.Run(item.Type, func(t *testing.T) {
			var related []agent.Event
			for _, event := range events {
				if event.ItemID == item.ID || event.Tool != nil && event.Tool.ID == item.ID || event.Interaction != nil && event.Interaction.ID == item.ID {
					related = append(related, event)
				}
			}
			assertProtocolOutput(t, related, fixture.Want)
		})
	}
}

func TestProtocol154ActivityAndProblemNotifications(t *testing.T) {
	for _, fixture := range readProtocolFixtures(t).Notifications {
		var wire rpcIn
		_ = json.Unmarshal(fixture.Wire, &wire)
		t.Run(wire.Method, func(t *testing.T) {
			events, result, got := protocolTurn(t, string(fixture.Wire),
				`{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","items":[{"id":"answer","type":"agentMessage","text":"Done."}]}}}`)
			if !got || result.IsError {
				t.Fatalf("notice incorrectly failed the turn: %+v got=%v", result, got)
			}
			assertProtocolOutput(t, events, fixture.Want)
		})
	}
}

func TestProtocol154EveryErrorVariantKeepsPartialOutputAndDetails(t *testing.T) {
	for _, fixture := range readProtocolFixtures(t).Errors {
		t.Run(fixture.Name, func(t *testing.T) {
			failure := fmt.Sprintf(`{"method":"error","params":{"threadId":"thread-1","turnId":"turn-1","error":%s,"willRetry":false}}`, fixture.Wire)
			terminal := fmt.Sprintf(`{"method":"turn/completed","params":{"turn":{"id":"turn-1","status":"failed","items":[],"error":%s}}}`, fixture.Wire)
			events, result, got := protocolTurn(t,
				`{"method":"item/agentMessage/delta","params":{"itemId":"answer","delta":"Partial answer"}}`, failure, terminal)
			if !got || !result.IsError {
				t.Fatalf("failure became success: %+v got=%v", result, got)
			}
			for _, text := range fixture.Contains {
				if !strings.Contains(result.Text, text) {
					t.Errorf("result=%q missing %q", result.Text, text)
				}
			}
			if countType(events, agent.EventResult) != 1 || firstOf(events, agent.EventText).Text != "Partial answer" {
				t.Fatal("lost partial answer or duplicated terminal result")
			}
			_, execResult, execGot := feed(t, `{"type":"turn.failed","error":`+string(fixture.Wire)+`}`)
			if !execGot || !execResult.IsError || execResult.Text != result.Text {
				t.Fatalf("transports disagree: exec=%+v app-server=%+v", execResult, result)
			}
		})
	}
}
