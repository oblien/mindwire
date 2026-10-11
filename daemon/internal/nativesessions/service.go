// Package nativesessions shares on-demand native attachments between viewers.
// It owns no harness processes or transcript files, and does no idle scanning.
package nativesessions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

const (
	maxConnections = 32
	maxReplay      = 512
	maxReplayBytes = 2 << 20
	maxTurnBytes   = 8 << 20
	maxReceipts    = 2048
	closeGrace     = 30 * time.Second
)

type Snapshot struct {
	ConnectionID    string                `json:"connectionId"`
	Sequence        int64                 `json:"sequence"`
	TurnID          string                `json:"turnId,omitempty"`
	Activity        agent.SessionActivity `json:"activity"`
	Parts           []agent.Part          `json:"parts"`
	Inputs          []agent.UserInput     `json:"inputs"`
	HistoryRequired bool                  `json:"historyRequired,omitempty"`
	Partial         bool                  `json:"partial,omitempty"`
	Replay          bool                  `json:"replay,omitempty"` // retain the previous projection and apply replay events
}

type Event struct {
	ConnectionID string `json:"connectionId"`
	Sequence     int64  `json:"sequence"`
	agent.NativeSessionUpdate
}

// Frame is the SDK/SSE envelope. A resume frame acknowledges a replay cursor;
// unlike a snapshot, it must not replace the client's existing projection.
type Frame struct {
	Type     string    `json:"type"` // snapshot | resume | update
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Update   *Event    `json:"update,omitempty"`
}

type receipt struct {
	digest [32]byte
	done   chan struct{}
	result agent.NativeSessionReceipt
	err    error
}

type entry struct {
	ready            chan struct{}
	connection       agent.NativeSessionConnection
	err              error
	id               string
	refs             int
	timer            *time.Timer
	closed           bool
	activity         agent.SessionActivity
	transcript       stream.Transcript
	inputs           []agent.UserInput
	sequence         int64
	turnID           string
	buffer           []Event
	bytes, turnBytes int
	historyRequired  bool
	partial          bool
	subs             map[chan Event]struct{}
	receipts         map[string]*receipt
}

type Service struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	entries  map[string]*entry
	grace    time.Duration
	activity map[string]*activityRead
}

type activityRead struct {
	done  chan struct{}
	at    time.Time
	value agent.SessionActivity
	err   error
}

func New() *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{ctx: ctx, cancel: cancel, entries: map[string]*entry{}, activity: map[string]*activityRead{}, grace: closeGrace}
}

func key(ad agent.Adapter, q agent.NativeSessionQuery) string {
	return ad.ID() + "\x00" + q.SessionID + "\x00" + q.CWD
}

func (s *Service) Activity(ctx context.Context, ad agent.Adapter, q agent.NativeSessionQuery) (agent.SessionActivity, error) {
	if err := agent.ValidateNativeSessionQuery(q); err != nil {
		return agent.SessionActivity{}, err
	}
	k := key(ad, q)
	s.mu.Lock()
	e := s.entries[k]
	if e != nil && e.connection != nil && !e.closed {
		value := e.activity
		s.mu.Unlock()
		return value, nil
	}
	mod, ok := ad.(agent.NativeSessionModule)
	if !ok {
		s.mu.Unlock()
		return agent.NativeSessionUnavailable(q, "harness_unsupported"), nil
	}
	read := s.activity[k]
	if read == nil || !read.at.IsZero() && time.Since(read.at) >= time.Second {
		// Only callers perform discovery. The bounded cache shares concurrent
		// status checks; it has no polling worker while the app is closed.
		if len(s.activity) >= 256 {
			for id, old := range s.activity {
				if !old.at.IsZero() {
					delete(s.activity, id)
				}
			}
		}
		if len(s.activity) >= 256 {
			s.mu.Unlock()
			return agent.NativeSessionUnavailable(q, "native_status_busy"), nil
		}
		read = &activityRead{done: make(chan struct{})}
		s.activity[k] = read
		go func() {
			probe, cancel := context.WithTimeout(s.ctx, 6*time.Second)
			defer cancel()
			value, err := mod.SessionActivity(probe, q)
			s.mu.Lock()
			read.value, read.err, read.at = value, err, time.Now()
			close(read.done)
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return agent.SessionActivity{}, ctx.Err()
	case <-read.done:
		return read.value, read.err
	}
}

func (s *Service) acquire(ctx context.Context, ad agent.Adapter, q agent.NativeSessionQuery) (*entry, func(), error) {
	if err := agent.ValidateNativeSessionQuery(q); err != nil {
		return nil, nil, err
	}
	mod, ok := ad.(agent.NativeSessionModule)
	if !ok {
		return nil, nil, agent.NativeActionUnsupported("live attachment")
	}
	k := key(ad, q)
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil, nil, context.Canceled
	}
	e := s.entries[k]
	if e == nil || e.closed {
		if len(s.entries) >= maxConnections {
			s.mu.Unlock()
			return nil, nil, agent.NativeError("native_session_capacity", "Too many native conversations are being watched.")
		}
		e = &entry{id: rand.Text(), ready: make(chan struct{}), subs: map[chan Event]struct{}{}, receipts: map[string]*receipt{}}
		s.entries[k] = e
		go s.open(k, e, mod, q)
	}
	e.refs++
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	s.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { s.release(k, e) }) }
	select {
	case <-ctx.Done():
		release()
		return nil, nil, ctx.Err()
	case <-e.ready:
		s.mu.Lock()
		err := e.err
		if e.closed && err == nil {
			err = agent.NativeError("native_connection_lost", "The native session disconnected. Reconnect to read its current state.")
		}
		s.mu.Unlock()
		if err != nil {
			release()
			return nil, nil, err
		}
		return e, release, nil
	}
}

func (s *Service) open(k string, e *entry, mod agent.NativeSessionModule, q agent.NativeSessionQuery) {
	// One opening job survives any individual viewer's cancellation. The adapter
	// bounds its handshake; the returned connection lives until Close, not HTTP EOF.
	connection, err := mod.OpenNativeSession(s.ctx, q)
	s.mu.Lock()
	e.err = err
	if err != nil || s.ctx.Err() != nil || e.closed {
		e.closed = true
		if s.entries[k] == e {
			delete(s.entries, k)
		}
		close(e.ready)
		s.mu.Unlock()
		if connection != nil {
			connection.Close()
		}
		return
	}
	e.connection = connection
	initial := connection.Initial()
	e.activity = initial.Activity
	e.turnID, e.historyRequired = initial.TurnID, initial.HistoryRequired
	e.partial = initial.HistoryRequired
	for _, ev := range initial.Events {
		s.apply(e, agent.NativeSessionUpdate{Event: &ev, TurnID: initial.TurnID})
	}
	close(e.ready)
	s.mu.Unlock()
	for update := range connection.Updates() {
		s.mu.Lock()
		if !e.closed {
			s.apply(e, update)
		}
		s.mu.Unlock()
	}
	s.closeEntry(k, e, false)
}

func (s *Service) apply(e *entry, u agent.NativeSessionUpdate) {
	if u.TurnID != "" {
		e.turnID = u.TurnID
	}
	if u.Reset {
		e.transcript = stream.Transcript{}
		e.inputs = nil
		e.turnBytes = 0
		e.historyRequired = false
		e.partial = false
	}
	if u.Activity != nil {
		e.activity = *u.Activity
	}
	if u.HistoryChanged {
		e.historyRequired = true
	}
	if u.Partial {
		e.partial = true
	}
	e.sequence++
	ev := Event{ConnectionID: e.id, Sequence: e.sequence, NativeSessionUpdate: u}
	data, _ := json.Marshal(ev)
	e.turnBytes += len(data)
	if e.turnBytes > maxTurnBytes {
		e.transcript = stream.Transcript{}
		e.inputs = nil
		e.turnBytes = len(data)
		e.historyRequired = true
		e.partial = true
		ev.HistoryChanged = true
		ev.Reset = true
		ev.Partial = true
	}
	// A single oversized item must not defeat either projection or replay limits.
	// Native history remains authoritative and can fetch its full content later.
	if len(data) > maxReplayBytes {
		ev.Event = nil
		ev.HistoryChanged = true
		e.historyRequired = true
		e.partial = true
		ev.Partial = true
		data, _ = json.Marshal(ev)
	}
	if ev.Event != nil {
		item := *ev.Event
		item.Sequence = e.sequence
		e.transcript.Apply(item)
		if item.Type == agent.EventInput && item.Input != nil {
			found := false
			for i, old := range e.inputs {
				if old.ID == item.Input.ID {
					e.inputs[i] = *item.Input
					found = true
					break
				}
			}
			if !found && len(e.inputs) < 256 {
				e.inputs = append(e.inputs, *item.Input)
			}
		}
	}
	data, _ = json.Marshal(ev)
	e.buffer = append(e.buffer, ev)
	e.bytes += len(data)
	for len(e.buffer) > maxReplay || e.bytes > maxReplayBytes {
		old, _ := json.Marshal(e.buffer[0])
		e.bytes -= len(old)
		e.buffer[0] = Event{}
		e.buffer = e.buffer[1:]
	}
	for ch := range e.subs {
		select {
		case ch <- ev:
		default:
			close(ch)
			delete(e.subs, ch)
		}
	}
}

func snapshot(e *entry) Snapshot {
	return Snapshot{ConnectionID: e.id, Sequence: e.sequence, TurnID: e.turnID, Activity: e.activity,
		Parts: e.transcript.Snapshot(false).Parts, Inputs: append([]agent.UserInput{}, e.inputs...), HistoryRequired: e.historyRequired, Partial: e.partial}
}

// Subscribe registers the viewer and reads its baseline under the same lock as
// publication. A stale/evicted cursor gets a replacement snapshot, never a gap.
func (s *Service) Subscribe(ctx context.Context, ad agent.Adapter, q agent.NativeSessionQuery, connectionID string, after int64) (Snapshot, []Event, <-chan Event, func(), error) {
	e, release, err := s.acquire(ctx, ad, q)
	if err != nil {
		return Snapshot{}, nil, nil, nil, err
	}
	s.mu.Lock()
	if e.closed {
		s.mu.Unlock()
		release()
		return Snapshot{}, nil, nil, nil, agent.NativeError("native_connection_lost", "The native session disconnected.")
	}
	base := snapshot(e)
	var replay []Event
	if connectionID == e.id && after >= 0 && after <= e.sequence && (after == e.sequence || len(e.buffer) > 0 && after >= e.buffer[0].Sequence-1) {
		for _, ev := range e.buffer {
			if ev.Sequence > after {
				replay = append(replay, ev)
			}
		}
		// A replay uses the caller's existing state, not the current replacement snapshot.
		base.Parts = nil
		base.Inputs = nil
		base.Sequence = after
		base.Replay = true
	}
	ch := make(chan Event, 256)
	e.subs[ch] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			if _, ok := e.subs[ch]; ok {
				delete(e.subs, ch)
				close(ch)
			}
			s.mu.Unlock()
			release()
		})
	}
	return base, replay, ch, cancel, nil
}

func (s *Service) Action(ctx context.Context, ad agent.Adapter, q agent.NativeSessionQuery, action agent.NativeSessionAction) (agent.NativeSessionReceipt, error) {
	if action.ConnectionID == "" || !agent.ValidNativeSessionID(action.RequestID) {
		return agent.NativeSessionReceipt{}, agent.NativeError("native_action_invalid", "connectionId and requestId are required.")
	}
	if action.Kind != "input" && action.Kind != "interrupt" && action.Kind != "response" {
		return agent.NativeSessionReceipt{}, agent.NativeError("native_action_invalid", "Unknown native action.")
	}
	if action.Kind == "input" && strings.TrimSpace(action.Text) == "" && len(action.Attachments) == 0 {
		return agent.NativeSessionReceipt{}, agent.NativeError("native_action_invalid", "A message or attachment is required.")
	}
	s.mu.Lock()
	e := s.entries[key(ad, q)]
	if e == nil || e.closed || e.id != action.ConnectionID || e.connection == nil {
		s.mu.Unlock()
		return agent.NativeSessionReceipt{}, agent.NativeError("native_connection_changed", "Reconnect to the conversation before sending. The previous attachment has ended.")
	}
	encoded, _ := json.Marshal(action)
	digest := sha256.Sum256(encoded)
	if old := e.receipts[action.RequestID]; old != nil {
		s.mu.Unlock()
		if old.digest != digest {
			return agent.NativeSessionReceipt{}, agent.NativeError("native_request_conflict", "This requestId was already used for another action.")
		}
		select {
		case <-ctx.Done():
			return agent.NativeSessionReceipt{}, ctx.Err()
		case <-old.done:
			return old.result, old.err
		}
	}
	if len(e.receipts) >= maxReceipts {
		s.mu.Unlock()
		return agent.NativeSessionReceipt{}, agent.NativeError("native_action_capacity", "Reconnect after closing this conversation to start a new control connection.")
	}
	r := &receipt{digest: digest, done: make(chan struct{})}
	e.receipts[action.RequestID] = r
	e.refs++
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	s.mu.Unlock()
	// A disconnected HTTP caller must not turn an accepted native send into an
	// automatic resend. Complete and retain its acknowledgement independently.
	go func() {
		defer s.release(key(ad, q), e)
		actionCtx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		defer cancel()
		r.result, r.err = e.connection.Action(actionCtx, action)
		if errors.Is(r.err, context.DeadlineExceeded) || errors.Is(r.err, context.Canceled) {
			r.err = agent.NativeError("native_action_unknown", "The native acknowledgement was interrupted. Check the conversation before sending again.")
		}
		close(r.done)
	}()
	select {
	case <-ctx.Done():
		return agent.NativeSessionReceipt{}, ctx.Err()
	case <-r.done:
		return r.result, r.err
	}
}

func (s *Service) release(k string, e *entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.refs--
	if e.refs == 0 && !e.closed {
		e.timer = time.AfterFunc(s.grace, func() {
			s.closeEntry(k, e, true)
		})
	}
}

func (s *Service) closeEntry(k string, e *entry, idleOnly bool) {
	s.mu.Lock()
	if e.closed || idleOnly && e.refs != 0 {
		s.mu.Unlock()
		return
	}
	e.closed = true
	if e.timer != nil {
		e.timer.Stop()
	}
	if s.entries[k] == e {
		delete(s.entries, k)
	}
	delete(s.activity, k)
	for ch := range e.subs {
		close(ch)
		delete(e.subs, ch)
	}
	connection := e.connection
	s.mu.Unlock()
	if connection != nil {
		connection.Close()
	}
}

func (s *Service) Close() {
	s.cancel()
	s.mu.Lock()
	entries := make(map[string]*entry, len(s.entries))
	for k, e := range s.entries {
		entries[k] = e
	}
	s.mu.Unlock()
	for k, e := range entries {
		s.closeEntry(k, e, false)
	}
}
