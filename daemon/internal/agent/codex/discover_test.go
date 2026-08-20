package codex

import (
	"reflect"
	"strings"
	"testing"
)

// sampleExecHelp mimics `codex exec --help`: clap renders -s/--sandbox with an inline
// `[possible values: …]`. A following flag bounds the section so its values can't bleed in.
const sampleExecHelp = `Usage: codex exec [OPTIONS] [PROMPT]

Options:
  -s, --sandbox <SANDBOX>
          Sandbox policy for model-generated shell commands [possible values: read-only, workspace-write, danger-full-access]
  -m, --model <MODEL>
          Model to use [possible values: gpt-5.5, o3]
      --json
          Emit a JSONL event stream
`

// sampleRootHelp mimics `codex --help`: clap renders -a/--ask-for-approval as a multi-line
// `Possible values:` block of `- name: description` bullets.
const sampleRootHelp = `Usage: codex [OPTIONS] [COMMAND]

Options:
  -a, --ask-for-approval <APPROVAL_POLICY>
          Determines when the user should be prompted to approve a command

          Possible values:
          - untrusted:  Only run "trusted" commands without asking
          - on-request: The model decides when to escalate
          - never:      Never ask the user to approve

  -s, --sandbox <SANDBOX>
          Sandbox policy [possible values: read-only, workspace-write, danger-full-access]
`

func TestBracketChoices(t *testing.T) {
	got := bracketChoices(sampleExecHelp, "--sandbox")
	want := []string{"read-only", "workspace-write", "danger-full-access"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bracketChoices(--sandbox) = %v, want %v", got, want)
	}
	// --model's inline values are bounded to its own section (not bled from --sandbox above).
	if got := bracketChoices(sampleExecHelp, "--model"); !reflect.DeepEqual(got, []string{"gpt-5.5", "o3"}) {
		t.Errorf("bracketChoices(--model) = %v, want [gpt-5.5 o3]", got)
	}
	if got := bracketChoices(sampleExecHelp, "--nonexistent"); got != nil {
		t.Errorf("bracketChoices(absent) = %v, want nil", got)
	}
}

func TestListChoices(t *testing.T) {
	got := listChoices(sampleRootHelp, "--ask-for-approval")
	want := []string{"untrusted", "on-request", "never"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listChoices(--ask-for-approval) = %v, want %v", got, want)
	}
	if got := listChoices(sampleRootHelp, "--nonexistent"); got != nil {
		t.Errorf("listChoices(absent) = %v, want nil", got)
	}
}

// flagSection must stop at the next option header so a flag's parse can't read a neighbour's values.
func TestFlagSectionBounds(t *testing.T) {
	sec := flagSection(sampleRootHelp, "--ask-for-approval")
	if !containsAll(sec, "untrusted", "on-request", "never") {
		t.Errorf("--ask-for-approval section missing its own values: %q", sec)
	}
	// The sandbox values live in the NEXT option — they must not appear in this section.
	if containsAll(sec, "danger-full-access") {
		t.Errorf("--ask-for-approval section bled into --sandbox: %q", sec)
	}
}

func TestIsOptionHeader(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"  -s, --sandbox <SANDBOX>", true},
		{"      --json", true},
		{"          - untrusted:  Only run trusted", false}, // a description bullet, not a header
		{"          Possible values:", false},
		{"          The model decides", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isOptionHeader(c.line); got != c.want {
			t.Errorf("isOptionHeader(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
