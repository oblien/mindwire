// Package conversations projects native harness session inventories into the
// existing workspace metadata protocol. It never owns or copies transcripts.
package conversations

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

type Query struct {
	ProjectID string `json:"projectId,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Refresh   bool   `json:"refresh,omitempty"`
}

type entry struct {
	done    chan struct{}
	checked time.Time
	err     error
}

type Index struct {
	registry *registry.Store
	store    *session.Store
	adapters []agent.Adapter
	gate     *sync.Mutex
	busy     func(string) bool
	mu       sync.Mutex
	entries  map[string]*entry
	now      func() time.Time
}

func New(reg *registry.Store, store *session.Store, adapters []agent.Adapter, gate *sync.Mutex, busy func(string) bool) *Index {
	return &Index{registry: reg, store: store, adapters: adapters, gate: gate, busy: busy, entries: map[string]*entry{}, now: time.Now}
}

// Refresh coalesces concurrent callers per project/harness. Healthy inventories
// are reused for a minute; an explicit refresh bypasses that cache. Errors keep
// the last successful links and briefly back off instead of pruning as empty.
func (idx *Index) Refresh(ctx context.Context, q Query) ([]registry.SessionDiscoveryIssue, error) {
	if idx == nil {
		return nil, nil
	}
	snapshot, err := idx.registry.Snapshot(nil)
	if err != nil {
		return nil, err
	}
	cwd := ""
	if q.CWD != "" {
		cwd, err = workspacepath.Canonical(q.CWD)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid conversation directory", registry.ErrInvalid)
		}
	}
	type job struct {
		project registry.Project
		cwd     string
		adapter agent.Adapter
		lister  agent.SessionLister
	}
	var jobs []job
	for _, project := range snapshot.Projects {
		if q.ProjectID != "" && q.ProjectID != project.ID {
			continue
		}
		path, err := workspacepath.Canonical(project.Path)
		if err != nil {
			continue
		}
		if cwd != "" && cwd != path {
			continue
		}
		for _, adapter := range idx.adapters {
			if lister, ok := adapter.(agent.SessionLister); ok {
				jobs = append(jobs, job{project, path, adapter, lister})
			}
		}
	}
	// Bound both native subprocess concurrency and the whole metadata request.
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var issues []registry.SessionDiscoveryIssue
	queue := make(chan job, len(jobs))
	for _, j := range jobs {
		queue <- j
	}
	close(queue)
	for n := 0; n < min(4, len(jobs)); n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				if err := idx.refreshOne(ctx, j.project, j.cwd, j.adapter, j.lister, q.Refresh); err != nil {
					// Native process errors can contain credentials or prompt snippets.
					// Expose a useful, stable message without forwarding arbitrary stderr.
					mu.Lock()
					issues = append(issues, registry.SessionDiscoveryIssue{ProjectID: j.project.ID, Agent: j.adapter.ID(),
						Message: "Could not refresh " + j.adapter.Meta().Name + " conversations. Check the harness installation and refresh."})
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].ProjectID == issues[j].ProjectID {
			return issues[i].Agent < issues[j].Agent
		}
		return issues[i].ProjectID < issues[j].ProjectID
	})
	return issues, nil
}

func (idx *Index) refreshOne(ctx context.Context, project registry.Project, cwd string, adapter agent.Adapter, lister agent.SessionLister, force bool) error {
	key := project.ID + "\x00" + cwd + "\x00" + adapter.ID()
	idx.mu.Lock()
	if cached := idx.entries[key]; cached != nil {
		if cached.done != nil {
			done := cached.done
			idx.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-done:
				return cached.err
			}
		}
		ttl := time.Minute
		if cached.err != nil {
			ttl = 5 * time.Second
		}
		if !force && idx.now().Sub(cached.checked) < ttl {
			idx.mu.Unlock()
			return cached.err
		}
	}
	flight := &entry{done: make(chan struct{})}
	idx.entries[key] = flight
	idx.mu.Unlock()
	rows, err := lister.ListSessions(ctx, project.Path)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		idx.gate.Lock()
		err = idx.registry.SyncNativeSessions(registry.NativeInventory{ProjectID: project.ID, CWD: cwd,
			AgentType: adapter.ID(), AgentName: adapter.Meta().Name, Sessions: rows}, idx.store, idx.busy)
		idx.gate.Unlock()
	}
	idx.mu.Lock()
	flight.err, flight.checked = err, idx.now()
	close(flight.done)
	flight.done = nil
	idx.mu.Unlock()
	return err
}

// Filter keeps the protocol's existing chat summaries, scoped to a registered
// project or exact directory. It never invents a project for an arbitrary path.
func Filter(summaries []session.ChatSummary, snapshot registry.Snapshot, store *session.Store, q Query) []session.ChatSummary {
	projects := map[string]bool{}
	cwd, _ := workspacepath.Canonical(q.CWD)
	for _, p := range snapshot.Projects {
		path, _ := workspacepath.Canonical(p.Path)
		if (q.ProjectID == "" || q.ProjectID == p.ID) && (q.CWD == "" || cwd == path) {
			projects[p.ID] = true
		}
	}
	chats := map[string]bool{}
	for _, chat := range snapshot.Chats {
		if projects[chat.ProjectID] {
			chats[chat.ID] = true
		}
	}
	out := make([]session.ChatSummary, 0, len(summaries))
	for _, summary := range summaries {
		if q.ProjectID != "" || q.CWD != "" {
			if !chats[summary.ChatID] {
				path, _ := workspacepath.Canonical(store.ChatCWD(summary.ChatID))
				if q.ProjectID != "" || q.CWD == "" || path != cwd {
					continue
				}
			}
		}
		out = append(out, summary)
	}
	return out
}
