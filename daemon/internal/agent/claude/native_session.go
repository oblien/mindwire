package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/proc"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

var _ agent.NativeSessionModule = adapter{}

type nativeAgentRow struct {
	SessionID  string `json:"sessionId"`
	CWD        string `json:"cwd"`
	Kind       string `json:"kind"`
	PID        int    `json:"pid"`
	Status     string `json:"status"`
	State      string `json:"state"`
	WaitingFor string `json:"waitingFor"`
	StartedAt  int64  `json:"startedAt"`
}

// The official JSON inventory is shared across project viewers. Reading status
// never launches/resumes a Claude conversation or parses its private jobs files.
var nativeInventory struct {
	sync.Mutex
	key     string
	at      time.Time
	rows    []nativeAgentRow
	err     error
	pending chan struct{}
}

type limitedNativeOutput struct{ bytes.Buffer }

func (b *limitedNativeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2<<20 {
		return 0, agent.NativeError("native_inventory_large", "Claude returned an oversized activity list.")
	}
	return b.Buffer.Write(p)
}

func activeNativeAgents(ctx context.Context) ([]nativeAgentRow, error) {
	key := configBase()
	for {
		nativeInventory.Lock()
		if nativeInventory.key == key && time.Since(nativeInventory.at) < 2*time.Second {
			rows, err := nativeInventory.rows, nativeInventory.err
			nativeInventory.Unlock()
			return rows, err
		}
		if pending := nativeInventory.pending; pending != nil {
			nativeInventory.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		shell, env := toolchain.Shell("claude agents --json"), toolchain.Environment()
		nativeInventory.pending = pending
		nativeInventory.Unlock()
		// Cancellation of one viewer does not cancel other viewers' inventory read.
		go func() {
			readCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			cmd := exec.CommandContext(readCtx, "bash", "-lc", shell)
			proc.Group(cmd)
			cmd.Env = env
			var output limitedNativeOutput
			cmd.Stdout = &output
			err := cmd.Run()
			var rows []nativeAgentRow
			if err == nil {
				err = json.Unmarshal(output.Bytes(), &rows)
			}
			nativeInventory.Lock()
			nativeInventory.key = key
			nativeInventory.at = time.Now()
			nativeInventory.rows = rows
			nativeInventory.err = err
			nativeInventory.pending = nil
			close(pending)
			nativeInventory.Unlock()
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending:
		}
	}
}

func (adapter) SessionActivity(ctx context.Context, q agent.NativeSessionQuery) (agent.SessionActivity, error) {
	if err := agent.ValidateNativeSessionQuery(q); err != nil {
		return agent.SessionActivity{}, err
	}
	rows, err := activeNativeAgents(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return agent.SessionActivity{}, ctx.Err()
		}
		return agent.NativeSessionUnavailable(q, "native_status_unavailable"), nil
	}
	return claudeSessionActivity(rows, q)
}

func claudeSessionActivity(rows []nativeAgentRow, q agent.NativeSessionQuery) (agent.SessionActivity, error) {
	expected, err := workspacepath.Canonical(q.CWD)
	if err != nil {
		return agent.SessionActivity{}, err
	}
	activity := agent.SessionActivity{SessionID: q.SessionID, CWD: expected, Owner: "none", State: "idle", Capabilities: agent.NativeSessionCapabilities{Status: true}}
	for _, row := range rows {
		if row.SessionID != q.SessionID {
			continue
		}
		actual, err := workspacepath.Canonical(row.CWD)
		if err != nil || actual != expected {
			return activity, agent.NativeError("native_session_mismatch", "The running Claude conversation belongs to another project.")
		}
		if row.PID <= 0 {
			continue
		}
		activity.Owner = "native"
		activity.Capabilities.Observe = true
		activity.Reason = "terminal_handoff_required"
		if row.StartedAt > 0 {
			activity.StartedAt = time.UnixMilli(row.StartedAt).UTC().Format(time.RFC3339Nano)
		}
		switch row.Status {
		case "busy":
			activity.State = "running"
		case "waiting":
			activity.State = "waiting"
		default:
			activity.State = "idle"
		}
		return activity, nil
	}
	return activity, nil
}

type claudeNativeSession struct {
	initial agent.NativeSessionInitial
	updates chan agent.NativeSessionUpdate
	cancel  context.CancelFunc
}

func (a adapter) OpenNativeSession(ctx context.Context, q agent.NativeSessionQuery) (agent.NativeSessionConnection, error) {
	activity, err := a.SessionActivity(ctx, q)
	if err != nil {
		return nil, err
	}
	if !activity.Capabilities.Observe {
		return nil, agent.NativeError("native_attach_unavailable", "Claude does not report this conversation as open in a native terminal.")
	}
	lifetime, cancel := context.WithCancel(ctx)
	c := &claudeNativeSession{initial: agent.NativeSessionInitial{Activity: activity}, updates: make(chan agent.NativeSessionUpdate, 8), cancel: cancel}
	go func() {
		defer close(c.updates)
		query := agent.HistoryQuery{SessionID: q.SessionID, CWD: q.CWD}
		revision, _ := a.HistoryRevision(query)
		timer := time.NewTicker(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-lifetime.Done():
				return
			case <-timer.C:
			}
			next, err := a.SessionActivity(lifetime, q)
			if err != nil {
				continue
			}
			current, revisionErr := a.HistoryRevision(query)
			changed := revisionErr == nil && current != revision
			if changed {
				revision = current
			}
			if changed || !reflect.DeepEqual(activity, next) {
				activity = next
				select {
				case c.updates <- agent.NativeSessionUpdate{Activity: &next, HistoryChanged: changed}:
				case <-lifetime.Done():
					return
				default:
					return
				}
			}
		}
	}()
	return c, nil
}

func (c *claudeNativeSession) Initial() agent.NativeSessionInitial       { return c.initial }
func (c *claudeNativeSession) Updates() <-chan agent.NativeSessionUpdate { return c.updates }
func (c *claudeNativeSession) Close()                                    { c.cancel() }
func (c *claudeNativeSession) Action(_ context.Context, a agent.NativeSessionAction) (agent.NativeSessionReceipt, error) {
	return agent.NativeSessionReceipt{}, agent.NativeActionUnsupported(a.Kind)
}
