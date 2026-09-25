package conversations

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
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

type listingAdapter struct {
	agent.Adapter
	calls            atomic.Int32
	fail             atomic.Bool
	started, release chan struct{}
}

func (*listingAdapter) ID() string { return "native-fixture" }
func (*listingAdapter) Meta() agent.CatalogEntry {
	return agent.CatalogEntry{ID: "native-fixture", Name: "Native fixture"}
}
func (f *listingAdapter) ListSessions(ctx context.Context, cwd string) ([]agent.NativeSession, error) {
	f.calls.Add(1)
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.release != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
	}
	if f.fail.Load() {
		return nil, errors.New("native listing failed; sensitive stderr must not be returned")
	}
	return []agent.NativeSession{{ID: "native", CWD: cwd, Title: "Existing native conversation", CreatedAt: "2026-09-24T00:00:00Z", UpdatedAt: "2026-09-25T00:00:00Z"}}, nil
}

func indexFixture(t *testing.T, adapter *listingAdapter) (*Index, registry.Project) {
	t.Helper()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cwd, _ := workspacepath.Canonical(t.TempDir())
	project := registry.Project{Record: registry.Record{ID: "project"}, Name: "Project", Path: cwd}
	if err = reg.Import(registry.Import{Projects: []registry.Project{project}}); err != nil {
		t.Fatal(err)
	}
	return New(reg, store, []agent.Adapter{adapter}, &sync.Mutex{}, nil), project
}

func TestNativeDiscoveryCacheRefreshAndFailureKeepMembership(t *testing.T) {
	f := &listingAdapter{}
	idx, _ := indexFixture(t, f)
	refresh := func(force bool) []registry.SessionDiscoveryIssue {
		t.Helper()
		issues, err := idx.Refresh(context.Background(), Query{Refresh: force})
		if err != nil {
			t.Fatal(err)
		}
		return issues
	}
	if issues := refresh(false); len(issues) != 0 {
		t.Fatal(issues)
	}
	first, _ := idx.registry.Snapshot(nil)
	for i := 0; i < 10; i++ {
		refresh(false)
	}
	if f.calls.Load() != 1 {
		t.Fatal("ordinary navigation bypassed the metadata cache")
	}
	refresh(true)
	if f.calls.Load() != 2 {
		t.Fatal("manual refresh did not reach the harness")
	}
	f.fail.Store(true)
	issues := refresh(true)
	if len(issues) != 1 || issues[0].ProjectID != "project" || issues[0].Agent != f.ID() || issues[0].Message == "native listing failed; sensitive stderr must not be returned" {
		t.Fatal(issues)
	}
	after, _ := idx.registry.Snapshot(nil)
	if len(after.Chats) != 1 || after.Revision != first.Revision {
		t.Fatal("failed refresh pruned or changed cached chats")
	}
	refresh(false)
	if f.calls.Load() != 3 {
		t.Fatal("failed listings caused a retry storm")
	}
	idx.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	f.fail.Store(false)
	refresh(false)
	if f.calls.Load() != 4 {
		t.Fatal("expired metadata never refreshed")
	}
}

func TestNativeDiscoverySingleflightAndDoesNotHoldMutationGate(t *testing.T) {
	f := &listingAdapter{started: make(chan struct{}, 2), release: make(chan struct{})}
	idx, project := indexFixture(t, f)
	first := make(chan error, 1)
	go func() { _, err := idx.Refresh(context.Background(), Query{}); first <- err }()
	<-f.started
	// A second reader joins the in-progress native call. Cancellation must not
	// abandon or duplicate the first reader's request.
	ctx, cancel := context.WithCancel(context.Background())
	joined := make(chan error, 1)
	go func() { joined <- idx.refreshOne(ctx, project, project.Path, f, f, true) }()
	cancel()
	if err := <-joined; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Deleting a project while native IO is pending completes immediately.
	idx.gate.Lock()
	err := idx.registry.Delete("projects", project.ID, nil, true, idx.store)
	idx.gate.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	close(f.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 1 {
		t.Fatal("parallel discovery launched duplicate native calls")
	}
	got, _ := idx.registry.Snapshot(nil)
	if len(got.Projects)+len(got.Chats)+len(got.Agents) != 0 {
		t.Fatal("late discovery resurrected a deleted project")
	}
}

func TestNativeDiscoveryDirectoryFilterDoesNotIncludeSiblings(t *testing.T) {
	f := &listingAdapter{}
	idx, project := indexFixture(t, f)
	if _, err := idx.Refresh(context.Background(), Query{CWD: project.Path + "-sibling"}); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 0 {
		t.Fatal("scanned an unrelated folder")
	}
	if _, err := idx.Refresh(context.Background(), Query{ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := idx.registry.Snapshot(nil)
	summaries, _ := idx.registry.MergeSummaries(nil)
	if got := Filter(summaries, snapshot, idx.store, Query{CWD: project.Path}); len(got) != 1 {
		t.Fatal(got)
	}
	if got := Filter(summaries, snapshot, idx.store, Query{CWD: project.Path + "-sibling"}); len(got) != 0 {
		t.Fatal(got)
	}
}
