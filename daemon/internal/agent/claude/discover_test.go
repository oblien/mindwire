package claude

import (
	"reflect"
	"testing"
)

// sampleHelp mimics `claude --help` formatting: options wrap across lines, choices
// appear either as a (choices: "a", …) list or a bare (w1, w2, …) list, and --model
// carries a parenthetical EXAMPLE that must NOT be read as an enum.
const sampleHelp = `Options:
  --effort <level>                      Effort level for the current session
                                        (low, medium, high, xhigh, max)
  --model <model>                       Model for the current session. Provide
                                        an alias for the latest model (e.g.
                                        'fable', 'opus', or 'sonnet') or a
                                        model's full name (e.g.
                                        'claude-fable-5').
  --permission-mode <mode>              Permission mode to use for the session
                                        (choices: "acceptEdits", "auto",
                                        "bypassPermissions", "default",
                                        "dontAsk", "plan")
  --add-dir <directories...>            Additional directories to allow
`

func TestParseChoices(t *testing.T) {
	cases := []struct {
		flag string
		want []string
	}{
		{"--effort", []string{"low", "medium", "high", "xhigh", "max"}},
		{"--permission-mode", []string{"acceptEdits", "auto", "bypassPermissions", "default", "dontAsk", "plan"}},
		{"--model", nil},   // example values must not be mistaken for an enum → stays free text
		{"--add-dir", nil}, // no choices
		{"--nonexistent", nil},
	}
	for _, c := range cases {
		got := parseChoices(sampleHelp, c.flag)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseChoices(%q) = %v, want %v", c.flag, got, c.want)
		}
	}
}

func TestRememberToolsUnion(t *testing.T) {
	toolMu.Lock()
	toolSeen = map[string]bool{}
	toolMu.Unlock()

	rememberTools([]string{"Bash", "Edit"})
	rememberTools([]string{"Edit", "Read"}) // overlapping + new → union grows, no dupes
	got := knownTools()
	want := []string{"Bash", "Edit", "Read"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("knownTools() = %v, want %v", got, want)
	}
}
