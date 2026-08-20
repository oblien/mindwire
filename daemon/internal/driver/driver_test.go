package driver

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// lineParse is a minimal stand-in for a real adapter parser: each stdout line becomes an EventText,
// and a line prefixed "RESULT:" is the terminal result (got=true). It lets the driver tests drive the
// generic plumbing (spawn, env, stderr fallback) with a trivial, deterministic stream.
func lineParse(stdout io.Reader, emit agent.Emit) (agent.TurnResult, bool) {
	sc := bufio.NewScanner(stdout)
	var res agent.TurnResult
	got := false
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, "RESULT:"); ok {
			res = agent.TurnResult{Text: rest}
			got = true
			emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: rest}})
			continue
		}
		emit(agent.Event{Type: agent.EventText, Text: line})
	}
	return res, got
}

func collect() (agent.Emit, *[]agent.Event) {
	var evs []agent.Event
	return func(e agent.Event) { evs = append(evs, e) }, &evs
}

// CLI.Run spawns the command, streams stdout through Parse into unified events, and returns the parser's
// terminal result.
func TestCLIRunStreamsEventsAndResult(t *testing.T) {
	emit, evs := collect()
	res, err := CLI{Command: `printf 'hello\nworld\nRESULT:done\n'`, Parse: lineParse}.
		Run(context.Background(), agent.TurnInput{}, emit)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.IsError || res.Text != "done" {
		t.Fatalf("result = %+v, want {done,false}", res)
	}
	var texts []string
	sawResult := false
	for _, e := range *evs {
		switch e.Type {
		case agent.EventText:
			texts = append(texts, e.Text)
		case agent.EventResult:
			sawResult = true
		}
	}
	if strings.Join(texts, ",") != "hello,world" {
		t.Errorf("text events = %v, want [hello world] in order", texts)
	}
	if !sawResult {
		t.Error("expected a result event")
	}
}

// Env is exported through the PROCESS environment, not interpolated into the shell string — the command
// reads it as a normal env var.
func TestCLIRunExportsEnvNotArgv(t *testing.T) {
	emit, _ := collect()
	res, err := CLI{
		Command: `printf 'RESULT:%s\n' "$MY_TOKEN"`,
		Env:     map[string]string{"MY_TOKEN": "sekret"},
		Parse:   lineParse,
	}.Run(context.Background(), agent.TurnInput{}, emit)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Text != "sekret" {
		t.Fatalf("env not exported to the process: result = %+v", res)
	}
}

// When the parser sees no terminal result, the driver surfaces stderr as the error (an EventError plus
// an error TurnResult) rather than reporting a silent success.
func TestCLIRunSurfacesStderrWhenNoResult(t *testing.T) {
	emit, evs := collect()
	res, _ := CLI{Command: `echo "boom" >&2; exit 1`, Parse: lineParse}.
		Run(context.Background(), agent.TurnInput{}, emit)
	if !res.IsError || !strings.Contains(res.Text, "boom") {
		t.Fatalf("expected an error result carrying stderr, got %+v", res)
	}
	sawErr := false
	for _, e := range *evs {
		if e.Type == agent.EventError && strings.Contains(e.Error, "boom") {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("expected an EventError carrying the stderr text")
	}
}

// Persistent.Run smoke: the Preamble is written to the process's stdin (here echoed straight back by
// `cat`), parsed into events, and the terminal result closes stdin so the process exits cleanly.
func TestPersistentRunSmoke(t *testing.T) {
	emit, evs := collect()
	res, err := Persistent{
		Command:  "cat",
		Parse:    lineParse,
		Encode:   func(agent.Inbound) ([]byte, bool) { return nil, false },
		Preamble: [][]byte{[]byte("hello"), []byte("RESULT:done")},
		Inbound:  nil,
	}.Run(context.Background(), agent.TurnInput{}, emit)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Text != "done" {
		t.Fatalf("result = %+v, want done", res)
	}
	sawHello := false
	for _, e := range *evs {
		if e.Type == agent.EventText && e.Text == "hello" {
			sawHello = true
		}
	}
	if !sawHello {
		t.Error("expected the preamble line to be echoed through Parse as a text event")
	}
}
