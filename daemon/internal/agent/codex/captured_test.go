package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/runner"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// Replay real CLI output through the production runner, without credentials or model calls.
type capturedExec struct {
	adapter
	raw    string
	events *[]agent.Event
}

func (capturedExec) Settings() agent.SettingsSchema { return agent.SettingsSchema{} }
func (a capturedExec) RunStream(_ context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	result, complete := parseStream(strings.NewReader(a.raw), func(event agent.Event) {
		if a.events != nil {
			*a.events = append(*a.events, event)
		}
		emit(event)
	})
	if !complete {
		return result, fmt.Errorf("capture ended without a terminal event: %s", result.Text)
	}
	return result, nil
}

type capturedAuth struct{ agent.AuthModule }

func (capturedAuth) EnvForRun() map[string]string { return nil }

func readCapture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "live-0.154.0", name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Export the actual shared protocol for consumer regression fixtures when requested.
func exportCapture(t *testing.T, name string, events []agent.Event, messages []agent.Message) {
	t.Helper()
	if directory := os.Getenv("CODEX_CAPTURE_EXPORT"); directory != "" {
		data, err := json.MarshalIndent(struct {
			Events   []agent.Event   `json:"events"`
			Messages []agent.Message `json:"messages"`
		}{events, messages}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCapturedExecAndNativeHistoryUseOneComponentPerItem(t *testing.T) {
	for _, name := range []string{"hello", "activity"} {
		t.Run(name, func(t *testing.T) {
			native, err := parseRollout(strings.NewReader(readCapture(t, name+".rollout")), "chat")
			if err != nil || len(native) != 2 {
				t.Fatalf("native history: messages=%d error=%v", len(native), err)
			}
			store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			var events []agent.Event
			r := runner.New(store, capturedExec{raw: readCapture(t, name+".exec"), events: &events}, capturedAuth{}, mapStore{}, stream.New(), t.TempDir())
			result, parts := r.RunTurn(context.Background(), runner.Turn{ChatID: "chat", RunID: "run", Message: native[0].Text})
			if result.IsError {
				t.Fatalf("stream failed: %+v", result)
			}
			recorded := []agent.Message{native[0], historyMessage("recorded", "assistant", result.Text, native[1].CreatedAt, parts...)}
			merged := mergeRecordedHistory(native, recorded)
			if len(merged) != 2 || len(merged[1].Parts) != len(native[1].Parts) {
				t.Fatalf("duplicate components: native=%d streamed=%d merged=%+v", len(native[1].Parts), len(parts), merged)
			}
			for _, preview := range parts {
				id := preview.ID
				if preview.Tool != nil {
					id = preview.Tool.ID
				}
				found := 0
				for _, final := range merged[1].Parts {
					if id != "" && (final.ID == id || final.Tool != nil && final.Tool.ID == id) {
						found++
					}
				}
				if found != 1 {
					t.Errorf("stream identity %q must survive exactly once in history; found %d", id, found)
				}
			}
			if again := mergeRecordedHistory(merged, recorded); !reflect.DeepEqual(again, merged) {
				t.Error("reloading history changed or duplicated components")
			}
			exportCapture(t, name, events, merged)
			if name == "hello" && merged[1].Text != "Hi! What can I help you with?" {
				t.Errorf("greeting duplicated: %q", merged[1].Text)
			}
			if name == "activity" {
				edits, failedCommands := 0, 0
				for _, part := range merged[1].Parts {
					if part.Tool == nil || part.Tool.Action == nil {
						continue
					}
					action := part.Tool.Action
					for _, file := range action.Files {
						edits++
						if file.Diff == "" {
							t.Errorf("native diff lost for %s", file.Path)
						}
					}
					if action.Shell != nil {
						if action.Shell.Cwd != "/workspace" {
							t.Errorf("native working directory was not normalized: %q", action.Shell.Cwd)
						}
						if action.Shell.ExitCode != nil && *action.Shell.ExitCode == 7 && part.Tool.IsError {
							failedCommands++
						}
					}
				}
				if edits != 2 || failedCommands != 1 {
					t.Errorf("edits=%d failedCommands=%d; want 2 and 1", edits, failedCommands)
				}
			}
		})
	}
}

func TestCapturedStructuredReplyAndCacheUsage(t *testing.T) {
	events, result, got := feed(t, readCapture(t, "structured.exec"))
	if !got || result.IsError || countType(events, agent.EventText) != 1 || countType(events, agent.EventThinking) != 1 {
		t.Fatalf("structured reply lost text or reasoning: %+v", result)
	}
	var reply struct {
		Greeting string `json:"greeting"`
		Checked  bool   `json:"checked"`
		Items    []int  `json:"items"`
	}
	if json.Unmarshal([]byte(result.Text), &reply) != nil || reply.Greeting != "Hello" || !reply.Checked || !reflect.DeepEqual(reply.Items, []int{1, 2, 3}) {
		t.Fatalf("structured response changed: %q", result.Text)
	}
	terminal := firstOf(events, agent.EventResult)
	if terminal == nil || terminal.Result.Usage == nil || terminal.Result.Usage.CacheWriteTokens != 9571 {
		t.Fatal("cache-write usage from the live CLI was lost")
	}
	exportCapture(t, "structured", events, nil)
}

func TestCapturedAppServerPlanUsesSharedInteractions(t *testing.T) {
	events, result, got := protocolTurn(t, strings.Split(readCapture(t, "plan.appserver"), "\n")...)
	if !got || result.IsError || countType(events, agent.EventToolUse) != 1 || countType(events, agent.EventToolResult) != 1 {
		t.Fatalf("app-server capture failed: %+v", result)
	}
	plans := map[string]string{}
	for _, event := range events {
		if event.Interaction != nil && event.Interaction.Kind == "plan" {
			plans[event.Interaction.ID] = event.Interaction.Detail
		}
	}
	if len(plans) != 1 || !strings.Contains(plans["turn-1-plan"], "test_add.py") {
		t.Fatalf("streamed plan did not settle into one authoritative component: %+v", plans)
	}
	exportCapture(t, "plan", events, nil)
}
