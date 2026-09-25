//go:build unix

package workspaceexec

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

func TestExecPreservesArgumentsAndCancellation(t *testing.T) {
	s := New(t.TempDir())
	defer s.Terminals.Close()
	text := "quoted ' \" $HOME $(touch bad)\nمرحبا"
	r, err := s.Exec(context.Background(), ExecRequest{Argv: []string{"/usr/bin/printf", "%s", text}}, nil)
	if err != nil || r.Stdout != text || r.ExitCode != 0 {
		t.Fatalf("exec: %#v %v", r, err)
	}
	r, err = s.Exec(context.Background(), ExecRequest{Argv: []string{"/bin/sh", "-c", "printf error >&2; exit 7"}}, nil)
	if err != nil || r.ExitCode != 7 || r.Stderr != "error" {
		t.Fatalf("exit: %#v %v", r, err)
	}
	started := time.Now()
	_, err = s.Exec(context.Background(), ExecRequest{Argv: []string{"/bin/sh", "-c", "sleep 30 & wait"}, TimeoutMS: 50}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("cancellation: %v after %v", err, time.Since(started))
	}
}

func waitOutput(t *testing.T, terminal *Terminal, after uint64, text string, timeout ...time.Duration) uint64 {
	t.Helper()
	replay, events, cancel := terminal.Subscribe(after)
	defer cancel()
	var output string
	last := after
	accept := func(e TerminalEvent) bool {
		data, _ := base64.StdEncoding.DecodeString(e.Data)
		output += string(data)
		if len(output) > 8192 {
			output = output[len(output)-8192:]
		}
		last = e.Sequence
		return strings.Contains(output, text)
	}
	for _, e := range replay {
		if accept(e) {
			return last
		}
	}
	wait := 5 * time.Second
	if len(timeout) > 0 {
		wait = timeout[0]
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("terminal exited before %q: %q", text, output)
			}
			if accept(e) {
				return last
			}
		case <-deadline.C:
			t.Fatalf("no output %q: %q", text, output)
		}
	}
}

func TestTerminalDetachReplayInputReceiptsAndClose(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	s := New(t.TempDir())
	defer s.Terminals.Close()
	req := TerminalRequest{ID: "terminal-test", Columns: 80, Rows: 24}
	state, err := s.Terminals.Open(req)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.Terminals.Open(req)
	if err != nil || retry.CreatedAt != state.CreatedAt || len(s.Terminals.List("")) != 1 {
		t.Fatalf("duplicate terminal: %#v %v", retry, err)
	}
	terminal, _ := s.Terminals.Get(req.ID)
	input := func(sequence uint64, text string) TerminalInput {
		return TerminalInput{Writer: "writer-test", Sequence: sequence, Data: base64.StdEncoding.EncodeToString([]byte(text))}
	}
	first := input(1, "printf '\\115\\127:one\\n'\n")
	if err := terminal.Input(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	cursor := waitOutput(t, terminal, 0, "MW:one")
	if err := terminal.Input(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Input(context.Background(), input(1, "different")); err == nil {
		t.Fatal("same cursor accepted different input")
	}
	if err := terminal.Input(context.Background(), input(3, "out of order")); err == nil {
		t.Fatal("skipped input cursor")
	}
	// No subscribers: the shell must keep running and replay this output on return.
	if err := terminal.Input(context.Background(), input(2, "printf '\\115\\127:detached\\n'\n")); err != nil {
		t.Fatal(err)
	}
	waitOutput(t, terminal, cursor, "MW:detached")
	replay, _, cancel := terminal.Subscribe(0)
	var all strings.Builder
	for _, e := range replay {
		data, _ := base64.StdEncoding.DecodeString(e.Data)
		all.Write(data)
	}
	cancel()
	if strings.Count(all.String(), "MW:one") != 1 {
		t.Fatalf("duplicated input: %q", all.String())
	}
	if err := terminal.Resize(101, 35); err != nil {
		t.Fatal(err)
	}
	if terminal.State().Columns != 101 || !s.Terminals.Busy() {
		t.Fatal("resize/running status was lost")
	}
	if err := s.Terminals.Remove(req.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-terminal.done:
	case <-time.After(5 * time.Second):
		t.Fatal("PTY did not close")
	}
	if s.Terminals.Busy() || len(s.Terminals.List("")) != 0 {
		t.Fatal("closed terminal remains")
	}
	if _, err := s.Terminals.Open(req); err == nil {
		t.Fatal("late open resurrected a closed terminal")
	}
	if err := s.Terminals.Remove("late-terminal"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Terminals.Open(TerminalRequest{ID: "late-terminal", Columns: 80, Rows: 24}); err == nil {
		t.Fatal("late create resurrected a deleted intent")
	}
}

type failedPTY struct {
	pty.Pty
	writes int
}

func (p *failedPTY) Write(data []byte) (int, error) { p.writes++; return 1, io.ErrClosedPipe }

func TestInputRetryPreservesPartialFailure(t *testing.T) {
	p := &failedPTY{}
	terminal := &Terminal{pty: p, state: TerminalState{Running: true}, inputs: map[string]inputReceipt{}}
	input := TerminalInput{Writer: "writer-test", Sequence: 1, Data: base64.StdEncoding.EncodeToString([]byte("hello"))}
	for range 2 {
		if err := terminal.Input(context.Background(), input); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("lost write error: %v", err)
		}
	}
	if p.writes != 1 {
		t.Fatal("partial input was replayed")
	}
}

func TestDetachedTerminalAnswersQueriesAndRestoresBoundedScreen(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	s := New(t.TempDir())
	defer s.Terminals.Close()
	state, err := s.Terminals.Open(TerminalRequest{ID: "detached-query-screen", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	terminal, _ := s.Terminals.Get(state.ID)
	command := "stty -echo -icanon min 1 time 20; printf '\\033[5n'; answer=$(dd bs=1 count=4 2>/dev/null); " +
		"[ \"$answer\" = \"$(printf '\\033[0n')\" ] && printf '\\nDAEMON_QUERY_%s\\n' 'READY'; stty sane\n"
	if err := terminal.Input(context.Background(), TerminalInput{Writer: "query-writer", Sequence: 1,
		Data: base64.StdEncoding.EncodeToString([]byte(command))}); err != nil {
		t.Fatal(err)
	}
	// The subscriber only reads output. No client supplies a terminal query reply.
	waitOutput(t, terminal, 0, "DAEMON_QUERY_READY")
	command = "head -c 1800000 /dev/zero | tr '\\000' 'x'; printf '\\033[2J\\033[HSCENE_%s\\n' 'READY'\n"
	if err := terminal.Input(context.Background(), TerminalInput{Writer: "query-writer", Sequence: 2,
		Data: base64.StdEncoding.EncodeToString([]byte(command))}); err != nil {
		t.Fatal(err)
	}
	waitOutput(t, terminal, 0, "SCENE_READY", 30*time.Second)
	replay, _, cancel := terminal.Subscribe(0)
	defer cancel()
	if len(replay) != 1 || replay[0].Kind != "reset" {
		t.Fatal("client outside the replay window did not receive a screen reset")
	}
	data, err := base64.StdEncoding.DecodeString(replay[0].Data)
	if err != nil || !strings.Contains(string(data), "SCENE_READY") || len(data) > 32_768 {
		t.Fatal("bounded snapshot lost the terminal's current screen")
	}
}
