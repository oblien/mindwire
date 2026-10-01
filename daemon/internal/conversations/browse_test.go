package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

type globalAdapter struct {
	agent.Adapter
	mu               sync.Mutex
	rows             []agent.NativeSession
	calls            int
	fail             bool
	started, release chan struct{}
}

func (*globalAdapter) ID() string { return "native-fixture" }
func (*globalAdapter) Meta() agent.CatalogEntry {
	return agent.CatalogEntry{ID: "native-fixture", Name: "Native fixture"}
}
func (f *globalAdapter) ListSessions(ctx context.Context, cwd string) ([]agent.NativeSession, error) {
	f.mu.Lock()
	f.calls++
	rows, fail := append([]agent.NativeSession{}, f.rows...), f.fail
	f.mu.Unlock()
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
	if fail {
		return nil, errors.New("sensitive native stderr")
	}
	if cwd == "" {
		return rows, nil
	}
	var selected []agent.NativeSession
	for _, row := range rows {
		if row.CWD == cwd {
			selected = append(selected, row)
		}
	}
	return selected, nil
}

func globalFixture(t *testing.T) (*Index, *globalAdapter) {
	t.Helper()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := workspacepath.Canonical(t.TempDir())
	second, _ := workspacepath.Canonical(t.TempDir())
	f := &globalAdapter{rows: []agent.NativeSession{
		{ID: "one", CWD: first, Title: "First folder work", CreatedAt: "2026-10-01T10:00:00Z", UpdatedAt: "2026-10-01T12:00:00Z"},
		{ID: "two", CWD: second, Title: "Second folder work", CreatedAt: "2026-10-01T10:00:00Z", UpdatedAt: "2026-10-01T11:00:00Z"},
	}}
	return New(reg, store, []agent.Adapter{f}, &sync.Mutex{}, nil), f
}

func browse(t *testing.T, idx *Index, q BrowseQuery) Page {
	t.Helper()
	p, err := idx.Browse(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGlobalBrowseIsReadOnlyPaginatedSearchableAndFolderIndependent(t *testing.T) {
	idx, f := globalFixture(t)
	before, _ := idx.registry.Snapshot(nil)
	first := browse(t, idx, BrowseQuery{Limit: 1})
	if len(first.Items) != 1 || first.Total != 2 || first.NextCursor == "" || first.Items[0].CWD != f.rows[0].CWD {
		t.Fatalf("first page: %+v", first)
	}
	second := browse(t, idx, BrowseQuery{Limit: 1, Cursor: first.NextCursor})
	if len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID || second.NextCursor != "" {
		t.Fatalf("second page: %+v", second)
	}
	if got := browse(t, idx, BrowseQuery{Search: "SECOND"}); got.Total != 1 || got.Items[0].CWD != f.rows[1].CWD {
		t.Fatal(got)
	}
	if _, err := idx.Browse(context.Background(), BrowseQuery{Search: "different", Cursor: first.NextCursor}); !errors.Is(err, registry.ErrInvalid) {
		t.Fatal(err)
	}
	if got := browse(t, idx, BrowseQuery{Search: f.rows[0].CWD}); got.Total != 1 {
		t.Fatal(got)
	}
	after, _ := idx.registry.Snapshot(nil)
	if after.Revision != before.Revision || len(after.Chats)+len(after.Agents)+len(after.Projects) != 0 || len(idx.store.ChatSessions()) != 0 {
		t.Fatal("browsing mutated the workspace or native mappings")
	}
	if f.calls != 1 {
		t.Fatal("pagination/search rescanned native history", f.calls)
	}
}

func TestGlobalOpenIsIdempotentKeepsOriginalContextAndNeverPrunes(t *testing.T) {
	idx, f := globalFixture(t)
	profile := registry.Agent{Record: registry.Record{ID: "preferred"}, AgentType: f.ID(), Name: "My profile"}
	if err := idx.registry.Import(registry.Import{Agents: []registry.Agent{profile}}); err != nil {
		t.Fatal(err)
	}
	page := browse(t, idx, BrowseQuery{AgentID: profile.ID})
	request := OpenRequest{ID: page.Items[0].ID, AgentID: profile.ID}
	var wg sync.WaitGroup
	results := make(chan OpenResult, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := idx.Open(context.Background(), request)
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	id := ""
	for result := range results {
		if id != "" && result.ChatID != id {
			t.Fatal("concurrent opens duplicated the chat")
		}
		id = result.ChatID
		if len(result.Snapshot.Projects) != 1 || len(result.Snapshot.Chats) != 1 || len(result.Snapshot.Agents) != 1 {
			t.Fatal(result.Snapshot)
		}
		if result.Snapshot.Chats[0].AgentID != profile.ID || result.Snapshot.Projects[0].Path != f.rows[0].CWD {
			t.Fatal("opening lost the profile/directory")
		}
	}
	refs := idx.store.ChatSessions()
	if len(refs) != 1 || refs[0].SID != "one" || refs[0].CWD != f.rows[0].CWD || refs[0].ForkPending {
		t.Fatal(refs)
	}
	if len(idx.store.Chats()) != 0 {
		t.Fatal("opening copied a transcript or started a run")
	}
	second, err := idx.Open(context.Background(), OpenRequest{ID: page.Items[1].ID, AgentID: profile.ID})
	if err != nil || len(second.Snapshot.Chats) != 2 || len(second.Snapshot.Projects) != 2 {
		t.Fatal(second, err)
	}
	got := browse(t, idx, BrowseQuery{})
	if len(got.Items) != 2 || got.Items[0].ID != page.Items[0].ID || got.Items[0].ChatID != id {
		t.Fatal("adoption duplicated browser rows", got)
	}
	// The existing project endpoint remains exact-folder scoped.
	project := second.Snapshot.Projects[0]
	if _, err := idx.Refresh(context.Background(), Query{ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := idx.registry.Snapshot(nil)
	summaries, _ := idx.registry.MergeSummaries(nil)
	if scoped := Filter(summaries, snapshot, idx.store, Query{ProjectID: project.ID}); len(scoped) != 1 {
		t.Fatal(scoped)
	}
}

func TestGlobalBrowseRespectsAliasesProfileOwnershipRenamesAndHiddenChats(t *testing.T) {
	idx, f := globalFixture(t)
	f.rows[0].Aliases = []string{"thread-one"}
	if err := idx.registry.Import(registry.Import{
		Projects: []registry.Project{{Record: registry.Record{ID: "project"}, Name: "Saved folder", Path: f.rows[0].CWD}},
		Agents:   []registry.Agent{{Record: registry.Record{ID: "owner"}, AgentType: f.ID(), Name: "Owner"}, {Record: registry.Record{ID: "other"}, AgentType: f.ID(), Name: "Other"}},
		Chats:    []registry.Chat{{Record: registry.Record{ID: "existing"}, AgentID: "owner", ProjectID: "project", SessionID: "thread-one", Title: "User rename", TitleIsUserSet: true}},
	}); err != nil {
		t.Fatal(err)
	}
	page := browse(t, idx, BrowseQuery{})
	if page.Items[0].ChatID != "existing" || page.Items[0].Title != "User rename" || len(page.Items) != 2 {
		t.Fatal(page)
	}
	if other := browse(t, idx, BrowseQuery{AgentID: "other"}); len(other.Items) != 1 || other.Items[0].ChatID != "" {
		t.Fatal("a different profile stole this chat", other)
	}
	if _, err := idx.Open(context.Background(), OpenRequest{ID: page.Items[0].ID, AgentID: "owner"}); err != nil {
		t.Fatal(err)
	}
	idx.gate.Lock()
	saved, _ := idx.registry.Snapshot(nil)
	revision := saved.Chats[0].Revision
	err := idx.registry.Delete("chats", "existing", &revision, false, idx.store)
	idx.gate.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := browse(t, idx, BrowseQuery{Refresh: true}); len(got.Items) != 1 {
		t.Fatal("hidden chat returned", got)
	}
	if _, err := idx.Open(context.Background(), OpenRequest{ID: page.Items[0].ID}); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("stale browser resurrected deleted chat", err)
	}
}

func TestGlobalOpenMissingFolderAndInvalidProfileLeaveNoPartialMetadata(t *testing.T) {
	idx, f := globalFixture(t)
	page := browse(t, idx, BrowseQuery{})
	if err := os.Remove(f.rows[0].CWD); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Open(context.Background(), OpenRequest{ID: page.Items[0].ID}); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal(err)
	}
	// A failure after project preparation must roll back the whole transaction.
	if _, err := idx.registry.OpenNativeSession(registry.NativeInventory{CWD: f.rows[1].CWD, AgentType: f.ID(), AgentName: "Fixture", AgentID: "missing", Sessions: f.rows[1:]}, idx.store); !errors.Is(err, registry.ErrInvalid) {
		t.Fatal(err)
	}
	snapshot, _ := idx.registry.Snapshot(nil)
	if len(snapshot.Projects)+len(snapshot.Chats)+len(snapshot.Agents) != 0 {
		t.Fatal("failed open left orphan metadata", snapshot)
	}
}

func TestGlobalCacheRetainsRowsOnFailureAndDismissedReaderDoesNotCancelScan(t *testing.T) {
	idx, f := globalFixture(t)
	f.started, f.release = make(chan struct{}, 1), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := idx.Browse(ctx, BrowseQuery{}); finished <- err }()
	<-f.started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(f.release)
	page := browse(t, idx, BrowseQuery{})
	if len(page.Items) != 2 {
		t.Fatal(page)
	}
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	failed := browse(t, idx, BrowseQuery{Refresh: true})
	if len(failed.Items) != 2 || len(failed.Issues) != 1 || failed.Issues[0].Message == "sensitive native stderr" {
		t.Fatal(failed)
	}
	for i := 0; i < 10; i++ {
		browse(t, idx, BrowseQuery{})
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 2 {
		t.Fatal("cache/retry backoff failed", calls)
	}
}

func TestGlobalCursorDoesNotDuplicateAfterNewChatsArrive(t *testing.T) {
	idx, f := globalFixture(t)
	page := browse(t, idx, BrowseQuery{Limit: 1})
	f.mu.Lock()
	f.rows = append(f.rows, agent.NativeSession{ID: "new", CWD: f.rows[0].CWD, Title: "Latest", UpdatedAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
	f.mu.Unlock()
	next := browse(t, idx, BrowseQuery{Limit: 1, Cursor: page.NextCursor, Refresh: true})
	if len(next.Items) != 1 || next.Items[0].Title != "Second folder work" || next.Total != 3 {
		b, _ := json.Marshal(next)
		t.Fatal(string(b))
	}
}

func TestGlobalInventoryRetainsAliasesFromOlderDuplicatePages(t *testing.T) {
	cwd, _ := workspacepath.Canonical(t.TempDir())
	rows := normalizeInventory([]agent.NativeSession{
		{ID: "thread", CWD: cwd, Title: "Newest title", UpdatedAt: "2026-10-01T10:00:00Z"},
		{ID: "native", Aliases: []string{"thread"}, CWD: cwd, Title: "Old title", UpdatedAt: "2026-10-01T09:00:00Z"},
	})
	if len(rows) != 1 || rows[0].Title != "Newest title" || len(rows[0].Aliases) != 1 || rows[0].Aliases[0] != "native" {
		t.Fatalf("duplicate lost the saved native identity: %+v", rows)
	}
}
