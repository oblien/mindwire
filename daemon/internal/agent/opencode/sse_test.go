package opencode

import (
	"strings"
	"testing"
)

// collectFrames runs parseSSE over a stream and returns every decoded frame.
func collectFrames(t *testing.T, stream string) []sseEvent {
	t.Helper()
	var out []sseEvent
	if err := parseSSE(strings.NewReader(stream), func(ev sseEvent) bool {
		out = append(out, ev)
		return true
	}); err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	return out
}

// A single frame decodes with the type pulled from INSIDE the JSON (opencode's quirk — no `event:`
// line) and Properties left raw for per-event decoding.
func TestParseSSESingleFrame(t *testing.T) {
	frames := collectFrames(t, "data: {\"type\":\"session.idle\",\"properties\":{\"sessionID\":\"ses_1\"}}\n\n")
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].Type != "session.idle" {
		t.Errorf("type = %q, want session.idle", frames[0].Type)
	}
	if !strings.Contains(string(frames[0].Properties), "ses_1") {
		t.Errorf("properties = %s, want it to carry ses_1", frames[0].Properties)
	}
}

// Multiple `data:` lines in one event are joined with "\n" per the SSE spec, reassembling one JSON
// payload before decode.
func TestParseSSEMultiLineData(t *testing.T) {
	stream := "data: {\"type\":\"server.connected\",\n" +
		"data: \"properties\":{}}\n\n"
	frames := collectFrames(t, stream)
	if len(frames) != 1 || frames[0].Type != "server.connected" {
		t.Fatalf("multi-line frame = %+v, want one server.connected", frames)
	}
}

// Comment lines (":" heartbeats) and non-data fields are ignored; CRLF terminators are tolerated.
func TestParseSSECommentsAndCRLF(t *testing.T) {
	stream := ": heartbeat\r\n" +
		"event: ignored\r\n" +
		"data: {\"type\":\"a\",\"properties\":{}}\r\n" +
		"\r\n"
	frames := collectFrames(t, stream)
	if len(frames) != 1 || frames[0].Type != "a" {
		t.Fatalf("frames = %+v, want one frame of type a", frames)
	}
}

// An undecodable frame is skipped rather than fatal, and a following good frame still arrives.
func TestParseSSEUndecodableSkipped(t *testing.T) {
	stream := "data: not json\n\n" +
		"data: {\"type\":\"good\",\"properties\":{}}\n\n"
	frames := collectFrames(t, stream)
	if len(frames) != 1 || frames[0].Type != "good" {
		t.Fatalf("frames = %+v, want the good frame only", frames)
	}
}

// A final frame with no trailing blank line (EOF-terminated) is still flushed, not dropped.
func TestParseSSENoTrailingBlank(t *testing.T) {
	frames := collectFrames(t, "data: {\"type\":\"last\",\"properties\":{}}")
	if len(frames) != 1 || frames[0].Type != "last" {
		t.Fatalf("frames = %+v, want the EOF-flushed frame", frames)
	}
}

// Returning false from fn stops the scan early: the second frame is never delivered.
func TestParseSSEStopEarly(t *testing.T) {
	stream := "data: {\"type\":\"first\",\"properties\":{}}\n\n" +
		"data: {\"type\":\"second\",\"properties\":{}}\n\n"
	var seen []string
	err := parseSSE(strings.NewReader(stream), func(ev sseEvent) bool {
		seen = append(seen, ev.Type)
		return false // stop after the first
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if len(seen) != 1 || seen[0] != "first" {
		t.Errorf("seen = %v, want [first] (stopped early)", seen)
	}
}
