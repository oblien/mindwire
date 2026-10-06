package claude

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func TestQueuedInputKeepsNativeStreamOpenAcrossIntermediateResult(t *testing.T) {
	s := newControlSession()
	defer s.close()
	acks := []chan error{make(chan error, 1), make(chan error, 1)}
	var uuids []string
	for i, id := range []string{"one", "two"} {
		input := agent.UserInput{ID: id, Text: "Again", Status: "queued"}
		line, ok := s.encode(agent.Inbound{Kind: "input", Text: input.Text, Input: &input, Ack: acks[i]})
		if !ok {
			t.Fatal("input could not be encoded")
		}
		var message struct {
			UUID string `json:"uuid"`
		}
		if err := json.Unmarshal(line, &message); err != nil {
			t.Fatal(err)
		}
		uuids = append(uuids, message.UUID)
	}
	if uuids[0] == uuids[1] {
		t.Fatal("distinct inputs shared a native identity")
	}
	lines := []string{`{"type":"result","subtype":"success","result":"First answer"}`}
	for _, uuid := range []string{uuids[0], uuids[0], uuids[1]} {
		lines = append(lines, `{"type":"user","isReplay":true,"uuid":"`+uuid+`","message":{"role":"user","content":"Again"}}`)
	}
	lines = append(lines, `{"type":"result","subtype":"success","result":"Final answer"}`)
	var events []agent.Event
	result, got := s.parse(strings.NewReader(strings.Join(lines, "\n")), func(event agent.Event) { events = append(events, event) })
	if !got || result.Text != "Final answer" {
		t.Fatalf("stopped at an intermediate result: %+v", result)
	}
	var order []string
	for _, event := range events {
		if event.Input != nil {
			order = append(order, event.Input.ID)
		}
	}
	if strings.Join(order, ",") != "one,two" {
		t.Fatalf("native echoes were not correlated: %v", order)
	}
	for _, ack := range acks {
		select {
		case err := <-ack:
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("native input did not acknowledge")
		}
	}
	closed := make(chan error, 1)
	if _, ok := s.encode(agent.Inbound{Kind: "input", Text: "Too late", Ack: closed}); ok {
		t.Fatal("terminal stream accepted new input")
	}
	if err := <-closed; !errors.Is(err, agent.ErrInputClosed) {
		t.Fatalf("unsafe finish-boundary acknowledgement: %v", err)
	}
}
