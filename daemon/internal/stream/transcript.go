package stream

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// Snapshot is a materialized turn at an event cursor. Reading it and then subscribing
// after Sequence restores a turn without replaying its animation or losing new events.
type Snapshot struct {
	Sequence      int64             `json:"sequence"`
	Parts         []agent.Part      `json:"parts"`
	Result        *agent.ResultInfo `json:"result,omitempty"`
	Error         string            `json:"error,omitempty"`
	StatusMessage string            `json:"statusMessage,omitempty"`
}

// Transcript is the shared event reducer for persistence and live snapshots, for every
// harness. Callers serialize access. Parts and tool values are copied before mutation so
// a previously returned snapshot remains stable while the next event is applied.
type Transcript struct {
	state        Snapshot
	items        map[string]int
	tools        map[string]int
	interactions map[string]int
	toolStarted  map[string]time.Time
	thinking     int
	thinkStarted time.Time
}

func (t *Transcript) Apply(ev agent.Event) {
	if t.items == nil {
		t.items, t.tools, t.interactions = map[string]int{}, map[string]int{}, map[string]int{}
		t.toolStarted = map[string]time.Time{}
		t.thinking = -1
	}
	now, err := time.Parse(time.RFC3339Nano, ev.At)
	if err != nil {
		now = time.Now().UTC()
	}
	t.state.Sequence = ev.Sequence
	switch ev.Type {
	case agent.EventText, agent.EventThinking:
		t.state.StatusMessage = ""
		kind := string(ev.Type)
		i, exists := t.items[ev.ItemID]
		if ev.ItemID == "" {
			i = len(t.state.Parts) - 1
			exists = i >= 0 && t.state.Parts[i].Type == kind
		}
		if !exists {
			if ev.Text == "" {
				break
			}
			t.finishThinking(now)
			i = len(t.state.Parts)
			t.state.Parts = append(t.state.Parts, agent.Part{Type: kind, ID: ev.ItemID, At: ev.At})
			if ev.ItemID != "" {
				t.items[ev.ItemID] = i
			}
			if ev.Type == agent.EventThinking {
				t.thinking, t.thinkStarted = i, now
			}
		} else if ev.Type == agent.EventText {
			t.finishThinking(now)
		}
		if ev.Delta || ev.ItemID == "" {
			t.state.Parts[i].Text += ev.Text
		} else {
			t.state.Parts[i].Text = ev.Text
		}
		if ev.Tokens > 0 {
			t.state.Parts[i].Tokens = ev.Tokens
		}
	case agent.EventToolUse, agent.EventToolResult:
		t.finishThinking(now)
		t.state.StatusMessage = ""
		if ev.Tool == nil {
			break
		}
		i, exists := t.tools[ev.Tool.ID]
		if ev.Tool.ID == "" || !exists {
			i = len(t.state.Parts)
			t.state.Parts = append(t.state.Parts, agent.Part{Type: "tool", At: ev.At, Tool: &agent.ToolPart{ID: ev.Tool.ID}})
			if ev.Tool.ID != "" {
				t.tools[ev.Tool.ID], t.toolStarted[ev.Tool.ID] = i, now
			}
		}
		tool := *t.state.Parts[i].Tool
		if ev.Tool.Name != "" {
			tool.Name = ev.Tool.Name
		}
		if ev.Tool.Input != nil {
			tool.Input = rawInput(ev.Tool.Input)
		}
		if ev.Tool.Action != nil {
			tool.Action = ev.Tool.Action
		}
		if ev.Tool.Output != "" || ev.Type == agent.EventToolResult {
			tool.Output = ev.Tool.Output
		}
		if ev.Type == agent.EventToolResult {
			tool.IsError = ev.Tool.IsError
			if start, ok := t.toolStarted[ev.Tool.ID]; ok {
				t.state.Parts[i].DurationMs = int(now.Sub(start).Milliseconds())
			}
		}
		t.state.Parts[i].Tool = &tool
	case agent.EventInteraction:
		t.finishThinking(now)
		if ev.Interaction != nil {
			if i, ok := t.interactions[ev.Interaction.ID]; ev.Interaction.ID != "" && ok {
				t.state.Parts[i].Interaction = ev.Interaction
			} else {
				if ev.Interaction.ID != "" {
					t.interactions[ev.Interaction.ID] = len(t.state.Parts)
				}
				t.state.Parts = append(t.state.Parts, agent.Part{Type: "interaction", At: ev.At, Interaction: ev.Interaction})
			}
		}
	case agent.EventCompaction:
		t.finishThinking(now)
		t.state.Parts = append(t.state.Parts, agent.Part{Type: "compaction", At: ev.At, Compaction: ev.Compaction})
	case agent.EventError:
		t.state.Error = ev.Error
	case agent.EventResult:
		for i, part := range t.state.Parts {
			if part.Interaction != nil && !part.Interaction.IsMessageQuestion() {
				it := *part.Interaction
				it.NeedsResponse = false
				t.state.Parts[i].Interaction = &it
			}
		}
		t.finishThinking(now)
		t.state.Result, t.state.StatusMessage = ev.Result, ""
		if ev.Result != nil {
			t.state.Error = ""
			if ev.Result.IsError {
				t.state.Error = ev.Result.Text
			} else {
				// Some harnesses return text only in the terminal result. Materialize it
				// once so snapshots, recorded history and the live feed have the same parts.
				var text strings.Builder
				for _, part := range t.state.Parts {
					if part.Type == "text" {
						text.WriteString(part.Text)
					}
				}
				final := strings.TrimSpace(ev.Result.Text)
				if final != "" && !strings.HasSuffix(strings.TrimSpace(text.String()), final) {
					last := len(t.state.Parts) - 1
					if last >= 0 && t.state.Parts[last].Type == "text" && strings.HasPrefix(final, strings.TrimSpace(t.state.Parts[last].Text)) {
						t.state.Parts[last].Text = ev.Result.Text
					} else {
						t.state.Parts = append(t.state.Parts, agent.Part{Type: "text", Text: ev.Result.Text, At: ev.At})
					}
				}
			}
		}
	case agent.EventStatus:
		if retry, _ := ev.Meta["willRetry"].(bool); retry {
			t.state.StatusMessage, _ = ev.Meta["message"].(string)
		}
	case agent.EventContinuation:
		t.finishThinking(now)
		t.state.Result = nil
	}
}

func (t *Transcript) finishThinking(now time.Time) {
	if !t.thinkStarted.IsZero() {
		t.state.Parts[t.thinking].DurationMs = int(now.Sub(t.thinkStarted).Milliseconds())
		t.thinkStarted = time.Time{}
	}
}

func (t *Transcript) Snapshot(final bool) Snapshot {
	snapshot := t.state
	snapshot.Parts = append([]agent.Part{}, t.state.Parts...)
	if final {
		for i, part := range snapshot.Parts {
			if part.Interaction != nil && !part.Interaction.IsMessageQuestion() {
				it := *part.Interaction
				it.NeedsResponse = false
				snapshot.Parts[i].Interaction = &it
			}
		}
	}
	if final && !t.thinkStarted.IsZero() {
		snapshot.Parts[t.thinking].DurationMs = int(time.Since(t.thinkStarted).Milliseconds())
	}
	return snapshot
}

func rawInput(v any) json.RawMessage {
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
