// Package inventory discovers workspace software without starting a harness or
// owning its native state. It shares the workspace's existing profile registry.
package inventory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

type scan struct {
	done         chan struct{}
	checked      time.Time
	observations []registry.AgentInstallation
	issues       []registry.AgentDiscoveryIssue
	err          error
}

type Agents struct {
	registry *registry.Store
	adapters []agent.Adapter
	gate     *sync.Mutex
	mu       sync.Mutex
	current  *scan
	inspect  func(context.Context, toolchain.Spec) (string, error)
	now      func() time.Time
}

func NewAgents(reg *registry.Store, adapters []agent.Adapter, gate *sync.Mutex) *Agents {
	return &Agents{registry: reg, adapters: adapters, gate: gate,
		inspect: toolchain.InspectInstallation, now: time.Now}
}

// Refresh is entirely on demand. Concurrent clients share one bounded scan;
// navigation reuses it for a minute, and explicit refresh checks the source again.
// No timers, background pollers, version commands, or external requests are used.
func (idx *Agents) Refresh(ctx context.Context, force bool) ([]registry.AgentDiscoveryIssue, error) {
	if idx == nil {
		return nil, nil
	}
	result, err := idx.discover(ctx, force)
	if err != nil {
		return nil, err
	}
	idx.gate.Lock()
	defer idx.gate.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Reconcile cached observations too: an imported/native-session profile may
	// have appeared since the scan. Only changed observations advance the cursor.
	err = idx.registry.SyncAgentInstallations(result.observations)
	return append([]registry.AgentDiscoveryIssue(nil), result.issues...), err
}

func (idx *Agents) discover(ctx context.Context, force bool) (*scan, error) {
	idx.mu.Lock()
	if cached := idx.current; cached != nil {
		if cached.done != nil {
			done := cached.done
			idx.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				return cached, cached.err
			}
		}
		ttl := time.Minute
		if cached.err != nil || len(cached.issues) > 0 {
			ttl = 5 * time.Second
		}
		if !force && idx.now().Sub(cached.checked) < ttl {
			idx.mu.Unlock()
			return cached, cached.err
		}
	}
	flight := &scan{done: make(chan struct{})}
	idx.current = flight
	idx.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	queue := make(chan agent.Adapter, len(idx.adapters))
	for _, adapter := range idx.adapters {
		if _, ok := adapter.(agent.ToolchainModule); ok {
			queue <- adapter
		}
	}
	close(queue)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for range min(4, len(queue)) {
		wg.Go(func() {
			for adapter := range queue {
				state, err := idx.inspect(ctx, adapter.(agent.ToolchainModule).Toolchain())
				mu.Lock()
				if err != nil {
					// A profile startup script may print secrets. Never relay raw stderr.
					flight.issues = append(flight.issues, registry.AgentDiscoveryIssue{Agent: adapter.ID(),
						Message: "Could not check the " + adapter.Meta().Name + " installation. Refresh to try again."})
				} else {
					flight.observations = append(flight.observations, registry.AgentInstallation{
						AgentType: adapter.ID(), Name: adapter.Meta().Name, State: state})
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	idx.mu.Lock()
	flight.err, flight.checked = ctx.Err(), idx.now()
	sort.Slice(flight.observations, func(i, j int) bool { return flight.observations[i].AgentType < flight.observations[j].AgentType })
	sort.Slice(flight.issues, func(i, j int) bool { return flight.issues[i].Agent < flight.issues[j].Agent })
	close(flight.done)
	flight.done = nil
	idx.mu.Unlock()
	return flight, flight.err
}
