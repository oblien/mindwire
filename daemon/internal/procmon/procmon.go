// Package procmon samples live CPU/memory for the daemon's per-turn process groups, on demand.
//
// Every turn runs in its own process group (see internal/proc); the group-leader pid is the whole
// process tree for that turn (bash → node → claude/codex/opencode). The Supervisor Track()s a turn's
// group-leader pid for the turn's lifetime and Untrack()s it at teardown, labelling it (agent,
// chatId, runId). A Monitor samples every tracked group on a ticker and broadcasts Frames to
// subscribers — but ONLY while at least one subscriber is connected: the sampler goroutine starts on
// the first Subscribe and stops on the last unsubscribe. So an idle daemon that nobody is watching
// does ZERO background work (no goroutine, no /proc scans) — this is the on-demand / no-leak
// guarantee the console's Agent page relies on.
//
// The sampler itself is platform-split (sample_linux.go / sample_darwin.go / sample_other.go),
// mirroring internal/proc; the lifecycle and delta math here are platform-independent.
package procmon

import (
	"sync"
	"time"
)

const (
	// sampleInterval is how often tracked process groups are sampled while ≥1 subscriber is connected.
	// CPU% is a delta over this window, so it doubles as the smoothing window. A floor, never busier.
	sampleInterval = 1500 * time.Millisecond
	// maxSubscribers caps concurrent watchers so a flood of stream opens can't fan a frame out
	// unboundedly — well above the handful of console tabs a user realistically opens.
	maxSubscribers = 16
	// subBuffer bounds each subscriber's channel; a slow consumer drops frames rather than stalling
	// the sampler (frames are snapshots — the next one supersedes a dropped one).
	subBuffer = 4
)

// Sample is one running turn's live resource use at a single tick. Labels + numbers only — never any
// secret. RSSBytes is resident set size (physical memory) summed over the whole process group.
type Sample struct {
	Agent      string  `json:"agent"`
	ChatID     string  `json:"chatId"`
	RunID      string  `json:"runId"`
	Pid        int     `json:"pid"`
	CPUPercent float64 `json:"cpuPercent"`
	RSSBytes   uint64  `json:"rssBytes"`
}

// Frame is one sampling tick: every currently-tracked turn that had a live process this tick. An
// empty Samples slice (with no running turns, or between ticks) is a valid, meaningful frame.
type Frame struct {
	At      string   `json:"at"` // RFC3339 timestamp of the tick
	Samples []Sample `json:"samples"`
}

// procStat is one process group's CUMULATIVE resource counters at a tick (CPU seconds since the
// processes started, current RSS). The Monitor turns the CPU-seconds delta between ticks into a
// percent — so every platform sampler only has to report cumulative counters, uniformly.
type procStat struct {
	CPUSeconds float64
	RSSBytes   uint64
}

// track is the label + group-leader pid for one tracked turn.
type track struct {
	agent, chatID, runID string
	pgid                 int
}

// Monitor is the refcounted shared sampler. One per Supervisor; Track/Untrack are driven by the turn
// lifecycle and Subscribe by SSE clients. All state is guarded by mu.
type Monitor struct {
	mu     sync.Mutex
	tracks map[string]track   // runId -> track
	subs   map[int]chan Frame // subscriber id -> frame channel
	nextID int

	// Sampler lifecycle: stop is non-nil exactly while the sampler goroutine is running; closing it
	// stops the goroutine. prev/prevAt hold the previous tick's cumulative CPU per pgid for the delta.
	stop   chan struct{}
	prev   map[int]procStat
	prevAt time.Time
}

// NewMonitor builds an idle monitor: no sampler goroutine runs until the first Subscribe.
func NewMonitor() *Monitor {
	return &Monitor{tracks: map[string]track{}, subs: map[int]chan Frame{}}
}

// Track registers a turn's process group so the next sample includes it. Called from a spawn site's
// reporter (via proc.Report) after the process starts; safe to call from any goroutine. Re-tracking
// the same runId (e.g. successive resolve child turns) just updates the pgid.
func (m *Monitor) Track(runID, agent, chatID string, pid int) {
	m.mu.Lock()
	m.tracks[runID] = track{agent: agent, chatID: chatID, runID: runID, pgid: pid}
	m.mu.Unlock()
}

// Untrack drops a turn's process group at turn teardown. Idempotent.
func (m *Monitor) Untrack(runID string) {
	m.mu.Lock()
	delete(m.tracks, runID)
	m.mu.Unlock()
}

// Subscribe registers a frame channel and starts the sampler if this is the first subscriber. The
// returned cancel removes the subscriber and stops the sampler when the last one leaves — so sampling
// happens only while something is watching. ok=false if the subscriber cap is reached; the caller
// should serve a 503-style response and not block.
func (m *Monitor) Subscribe() (ch <-chan Frame, cancel func(), ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.subs) >= maxSubscribers {
		return nil, nil, false
	}
	id := m.nextID
	m.nextID++
	c := make(chan Frame, subBuffer)
	m.subs[id] = c
	if len(m.subs) == 1 {
		m.startLocked()
	}
	var once sync.Once
	cancel = func() {
		once.Do(func() {
			m.mu.Lock()
			delete(m.subs, id)
			if len(m.subs) == 0 {
				m.stopLocked()
			}
			m.mu.Unlock()
		})
	}
	return c, cancel, true
}

// startLocked launches the sampler goroutine and resets its delta state. Caller holds m.mu.
func (m *Monitor) startLocked() {
	stop := make(chan struct{})
	m.stop = stop
	m.prev = map[int]procStat{}
	m.prevAt = time.Time{}
	go m.loop(stop)
}

// stopLocked signals the sampler to exit and clears its delta state. Caller holds m.mu.
func (m *Monitor) stopLocked() {
	if m.stop != nil {
		close(m.stop)
		m.stop = nil
	}
	m.prev = nil
	m.prevAt = time.Time{}
}

// loop is the sampler goroutine: tick until stopped. The stop channel is passed in (not read from
// m.stop) so a restart after a stop can't make this goroutine observe the new run's channel.
func (m *Monitor) loop(stop chan struct{}) {
	t := time.NewTicker(sampleInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			m.tick(now)
		}
	}
}

// tick samples every tracked group once, computes per-group CPU% from the delta since the previous
// tick, and broadcasts a Frame to all subscribers (dropping to a slow consumer rather than stalling).
func (m *Monitor) tick(now time.Time) {
	m.mu.Lock()
	if m.stop == nil { // stopped between ticker wakeup and here
		m.mu.Unlock()
		return
	}
	tracks := make([]track, 0, len(m.tracks))
	pgids := make(map[int]struct{}, len(m.tracks))
	for _, tr := range m.tracks {
		tracks = append(tracks, tr)
		pgids[tr.pgid] = struct{}{}
	}
	prev := m.prev
	prevAt := m.prevAt
	m.mu.Unlock()

	// Only touch the OS when there's something to measure — an idle daemon with a watcher open still
	// does no /proc scanning, just emits empty keep-alive frames.
	var stats map[int]procStat
	if len(pgids) > 0 {
		stats = sampleProcs(pgids)
	}

	dt := 0.0
	if !prevAt.IsZero() {
		dt = now.Sub(prevAt).Seconds()
	}
	cur := make(map[int]procStat, len(stats))
	samples := make([]Sample, 0, len(tracks))
	for _, tr := range tracks {
		st, live := stats[tr.pgid]
		if !live {
			continue // no live process for this group this tick (just spawned / already ended)
		}
		cur[tr.pgid] = st
		cpu := 0.0
		if p, ok := prev[tr.pgid]; ok && dt > 0 {
			if d := st.CPUSeconds - p.CPUSeconds; d > 0 {
				cpu = d / dt * 100
			}
		}
		samples = append(samples, Sample{
			Agent: tr.agent, ChatID: tr.chatID, RunID: tr.runID, Pid: tr.pgid,
			CPUPercent: cpu, RSSBytes: st.RSSBytes,
		})
	}

	frame := Frame{At: now.UTC().Format(time.RFC3339), Samples: samples}

	m.mu.Lock()
	m.prev = cur
	m.prevAt = now
	for _, c := range m.subs {
		select {
		case c <- frame:
		default: // slow consumer: drop this frame, the next supersedes it
		}
	}
	m.mu.Unlock()
}
