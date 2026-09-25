package agent

import (
	"context"
	"strings"
	"unicode"
)

// NativeSession is a reference to a harness-owned conversation, never a copy of
// its messages. ID is passed unchanged to the adapter's existing resume/history.
type NativeSession struct {
	ID        string
	CWD       string
	Title     string
	CreatedAt string
	UpdatedAt string
	// Some harness versions expose both a thread ID and a session ID.
	Aliases []string
}

// SessionLister is optional. Listing is read-only: it must not start a turn,
// create a session, install a CLI, or change the harness's authentication.
// CWD selects one exact project directory, including its filesystem aliases.
type SessionLister interface {
	ListSessions(ctx context.Context, cwd string) ([]NativeSession, error)
}

func SessionTitle(text string) string {
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	runes := []rune(text)
	if len(runes) > 120 {
		text = string(runes[:117]) + "…"
	}
	return text
}

func ValidNativeSessionID(id string) bool {
	return id != "" && len(id) <= 512 && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}
