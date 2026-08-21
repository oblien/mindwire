package codex

import (
	"strings"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// This file makes Codex's settings VALUES come from the CLI, not hardcoded constants. The adapter
// owns the references (which fields, types, labels); the CLI owns the option lists. Codex's help
// enumerates enums in TWO shapes, so there are two parsers:
//   - `-s/--sandbox` uses clap's inline `[possible values: read-only, workspace-write, …]`
//   - `-a/--ask-for-approval` uses clap's multi-line `Possible values:\n  - name: description`
// Both bound their search to the flag's own section so values can't bleed in from a neighbour.

// help output is cached with a TTL (see agent.HelpCache): one exec per window (so opening the
// settings UI repeatedly doesn't shell out each time), refreshed periodically so a CLI upgrade is
// picked up without a daemon restart. Two surfaces are cached separately: the root help (has -a) and
// the exec help (has -s; exec has no -a).
var (
	rootHelp = &agent.HelpCache{Command: "codex", Args: []string{"--help"}}
	execHelp = &agent.HelpCache{Command: "codex", Args: []string{"exec", "--help"}}
)

// sandboxChoices reads the -s/--sandbox enum from `codex exec --help`. nil if unavailable → the field
// degrades to free text.
func sandboxChoices() []string { return bracketChoices(execHelp.Get(), "--sandbox") }

// approvalChoices reads the -a/--ask-for-approval enum from `codex --help` (root; exec has no -a).
func approvalChoices() []string { return listChoices(rootHelp.Get(), "--ask-for-approval") }

// flagSection returns the help text for one flag: from the flag's header up to the next option
// header, so value-parsing is bounded to that flag and can't bleed into a later option's text.
func flagSection(help, longFlag string) string {
	i := strings.Index(help, longFlag)
	if i < 0 {
		return ""
	}
	lines := strings.Split(help[i:], "\n")
	var b strings.Builder
	for n, ln := range lines {
		if n > 0 && isOptionHeader(ln) {
			break
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String()
}

// isOptionHeader reports whether a help line begins a new clap option (which bounds the previous
// option's section). Long options read `--flag`; short+long read `-x, --flag`. Description bullets
// read `- word` (single dash + space) and must NOT count as a header.
func isOptionHeader(line string) bool {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "--") {
		return true
	}
	// `-x, --flag` form: dash, letter, comma, space, dash.
	return len(t) >= 5 && t[0] == '-' && isLetter(t[1]) && t[2] == ',' && t[3] == ' ' && t[4] == '-'
}

// bracketChoices parses clap's inline `[possible values: a, b, c]` enum for a flag.
func bracketChoices(help, longFlag string) []string {
	sec := flagSection(help, longFlag)
	if sec == "" {
		return nil
	}
	lower := strings.ToLower(sec)
	a := strings.Index(lower, "possible values:")
	if a < 0 {
		return nil
	}
	seg := sec[a+len("possible values:"):]
	if b := strings.Index(seg, "]"); b >= 0 {
		seg = seg[:b]
	}
	return filterBareWords(agent.ParseCSV(seg))
}

// listChoices parses clap's multi-line `Possible values:\n  - name: description` enum for a flag,
// taking each bullet's name (the token before its colon).
func listChoices(help, longFlag string) []string {
	sec := flagSection(help, longFlag)
	if sec == "" {
		return nil
	}
	lower := strings.ToLower(sec)
	a := strings.Index(lower, "possible values:")
	if a < 0 {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(sec[a+len("possible values:"):], "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		name := strings.TrimPrefix(t, "- ")
		if c := strings.Index(name, ":"); c >= 0 {
			name = name[:c]
		}
		if name = strings.TrimSpace(name); isBareWord(name) {
			out = append(out, name)
		}
	}
	return out
}

// filterBareWords keeps only the CSV items that look like enum values (letters/hyphen/underscore),
// dropping any prose that slipped through.
func filterBareWords(items []string) []string {
	var out []string
	for _, w := range items {
		if isBareWord(w) {
			out = append(out, w)
		}
	}
	return out
}

// isBareWord reports whether a token looks like an enum value: an identifier of letters, digits, and
// the separators that appear in real values (a model like "gpt-5.5", a policy like "on-request").
// Prose is excluded because it contains spaces or other punctuation.
func isBareWord(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
