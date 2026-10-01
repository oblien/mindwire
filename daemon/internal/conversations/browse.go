package conversations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/registry"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/workspacepath"
)

const BrowserVersion = 1

// BrowseQuery is an explicit, all-folder inventory. Ordinary /chats activity
// polling remains separate and never triggers a global harness scan.
type BrowseQuery struct {
	AgentID string `json:"agentId,omitempty"`
	Search  string `json:"search,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Refresh bool   `json:"refresh,omitempty"`
}

// Conversation is metadata, not a transcript. ChatID is present when a native
// reference has already been attached to this workspace's normal chat protocol.
type Conversation struct {
	ID          string `json:"id"`
	ChatID      string `json:"chatId,omitempty"`
	AgentID     string `json:"agentId,omitempty"`
	AgentType   string `json:"agentType"`
	AgentName   string `json:"agentName"`
	ProjectID   string `json:"projectId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	CWD         string `json:"cwd"`
	Title       string `json:"title"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	LastStatus  string `json:"lastStatus,omitempty"`
	LastRunID   string `json:"lastRunId,omitempty"`
	native      agent.NativeSession
	harnessName string
}

type Page struct {
	WorkspaceID string                           `json:"workspaceId"`
	Items       []Conversation                   `json:"items"`
	Total       int                              `json:"total"`
	NextCursor  string                           `json:"nextCursor,omitempty"`
	Issues      []registry.SessionDiscoveryIssue `json:"issues,omitempty"`
}

type OpenRequest struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId,omitempty"`
}

type OpenResult struct {
	ChatID   string            `json:"chatId"`
	Snapshot registry.Snapshot `json:"snapshot"`
}

type globalEntry struct {
	rows    []agent.NativeSession
	done    chan struct{}
	checked time.Time
	err     error
}

// Global scans are shared across agent and workspace browsers. The short-lived
// worker survives one dismissed screen, so reopening joins the same bounded
// request. There is no background timer, transcript mirror, or idle scan.
func (idx *Index) globalSessions(ctx context.Context, adapter agent.Adapter, force bool) ([]agent.NativeSession, error) {
	idx.mu.Lock()
	previous := idx.global[adapter.ID()]
	if previous != nil {
		if previous.done != nil {
			done := previous.done
			idx.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				return previous.rows, previous.err
			}
		}
		ttl := time.Minute
		if previous.err != nil {
			ttl = 5 * time.Second
		}
		if !force && idx.now().Sub(previous.checked) < ttl {
			idx.mu.Unlock()
			return previous.rows, previous.err
		}
	}
	flight := &globalEntry{done: make(chan struct{})}
	if previous != nil {
		flight.rows = previous.rows
	}
	idx.global[adapter.ID()] = flight
	done := flight.done
	idx.mu.Unlock()
	go func() {
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
		defer cancel()
		rows, err := adapter.(agent.SessionLister).ListSessions(work, "")
		if err == nil {
			err = work.Err()
		}
		if err == nil {
			rows = normalizeInventory(rows)
		}
		idx.mu.Lock()
		if err == nil {
			flight.rows = rows
		}
		flight.err, flight.checked = err, idx.now()
		close(done)
		flight.done = nil
		idx.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return flight.rows, flight.err
	}
}

func nativeKey(harness, cwd, id string) string { return harness + "\x00" + cwd + "\x00" + id }

func aliases(row agent.NativeSession) []string {
	result := []string{row.ID}
	for _, id := range row.Aliases {
		if id != row.ID && agent.ValidNativeSessionID(id) {
			result = append(result, id)
		}
	}
	return result
}

func normalizeInventory(rows []agent.NativeSession) []agent.NativeSession {
	// Newest metadata wins for duplicate aliases returned on adjacent pages.
	rows = append([]agent.NativeSession(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool { return moment(rows[i].UpdatedAt).After(moment(rows[j].UpdatedAt)) })
	seen := map[string]int{}
	result := make([]agent.NativeSession, 0, len(rows))
	for _, row := range rows {
		cwd, err := workspacepath.Canonical(row.CWD)
		if err == nil && agent.ValidNativeSessionID(row.ID) {
			duplicate := 0
			for _, id := range aliases(row) {
				if index := seen[nativeKey("", cwd, id)]; index != 0 {
					duplicate = index
					break
				}
			}
			if duplicate == 0 {
				row.CWD, row.Title = cwd, agent.SessionTitle(row.Title)
				result = append(result, row)
				duplicate = len(result)
			} else {
				// Preserve every native identity even if an older page used another
				// primary ID. Otherwise a saved profile link could be lost on browse.
				for _, id := range aliases(row) {
					if id != result[duplicate-1].ID && seen[nativeKey("", cwd, id)] == 0 {
						result[duplicate-1].Aliases = append(result[duplicate-1].Aliases, id)
					}
				}
			}
			for _, id := range aliases(row) {
				seen[nativeKey("", cwd, id)] = duplicate
			}
		}
	}
	return result
}

func moment(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func newest(a, b string) string {
	if moment(b).After(moment(a)) {
		return b
	}
	return a
}

func before(a, b Conversation) bool {
	at, bt := moment(a.UpdatedAt), moment(b.UpdatedAt)
	if at.Equal(bt) {
		return a.ID < b.ID
	}
	return at.After(bt)
}

// all merges the native inventory with drafts/live runs and client preferences.
// Only references are read; even folders not registered as projects are visible.
func (idx *Index) all(ctx context.Context, q BrowseQuery) (Page, error) {
	if idx == nil {
		return Page{}, fmt.Errorf("%w: conversation browser is unavailable", registry.ErrNotFound)
	}
	snapshot, err := idx.registry.Snapshot(nil)
	if err != nil {
		return Page{}, err
	}
	harness := ""
	if q.AgentID != "" {
		for _, profile := range snapshot.Agents {
			if profile.ID == q.AgentID {
				harness = profile.AgentType
			}
		}
		if harness == "" {
			return Page{}, fmt.Errorf("%w: agent profile no longer exists", registry.ErrNotFound)
		}
	}
	type inventory struct {
		adapter agent.Adapter
		rows    []agent.NativeSession
		err     error
	}
	var inventories []*inventory
	var wg sync.WaitGroup
	for _, adapter := range idx.adapters {
		if _, ok := adapter.(agent.SessionLister); !ok || harness != "" && harness != adapter.ID() {
			continue
		}
		item := &inventory{adapter: adapter}
		inventories = append(inventories, item)
		wg.Add(1)
		go func() {
			defer wg.Done()
			item.rows, item.err = idx.globalSessions(ctx, item.adapter, q.Refresh)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	// Native IO never holds the mutation gate. Read associations after it so a
	// concurrent rename, deletion or turn cannot be overwritten by stale data.
	idx.gate.Lock()
	defer idx.gate.Unlock()
	snapshot, err = idx.registry.Snapshot(nil)
	if err != nil {
		return Page{}, err
	}
	links, err := idx.registry.NativeSessionLinks()
	if err != nil {
		return Page{}, err
	}
	page := Page{WorkspaceID: snapshot.WorkspaceID, Items: []Conversation{}}
	projects := map[string]registry.Project{}
	projectAt := map[string]registry.Project{}
	for _, p := range snapshot.Projects {
		if cwd, err := workspacepath.Canonical(p.Path); err == nil {
			p.Path = cwd
			projects[p.ID], projectAt[cwd] = p, p
		}
	}
	profiles := map[string]registry.Agent{}
	for _, p := range snapshot.Agents {
		profiles[p.ID] = p
	}
	if q.AgentID != "" && profiles[q.AgentID].AgentType != harness {
		return Page{}, fmt.Errorf("%w: agent profile no longer exists", registry.ErrNotFound)
	}
	refs := map[string]session.ChatSession{}
	for _, ref := range idx.store.ChatSessions() {
		refs[ref.ChatID] = ref
	}
	live := map[string]session.ChatSummary{}
	for _, row := range idx.store.Chats() {
		live[row.ChatID] = row
	}
	chats := map[string]registry.Chat{}
	bySession := map[string]string{}
	sort.Slice(snapshot.Chats, func(i, j int) bool { return snapshot.Chats[i].ID < snapshot.Chats[j].ID })
	for _, chat := range snapshot.Chats {
		chats[chat.ID] = chat
		ref := refs[chat.ID]
		if sid := agent.FirstNonEmpty(ref.SID, chat.SessionID); !ref.ForkPending && sid != "" {
			key := nativeKey(profiles[chat.AgentID].AgentType, projects[chat.ProjectID].Path, sid)
			if bySession[key] == "" {
				bySession[key] = chat.ID
			}
		}
	}
	hidden, indexed := map[string]bool{}, map[string]bool{}
	for _, link := range links {
		key := nativeKey(link.AgentType, projects[link.ProjectID].Path, link.SessionID)
		if link.Hidden {
			hidden[key] = true
		}
		if chat, exists := chats[link.ChatID]; exists {
			indexed[chat.ID] = true
			ref := refs[chat.ID]
			if bySession[key] == "" && !ref.ForkPending && (ref.SID == "" || ref.SID == link.SessionID) {
				bySession[key] = chat.ID
			}
		}
	}
	chatRow := func(chat registry.Chat) Conversation {
		p, profile, activity := projects[chat.ProjectID], profiles[chat.AgentID], live[chat.ID]
		return Conversation{ID: "chat:" + chat.ID, ChatID: chat.ID, AgentID: chat.AgentID, AgentType: profile.AgentType,
			AgentName: profile.Name, ProjectID: chat.ProjectID, ProjectName: p.Name, CWD: p.Path, Title: chat.Title,
			CreatedAt: chat.CreatedAt, UpdatedAt: newest(newest(chat.CreatedAt, chat.UpdatedAt), activity.UpdatedAt),
			LastStatus: activity.LastStatus, LastRunID: activity.LastRunID}
	}
	represented, complete := map[string]bool{}, map[string]bool{}
	for _, inventory := range inventories {
		typeID, name := inventory.adapter.ID(), inventory.adapter.Meta().Name
		complete[typeID] = inventory.err == nil
		if inventory.err != nil {
			page.Issues = append(page.Issues, registry.SessionDiscoveryIssue{Agent: typeID,
				Message: "Could not refresh " + name + " conversations. Check the harness installation and refresh."})
		}
		for _, native := range inventory.rows {
			id, suppressed := "", false
			for _, sid := range aliases(native) {
				key := nativeKey(typeID, native.CWD, sid)
				suppressed = suppressed || hidden[key]
				if id == "" {
					id = bySession[key]
				}
			}
			if suppressed {
				continue
			}
			p := projectAt[native.CWD]
			row := Conversation{AgentType: typeID, AgentName: name, ProjectID: p.ID, ProjectName: p.Name,
				CWD: native.CWD, Title: native.Title, CreatedAt: native.CreatedAt, UpdatedAt: newest(native.CreatedAt, native.UpdatedAt)}
			if chat, exists := chats[id]; exists {
				if represented[id] {
					continue
				}
				represented[id] = true
				if q.AgentID != "" && chat.AgentID != q.AgentID {
					continue
				}
				row = chatRow(chat)
				if !chat.TitleIsUserSet {
					row.Title = native.Title
				}
				row.UpdatedAt = newest(row.UpdatedAt, native.UpdatedAt)
			}
			row.ID = "native:" + registry.SyncKey(snapshot.WorkspaceID, typeID, native.CWD, native.ID)[:32]
			row.Title = agent.FirstNonEmpty(row.Title, name+" conversation")
			row.native, row.harnessName = native, name
			page.Items = append(page.Items, row)
		}
	}
	for _, chat := range snapshot.Chats {
		if represented[chat.ID] || q.AgentID != "" && chat.AgentID != q.AgentID {
			continue
		}
		if indexed[chat.ID] && complete[profiles[chat.AgentID].AgentType] && !refs[chat.ID].ForkPending && (idx.busy == nil || !idx.busy(chat.ID)) {
			continue // a complete native inventory no longer contains this session
		}
		page.Items = append(page.Items, chatRow(chat))
	}
	sort.Slice(page.Items, func(i, j int) bool { return before(page.Items[i], page.Items[j]) })
	return page, nil
}

// Browse uses a keyset cursor, so newly active chats cannot shift later pages
// and duplicate rows. Search runs over metadata only, across titles and folders.
func (idx *Index) Browse(ctx context.Context, q BrowseQuery) (Page, error) {
	if q.Limit < 0 || q.Limit > 100 || len(q.Search) > 512 || len(q.Cursor) > 2048 {
		return Page{}, fmt.Errorf("%w: invalid conversation query", registry.ErrInvalid)
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	search := strings.ToLower(strings.TrimSpace(q.Search))
	type cursor struct{ ID, UpdatedAt, Scope string }
	scope := registry.SyncKey(q.AgentID, search)
	var after cursor
	if q.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(b, &after) != nil || after.ID == "" || after.Scope != scope {
			return Page{}, fmt.Errorf("%w: invalid conversation cursor", registry.ErrInvalid)
		}
	}
	page, err := idx.all(ctx, q)
	if err != nil {
		return page, err
	}
	items := make([]Conversation, 0, min(q.Limit, len(page.Items)))
	all := page.Items
	page.Total = 0
	for _, row := range all {
		if search != "" && !strings.Contains(strings.ToLower(row.Title+"\n"+row.CWD+"\n"+row.ProjectName+"\n"+row.AgentName), search) {
			continue
		}
		page.Total++
		if after.ID != "" && !before(Conversation{ID: after.ID, UpdatedAt: after.UpdatedAt}, row) {
			continue
		}
		if len(items) < q.Limit+1 {
			items = append(items, row)
		}
	}
	if len(items) > q.Limit {
		items = items[:q.Limit]
		last := items[len(items)-1]
		b, _ := json.Marshal(cursor{last.ID, last.UpdatedAt, scope})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	page.Items = items
	return page, nil
}

// Open resolves the stable browser identity using native metadata, never a
// client-supplied path/session pair. It adopts a reference and delegates history
// and resume to the existing chat protocol. Repeated opens reuse the same chat.
func (idx *Index) Open(ctx context.Context, request OpenRequest) (OpenResult, error) {
	if request.ID == "" || len(request.ID) > 512 {
		return OpenResult{}, fmt.Errorf("%w: conversation id is required", registry.ErrInvalid)
	}
	page, err := idx.all(ctx, BrowseQuery{AgentID: request.AgentID})
	if err != nil {
		return OpenResult{}, err
	}
	for _, row := range page.Items {
		if row.ID != request.ID {
			continue
		}
		idx.gate.Lock()
		defer idx.gate.Unlock()
		if err := ctx.Err(); err != nil {
			return OpenResult{}, err
		}
		id := row.ChatID
		if row.native.ID != "" {
			id, err = idx.registry.OpenNativeSession(registry.NativeInventory{CWD: row.CWD,
				AgentType: row.AgentType, AgentName: row.harnessName, AgentID: request.AgentID, Sessions: []agent.NativeSession{row.native}}, idx.store)
			if err != nil {
				return OpenResult{}, err
			}
		}
		snapshot, err := idx.registry.Snapshot(nil)
		if err != nil {
			return OpenResult{}, err
		}
		for _, chat := range snapshot.Chats {
			if chat.ID == id {
				return OpenResult{ChatID: id, Snapshot: snapshot}, nil
			}
		}
		break
	}
	return OpenResult{}, fmt.Errorf("%w: conversation is no longer available; refresh the list", registry.ErrNotFound)
}
