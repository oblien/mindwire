package opencode

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// sseEvent is one decoded frame from opencode's GET /event bus. opencode is unusual: the event TYPE
// lives inside the JSON payload (`{"type":"…","properties":{…}}`), not on an SSE `event:` line, so a
// frame is just its `data:` payload decoded into this shape. Properties stays raw so normalize.go can
// decode it into the per-event struct only when the type is one we handle.
type sseEvent struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

// parseSSE reads an opencode SSE stream and calls fn once per decoded frame. It accumulates the `data:`
// lines of an event (opencode sends the JSON on one line, but the SSE spec allows several, joined by
// "\n") and flushes the decoded frame on the blank line that terminates the event. Comment lines (":")
// and non-data fields (`event:`, `id:`, `retry:`) are ignored — the type is in the JSON. An undecodable
// frame is skipped rather than fatal, so one malformed event can't kill the stream. fn returns false to
// stop early (the caller has what it needs); parseSSE then returns nil. Any scan error is returned so
// the caller can surface a truncated stream.
func parseSSE(r io.Reader, fn func(sseEvent) bool) error {
	sc := bufio.NewScanner(r)
	// opencode part payloads (cumulative text, tool output) can be large; give the scanner room so a
	// single event line never trips bufio.ErrTooLong. Start 64KiB, grow to 16MiB.
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)

	var data []string
	// flush decodes and dispatches the accumulated event; returns false only when fn asked to stop.
	flush := func() bool {
		if len(data) == 0 {
			return true // blank line with nothing buffered — keep going
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		var ev sseEvent
		if json.Unmarshal([]byte(payload), &ev) != nil {
			return true // undecodable frame — skip, keep reading
		}
		return fn(ev)
	}

	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r") // tolerate CRLF as well as LF
		if line == "" {                             // blank line terminates one event
			if !flush() {
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment / heartbeat
		}
		if d, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(d, " ")) // strip ONE leading space per the SSE spec
		}
		// any other field is ignored
	}
	// EOF without a trailing blank line: flush a pending frame so a final event isn't dropped.
	if len(data) > 0 {
		flush()
	}
	return sc.Err()
}
