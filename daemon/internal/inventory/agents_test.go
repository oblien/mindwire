package inventory

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/toolchain"
)

type inventoryAdapter struct {
	agent.Adapter
	id string
}

func (a inventoryAdapter) ID() string               { return a.id }
func (a inventoryAdapter) Meta() agent.CatalogEntry { return agent.CatalogEntry{ID: a.id, Name: a.id} }
func (a inventoryAdapter) Toolchain() toolchain.Spec {
	return toolchain.Spec{ID: a.id, Name: a.id, Binary: a.id}
}

func inventoryFixture(t *testing.T) (*Agents, *registry.Store) {
	t.Helper()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	return NewAgents(reg, []agent.Adapter{inventoryAdapter{id: "fixture"}}, &sync.Mutex{}), reg
}

func TestAgentDiscoveryCoalescesCachesAndRetainsLastGoodPresence(t *testing.T) {
	idx, reg := inventoryFixture(t)
	var calls atomic.Int32
	entered, release := make(chan struct{}, 24), make(chan struct{})
	idx.inspect = func(ctx context.Context, spec toolchain.Spec) (string, error) {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
			return "installed", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	results := make(chan error, 24)
	for range 24 {
		go func() { _, err := idx.Refresh(context.Background(), false); results <- err }()
	}
	<-entered
	// A cached scan is applied outside the mutation gate. Reading/editing metadata
	// must remain available while a login-shell lookup is outstanding.
	if _, err := reg.EnsureAgent(registry.EnsureAgentRequest{AgentType: "fixture", Name: "Custom name"}, "Fixture"); err != nil {
		t.Fatal(err)
	}
	close(release)
	for range 24 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent navigation spawned duplicate scans", calls.Load())
	}
	full, _ := reg.Snapshot(nil)
	if len(full.Agents) != 1 || full.Agents[0].Name != "Custom name" || full.Agents[0].Installation != "installed" {
		t.Fatal(full)
	}
	if _, err := idx.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	noChange, _ := reg.Snapshot(nil)
	if calls.Load() != 1 || noChange.Revision != full.Revision {
		t.Fatal("fresh inventory was rescanned or rewritten")
	}
	idx.inspect = func(context.Context, toolchain.Spec) (string, error) {
		calls.Add(1)
		return "", errors.New("secret login error")
	}
	issues, err := idx.Refresh(context.Background(), true)
	if err != nil || len(issues) != 1 || issues[0].Message == "secret login error" {
		t.Fatal(issues, err)
	}
	failed, _ := reg.Snapshot(nil)
	if failed.Revision != full.Revision || failed.Agents[0].Installation != "installed" {
		t.Fatal("failed scan removed a native agent", failed)
	}
	idx.inspect = func(context.Context, toolchain.Spec) (string, error) { return "missing", nil }
	if _, err := idx.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	absent, _ := reg.Snapshot(&full.Revision)
	if len(absent.Agents) != 1 || absent.Agents[0].Installation != "missing" || len(absent.Deleted) != 0 {
		t.Fatal(absent)
	}
}

func TestCancelledAgentScanDoesNotEraseInventoryAndExpiresWithoutPolling(t *testing.T) {
	idx, reg := inventoryFixture(t)
	now := time.Now()
	idx.now = func() time.Time { return now }
	var calls atomic.Int32
	idx.inspect = func(context.Context, toolchain.Spec) (string, error) { calls.Add(1); return "installed", nil }
	if _, err := idx.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	before, _ := reg.Snapshot(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := idx.Refresh(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, _ := reg.Snapshot(nil)
	if before.Revision != after.Revision || len(after.Agents) != 1 {
		t.Fatal("cancelled viewer changed source inventory")
	}
	previous := calls.Load()
	now = now.Add(24 * 24 * time.Hour)
	if calls.Load() != previous {
		t.Fatal("idle time should never start scans")
	}
	if _, err := idx.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != previous+1 {
		t.Fatal("expired inventory did not refresh on demand")
	}
}
