package agent

// Cross-adapter helpers. These were byte-identical (or near-identical) copies in the claude and codex
// adapters; they live here so a new adapter reuses them instead of re-forking them. Nothing here is
// agent-specific — every value an adapter varies (its binary name, its config env var, its reject
// vocabulary superset) is a parameter.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ShellQuote single-quotes a string for POSIX sh, escaping embedded single quotes. Used to build the
// `bash -lc` command line so user text (a message, a path, an option value) can never break framing
// or inject a second command.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ParseCSV splits a comma-separated list, trimming whitespace and surrounding quotes from each item
// and dropping empties. Used to read enum choices out of CLI `--help` text.
func ParseCSV(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// FirstNonEmpty returns the first argument that is non-empty after trimming (returned verbatim, not
// trimmed), or "" if all are blank.
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Denied reports whether an interaction decision id is a rejection. It is the UNION of every
// adapter's reject vocabulary, so allow stays the default and any adapter can answer with any of
// these tokens. Case- and whitespace-insensitive.
func Denied(decision string) bool {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "deny", "reject", "no", "cancel", "decline":
		return true
	}
	return false
}

// MethodFields returns the input fields for one of an AuthModule's method ids (empty for an unknown
// or interactive method). Adapters expose a field-based auth method's inputs through this without
// re-implementing the lookup.
func MethodFields(m AuthModule, id string) []Field {
	for _, mth := range m.Methods() {
		if mth.ID == id {
			return mth.Fields
		}
	}
	return nil
}

// ConfigDir resolves an agent's config directory: the value of envVar if set, else
// ~/<defaultSub> (os.UserHomeDir honors $HOME, so it's correct under any sandbox home). Empty when
// the home directory can't be resolved.
func ConfigDir(envVar, defaultSub string) string {
	if base := os.Getenv(envVar); base != "" {
		return base
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, defaultSub)
}

// ResolveDir picks the working directory a project-scope memory/prompt operation targets: an explicit
// request value wins, else the supplied fallback (the daemon's own cwd), else the process working
// directory. This mirrors how a turn's cwd is resolved, so a project memory read aligns with where
// turns actually run. Returns "" only when all three are empty (os.Getwd failing on a daemon with no
// cwd — pathological).
func ResolveDir(dir, fallback string) string {
	if d := strings.TrimSpace(dir); d != "" {
		return d
	}
	if f := strings.TrimSpace(fallback); f != "" {
		return f
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// ValidatePromptName rejects a saved-prompt template name that isn't a single safe filename segment.
// The caller appends ".md"; the name must be a bare basename — non-empty, no "."/".." traversal, and
// no path separator — so a joined path can never escape its prompt directory. Combined with the
// single-segment API route, this is defense-in-depth against traversal from client input.
func ValidatePromptName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("prompt name is empty")
	case name == "." || name == "..":
		return fmt.Errorf("invalid prompt name %q", name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("prompt name %q must not contain a path separator", name)
	}
	return nil
}

// TempFiles accumulates temp files written while materializing a turn's options so a single deferred
// Cleanup removes them all. The zero value is ready to use; bind Cleanup as the caller's deferred
// cleanup before the first Write so partial failures still tidy up.
type TempFiles struct {
	paths []string
}

// Write creates a temp file matching pattern (os.CreateTemp semantics), writes data, records the path
// for Cleanup, and returns it. A write error still leaves the (empty) file recorded for cleanup.
func (t *TempFiles) Write(pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	t.paths = append(t.paths, f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return "", err
	}
	return f.Name(), f.Close()
}

// Cleanup removes every temp file Write created. Safe to call on the zero value and to defer even
// when a later step errors.
func (t *TempFiles) Cleanup() {
	for _, p := range t.paths {
		_ = os.Remove(p)
	}
}

// HelpCacheTTL is how long a captured `--help` blob stays fresh: one exec per window (so opening the
// settings UI repeatedly doesn't shell out each time), refreshed periodically so a CLI upgrade (e.g.
// via POST /update) is picked up without a daemon restart.
const HelpCacheTTL = 10 * time.Minute

// HelpCache memoizes the combined output of one CLI help invocation with a TTL, keeping the last good
// value if a refresh exec fails. Each distinct help surface (root vs a subcommand) is its own
// instance. Safe for concurrent use.
type HelpCache struct {
	Command string   // binary to exec, e.g. "claude" or "codex"
	Args    []string // arguments, e.g. {"--help"} or {"exec", "--help"}

	mu  sync.Mutex
	val string
	at  time.Time
}

// Get returns the cached help text, refreshing it if the TTL has elapsed. A failed refresh leaves the
// previous value in place (so a transient CLI hiccup doesn't blank the settings enums).
func (c *HelpCache) Get() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.val != "" && time.Since(c.at) < HelpCacheTTL {
		return c.val
	}
	if out, err := exec.Command(c.Command, c.Args...).CombinedOutput(); err == nil && len(out) > 0 {
		c.val = string(out)
		c.at = time.Now()
	}
	return c.val // keeps the last good value if this exec failed
}
