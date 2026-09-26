package workspaceexec

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

const TerminalVersion = 1
const terminalReplayBytes = 2 << 20

type TerminalRequest struct {
	ID        string `json:"id"`
	Directory string `json:"directory,omitempty"`
	Columns   int    `json:"columns"`
	Rows      int    `json:"rows"`
}
type TerminalState struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
	Columns   int    `json:"columns"`
	Rows      int    `json:"rows"`
	Running   bool   `json:"running"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	CreatedAt string `json:"createdAt"`
}
type TerminalEvent struct {
	Sequence uint64         `json:"sequence"`
	Kind     string         `json:"kind"`
	Data     string         `json:"data,omitempty"` // Base64 preserves partial UTF-8 and arbitrary terminal bytes.
	State    *TerminalState `json:"state,omitempty"`
}
type TerminalInput struct {
	Writer   string `json:"writer"`
	Sequence uint64 `json:"sequence"`
	Data     string `json:"data"`
}
type inputReceipt struct {
	sequence uint64
	digest   [32]byte
	err      error
}

type Terminals struct {
	mu        sync.Mutex
	workspace *Service
	sessions  map[string]*Terminal
	closed    map[string]bool
}
type Terminal struct {
	mu              sync.Mutex
	inputMu         sync.Mutex
	writeMu         sync.Mutex
	state           TerminalState
	pty             pty.Pty
	cmd             *pty.Cmd
	emulator        *vt.Emulator
	modes           map[ansi.Mode]bool
	events          []TerminalEvent
	bytes           int
	sequence        uint64
	subs            map[chan TerminalEvent]bool
	inputs          map[string]inputReceipt
	done            chan struct{}
	repliesDone     chan struct{}
	replyPipe       io.Closer
	closeOnce       sync.Once
	releaseMutation func()
}

func NewTerminals(s *Service) *Terminals {
	return &Terminals{workspace: s, sessions: map[string]*Terminal{}, closed: map[string]bool{}}
}
func validID(id string) bool {
	if len(id) < 8 || len(id) > 128 {
		return false
	}
	return strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}
func validSize(columns, rows int) bool {
	return columns >= 2 && columns <= 512 && rows >= 2 && rows <= 256
}

func (m *Terminals) Open(req TerminalRequest) (TerminalState, error) {
	if !validID(req.ID) || !validSize(req.Columns, req.Rows) {
		return TerminalState{}, errors.New("invalid terminal ID or size")
	}
	directory, err := m.workspace.Path(req.Directory)
	if err != nil {
		return TerminalState{}, err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return TerminalState{}, err
	}
	if !info.IsDir() {
		return TerminalState{}, errors.New("select a directory")
	}
	release, err := m.workspace.beginMutation(directory)
	if err != nil {
		return TerminalState{}, err
	}
	retained := false
	defer func() {
		if !retained {
			release()
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed[req.ID] {
		return TerminalState{}, errors.New("this terminal was closed; create a new terminal")
	}
	if t := m.sessions[req.ID]; t != nil {
		state := t.State()
		if state.Directory != filepath.ToSlash(directory) {
			return TerminalState{}, errors.New("terminal ID already belongs to another directory")
		}
		return state, nil
	}
	if len(m.sessions) >= 16 {
		return TerminalState{}, errors.New("close a terminal before opening another (maximum 16)")
	}
	shell := os.Getenv("SHELL")
	// Inherit the launching user's environment; don't run login scripts a second time.
	args := []string{}
	if runtime.GOOS == "windows" {
		shell, args = "powershell.exe", []string{"-NoLogo"}
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	shell, err = Executable(shell)
	if err != nil {
		return TerminalState{}, err
	}
	p, err := pty.New()
	if err != nil {
		return TerminalState{}, err
	}
	if err = p.Resize(req.Columns, req.Rows); err != nil {
		p.Close()
		return TerminalState{}, err
	}
	cmd := p.Command(shell, args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	if err = cmd.Start(); err != nil {
		p.Close()
		return TerminalState{}, err
	}
	t := &Terminal{state: TerminalState{ID: req.ID, Directory: filepath.ToSlash(directory), Title: filepath.Base(shell),
		Columns: req.Columns, Rows: req.Rows, Running: true, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		pty: p, cmd: cmd, emulator: vt.NewEmulator(req.Columns, req.Rows), modes: map[ansi.Mode]bool{},
		subs: map[chan TerminalEvent]bool{}, inputs: map[string]inputReceipt{}, done: make(chan struct{})}
	t.emulator.SetScrollbackSize(2000)
	t.emulator.SetCallbacks(vt.Callbacks{
		EnableMode:  func(mode ansi.Mode) { t.modes[mode] = true },
		DisableMode: func(mode ansi.Mode) { t.modes[mode] = false },
	})
	// The upstream emulator's operating-status reply uses the DEC private form
	// for an ANSI query. Match native terminals for programs waiting for CSI 0 n.
	t.emulator.RegisterCsiHandler('n', func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 1)
		if !ok || n != 5 {
			return false
		}
		_, _ = io.WriteString(t.emulator.InputPipe(), ansi.DeviceStatusReport(ansi.ANSIStatusReport(0)))
		return true
	})
	var ok bool
	t.replyPipe, ok = t.emulator.InputPipe().(io.Closer)
	if !ok {
		t.Close()
		_ = cmd.Wait()
		return TerminalState{}, errors.New("terminal emulator requires a closable input pipe")
	}
	t.repliesDone = make(chan struct{})
	// The daemon owns device-query replies too, so a detached interactive program
	// never waits for an absent phone. Clients suppress replies generated while
	// rendering these bytes; user keyboard input uses the input endpoint.
	go func() {
		defer close(t.repliesDone)
		_, _ = io.Copy(terminalReplyWriter{t}, t.emulator)
	}()
	m.sessions[req.ID] = t
	t.releaseMutation = release
	retained = true
	go t.read()
	return t.State(), nil
}

func (m *Terminals) List(directory string) []TerminalState {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := []TerminalState{}
	var path string
	if directory != "" {
		path, _ = m.workspace.Path(directory)
	}
	for _, t := range m.sessions {
		state := t.State()
		if path == "" || state.Directory == filepath.ToSlash(path) {
			items = append(items, state)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt < items[j].CreatedAt })
	return items
}
func (m *Terminals) Get(id string) (*Terminal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.sessions[id]; t != nil {
		return t, nil
	}
	return nil, os.ErrNotExist
}
func (m *Terminals) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.sessions {
		if t.State().Running {
			return true
		}
	}
	return false
}
func (m *Terminals) Remove(id string) error {
	if !validID(id) {
		return errors.New("invalid terminal ID")
	}
	m.mu.Lock()
	t := m.sessions[id]
	if m.closed[id] {
		m.mu.Unlock()
		return nil
	}
	if len(m.closed) >= 4096 {
		m.mu.Unlock()
		return errors.New("terminal history limit reached; restart the service when idle")
	}
	delete(m.sessions, id)
	m.closed[id] = true
	m.mu.Unlock()
	// Retire a not-yet-visible create intent too: DELETE may beat an in-flight
	// idempotent POST when a phone closes the tab during connection setup.
	if t != nil {
		t.Close()
	}
	return nil
}
func (m *Terminals) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.sessions {
		t.Close()
	}
}
func (t *Terminal) State() TerminalState { t.mu.Lock(); defer t.mu.Unlock(); return t.state }
func (t *Terminal) Close() {
	t.closeOnce.Do(func() {
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		_ = t.pty.Close()
	})
}

func (t *Terminal) read() {
	defer t.releaseMutation()
	defer close(t.done)
	buffer := make([]byte, 8192)
	for {
		n, err := t.pty.Read(buffer)
		if n > 0 {
			t.mu.Lock()
			_, _ = t.emulator.Write(buffer[:n])
			t.publish(TerminalEvent{Kind: "output", Data: base64.StdEncoding.EncodeToString(buffer[:n])})
			t.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	_ = t.cmd.Wait()
	t.closeOnce.Do(func() { _ = t.pty.Close() })
	t.mu.Lock()
	defer t.mu.Unlock()
	code := -1
	if t.cmd.ProcessState != nil {
		code = t.cmd.ProcessState.ExitCode()
	}
	t.state.Running = false
	t.state.ExitCode = &code
	state := t.state
	t.publish(TerminalEvent{Kind: "exit", State: &state})
	for ch := range t.subs {
		close(ch)
		delete(t.subs, ch)
	}
	// Close the thread-safe pipe first and join its reader before changing the
	// emulator's non-atomic closed flag. Read/Close in the dependency otherwise race.
	_ = t.replyPipe.Close()
	<-t.repliesDone
	_ = t.emulator.Close()
}

// Caller holds mu; a slow client reconnects with a cursor rather than losing bytes.
func (t *Terminal) publish(event TerminalEvent) {
	t.sequence++
	event.Sequence = t.sequence
	t.events = append(t.events, event)
	t.bytes += len(event.Data)
	for t.bytes > terminalReplayBytes && len(t.events) > 1 {
		t.bytes -= len(t.events[0].Data)
		t.events[0] = TerminalEvent{}
		t.events = t.events[1:]
	}
	for ch := range t.subs {
		select {
		case ch <- event:
		default:
			close(ch)
			delete(t.subs, ch)
		}
	}
}

func (t *Terminal) snapshot() TerminalEvent {
	var text strings.Builder
	text.WriteString("\x1bc")
	if t.emulator.IsAltScreen() {
		text.WriteString("\x1b[?1049h")
	}
	text.WriteString("\x1b[H")
	text.WriteString(strings.ReplaceAll(t.emulator.Render(), "\n", "\r\n"))
	position := t.emulator.CursorPosition()
	fmt.Fprintf(&text, "\x1b[%d;%dH", position.Y+1, position.X+1)
	for mode, enabled := range t.modes {
		// Entering the alternate screen twice clears the content we just restored.
		if mode == ansi.DECMode(47) || mode == ansi.DECMode(1047) || mode == ansi.DECMode(1049) {
			continue
		}
		if enabled {
			text.WriteString(ansi.SetMode(mode))
		} else {
			text.WriteString(ansi.ResetMode(mode))
		}
	}
	state := t.state
	return TerminalEvent{Sequence: t.sequence, Kind: "reset", Data: base64.StdEncoding.EncodeToString([]byte(text.String())), State: &state}
}

func (t *Terminal) Subscribe(after uint64) ([]TerminalEvent, <-chan TerminalEvent, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	replay := []TerminalEvent{}
	if after > t.sequence || len(t.events) > 0 && after+1 < t.events[0].Sequence {
		replay = append(replay, t.snapshot())
	} else {
		for _, event := range t.events {
			if event.Sequence > after {
				replay = append(replay, event)
			}
		}
	}
	ch := make(chan TerminalEvent, 128)
	if !t.state.Running {
		close(ch)
	} else {
		t.subs[ch] = true
	}
	return replay, ch, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.subs[ch] {
			delete(t.subs, ch)
			close(ch)
		}
	}
}

func (t *Terminal) Input(ctx context.Context, input TerminalInput) error {
	if !validID(input.Writer) || input.Sequence == 0 {
		return errors.New("invalid terminal input cursor")
	}
	data, err := base64.StdEncoding.DecodeString(input.Data)
	if err != nil || len(data) > 65536 {
		return errors.New("invalid terminal input")
	}
	t.inputMu.Lock()
	defer t.inputMu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	t.mu.Lock()
	previous, exists := t.inputs[input.Writer]
	digest := sha256.Sum256(data)
	if previous.sequence == input.Sequence && previous.digest == digest {
		t.mu.Unlock()
		return previous.err
	}
	if input.Sequence != previous.sequence+1 || !t.state.Running || !exists && len(t.inputs) >= 64 {
		t.mu.Unlock()
		return errors.New("terminal input is out of order or the terminal has exited")
	}
	t.mu.Unlock()
	// inputMu serializes delivery and acknowledgement. Cache failures too: a retry
	// must neither repeat partially written keystrokes nor falsely report success.
	n, err := t.write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	t.mu.Lock()
	t.inputs[input.Writer] = inputReceipt{sequence: input.Sequence, digest: digest, err: err}
	t.mu.Unlock()
	return err
}

type terminalReplyWriter struct{ t *Terminal }

func (w terminalReplyWriter) Write(data []byte) (int, error) { return w.t.write(data) }
func (t *Terminal) write(data []byte) (int, error) {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.pty.Write(data)
}

func (t *Terminal) Resize(columns, rows int) error {
	if !validSize(columns, rows) {
		return errors.New("invalid terminal size")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.state.Running {
		return errors.New("terminal has exited")
	}
	if err := t.pty.Resize(columns, rows); err != nil {
		return err
	}
	t.emulator.Resize(columns, rows)
	t.state.Columns, t.state.Rows = columns, rows
	return nil
}
