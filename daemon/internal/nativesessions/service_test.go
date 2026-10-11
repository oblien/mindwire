package nativesessions

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

type testAdapter struct {
	agent.Adapter
	opened     atomic.Int32
	probed     atomic.Int32
	opening    <-chan struct{}
	connection *testConnection
}

func (*testAdapter) ID() string { return "native-fixture" }
func (a *testAdapter) SessionActivity(ctx context.Context, _ agent.NativeSessionQuery) (agent.SessionActivity, error) {
	a.probed.Add(1)
	if a.opening != nil {
		select {
		case <-a.opening:
		case <-ctx.Done():
			return agent.SessionActivity{}, ctx.Err()
		}
	}
	return a.connection.Initial().Activity, nil
}
func (a *testAdapter) OpenNativeSession(ctx context.Context, _ agent.NativeSessionQuery) (agent.NativeSessionConnection, error) {
	a.opened.Add(1)
	if a.opening != nil {
		select {
		case <-a.opening:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return a.connection, nil
}

type testConnection struct {
	updates chan agent.NativeSessionUpdate
	once    sync.Once
	closed  chan struct{}
	actions atomic.Int32
	accept  <-chan struct{}
}

func newTestConnection() *testConnection {
	return &testConnection{updates: make(chan agent.NativeSessionUpdate, 8), closed: make(chan struct{})}
}
func (*testConnection) Initial() agent.NativeSessionInitial {
	return agent.NativeSessionInitial{Activity: agent.SessionActivity{Owner: "native", State: "running", TurnID: "turn-1", Capabilities: agent.NativeSessionCapabilities{Status: true, Observe: true, Input: true, Interrupt: true}}, TurnID: "turn-1"}
}
func (c *testConnection) Updates() <-chan agent.NativeSessionUpdate { return c.updates }
func (c *testConnection) Action(ctx context.Context, a agent.NativeSessionAction) (agent.NativeSessionReceipt, error) {
	c.actions.Add(1)
	if c.accept != nil {
		select {
		case <-c.accept:
		case <-ctx.Done():
			return agent.NativeSessionReceipt{}, ctx.Err()
		}
	}
	return agent.NativeSessionReceipt{RequestID: a.RequestID, Status: "accepted", TurnID: "turn-1"}, nil
}
func (c *testConnection) Close() { c.once.Do(func() { close(c.closed); close(c.updates) }) }
func nativeCode(err error) string {
	var e *agent.NativeSessionError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func await(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native operation did not complete")
}

func TestNativeRunAcknowledgesResponsesAndStopsOnlyItsTurn(t *testing.T) {
	s := New()
	defer s.Close()
	c := newTestConnection()
	ad := &testAdapter{connection: c}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	input := make(chan agent.Inbound, 2)
	done := make(chan agent.TurnResult, 1)
	go func() {
		result, _ := s.RunTurn(ctx, ad, agent.TurnInput{SessionID: "session", CWD: t.TempDir(), Message: "Continue", Inbound: input}, func(agent.Event) {})
		done <- result
	}()
	await(t, func() bool { return c.actions.Load() == 1 })
	ack := make(chan error, 1)
	input <- agent.Inbound{Kind: "response", InteractionID: "approval", Decision: "allow", Ack: ack}
	select {
	case err := <-ack:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("native response never acknowledged to its caller")
	}
	cancel()
	result := <-done
	if !result.Cancelled || ad.opened.Load() != 1 || c.actions.Load() != 3 {
		t.Fatalf("owner lifecycle: result=%+v opens=%d actions=%d", result, ad.opened.Load(), c.actions.Load())
	}
	select {
	case <-c.closed:
		t.Fatal("Stop killed the native owner connection")
	default:
	}
}

func TestSharedAttachmentSurvivesViewerCancellationAndDetachesAfterGrace(t *testing.T) {
	s := New()
	s.grace = 20 * time.Millisecond
	defer s.Close()
	gate := make(chan struct{})
	c := newTestConnection()
	ad := &testAdapter{connection: c, opening: gate}
	q := agent.NativeSessionQuery{SessionID: "native-session", CWD: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, _, _, release, err := s.Subscribe(ctx, ad, q, "", 0)
		if release != nil {
			release()
		}
		first <- err
	}()
	await(t, func() bool { return ad.opened.Load() == 1 })
	type attached struct {
		base    Snapshot
		release func()
		err     error
	}
	second := make(chan attached, 1)
	go func() {
		base, _, _, release, err := s.Subscribe(context.Background(), ad, q, "", 0)
		second <- attached{base, release, err}
	}()
	await(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.entries[key(ad, q)].refs == 2 })
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled viewer: %v", err)
	}
	close(gate)
	two := <-second
	if two.err != nil || ad.opened.Load() != 1 {
		t.Fatalf("attachment duplicated/canceled: %v opens=%d", two.err, ad.opened.Load())
	}
	two.release()
	// Reopening inside the grace window reuses the same epoch and process.
	base, _, _, detach, err := s.Subscribe(context.Background(), ad, q, "", 0)
	if err != nil || base.ConnectionID != two.base.ConnectionID {
		t.Fatalf("rapid reopen: %v", err)
	}
	s.closeEntry(key(ad, q), s.entries[key(ad, q)], true)
	select {
	case <-c.closed:
		t.Fatal("idle close raced an active viewer")
	default:
	}
	detach()
	select {
	case <-c.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("idle viewer was not released")
	}
	if c.actions.Load() != 0 {
		t.Fatal("opening or closing a viewer controlled the native owner")
	}
}

func TestNativeActionsDeduplicateConcurrentAndCanceledCallers(t *testing.T) {
	s := New()
	defer s.Close()
	gate := make(chan struct{})
	c := newTestConnection()
	c.accept = gate
	ad := &testAdapter{connection: c}
	q := agent.NativeSessionQuery{SessionID: "native-session", CWD: t.TempDir()}
	base, _, _, detach, err := s.Subscribe(context.Background(), ad, q, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	a := agent.NativeSessionAction{ConnectionID: base.ConnectionID, RequestID: "request-one", Kind: "input", Text: "Continue"}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := s.Action(ctx, ad, q, a); first <- err }()
	await(t, func() bool { return c.actions.Load() == 1 })
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	changed := a
	changed.Text = "Different message"
	if _, err := s.Action(context.Background(), ad, q, changed); nativeCode(err) != "native_request_conflict" {
		t.Fatalf("ID conflict: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := s.Action(context.Background(), ad, q, a)
			if err == nil && receipt.TurnID != "turn-1" {
				err = errors.New("missing native receipt")
			}
			results <- err
		}()
	}
	close(gate)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if c.actions.Load() != 1 {
		t.Fatalf("native input sent %d times", c.actions.Load())
	}
	s.Close()
	if _, err := s.Action(context.Background(), ad, q, a); nativeCode(err) != "native_connection_changed" {
		t.Fatalf("expired epoch allowed: %v", err)
	}
	if c.actions.Load() != 1 {
		t.Fatal("expired action was resubmitted")
	}
}

func TestNativeReplayAndOversizedHistoryRemainBounded(t *testing.T) {
	s := New()
	defer s.Close()
	c := newTestConnection()
	ad := &testAdapter{connection: c}
	q := agent.NativeSessionQuery{SessionID: "native-session", CWD: t.TempDir()}
	base, _, _, detach, err := s.Subscribe(context.Background(), ad, q, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	s.mu.Lock()
	e := s.entries[key(ad, q)]
	for i := range maxReplay + 4 {
		ev := agent.Event{Type: agent.EventText, ItemID: "answer", Text: "x", Delta: true}
		s.apply(e, agent.NativeSessionUpdate{Event: &ev, TurnID: "turn-1"})
		_ = i
	}
	last := e.sequence
	s.mu.Unlock()
	resume, replay, _, done, err := s.Subscribe(context.Background(), ad, q, base.ConnectionID, last-2)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if !resume.Replay || len(replay) != 2 || replay[0].Sequence != last-1 || resume.Parts != nil {
		t.Fatal("valid cursor was not replayed exactly")
	}
	fresh, replay, _, done, err := s.Subscribe(context.Background(), ad, q, base.ConnectionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if fresh.Replay || len(replay) != 0 || len(fresh.Parts) != 1 || len(fresh.Parts[0].Text) != maxReplay+4 {
		t.Fatal("evicted cursor did not get replacement snapshot")
	}
	s.mu.Lock()
	huge := agent.Event{Type: agent.EventText, ItemID: "huge", Text: strings.Repeat("x", maxTurnBytes+1)}
	s.apply(e, agent.NativeSessionUpdate{Event: &huge, TurnID: "turn-1"})
	snapshot := snapshot(e)
	bytes := e.bytes
	lastEvent := e.buffer[len(e.buffer)-1]
	s.mu.Unlock()
	if !snapshot.HistoryRequired || bytes > maxReplayBytes || lastEvent.Event != nil || len(snapshot.Parts) != 0 {
		t.Fatal("oversized item escaped the history/replay bounds")
	}
}

func TestNativeStatusCoalescesWithoutOpeningOrPolling(t *testing.T) {
	s := New()
	defer s.Close()
	gate := make(chan struct{})
	ad := &testAdapter{connection: newTestConnection(), opening: gate}
	q := agent.NativeSessionQuery{SessionID: "native-session", CWD: t.TempDir()}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.Activity(context.Background(), ad, q)
			if err == nil && v.State != "running" {
				err = errors.New("wrong state")
			}
			errs <- err
		}()
	}
	await(t, func() bool { return ad.probed.Load() == 1 })
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Activity(context.Background(), ad, q)
	if err != nil {
		t.Fatal(err)
	}
	if ad.probed.Load() != 1 || ad.opened.Load() != 0 {
		t.Fatalf("status duplicated work: probes=%d opens=%d", ad.probed.Load(), ad.opened.Load())
	}
}
