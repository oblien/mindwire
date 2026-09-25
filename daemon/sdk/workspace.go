package mindwire

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/conversations"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

// Aliases keep the same records, revisions and deletion protocol across Go, HTTP, TypeScript and iOS.
type (
	WorkspaceRecord   = registry.Record
	WorkspaceAgent    = registry.Agent
	WorkspaceProject  = registry.Project
	WorkspaceChat     = registry.Chat
	WorkspaceDeletion = registry.Deletion
	WorkspaceImport   = registry.Import
	WorkspaceSnapshot = registry.Snapshot
)

// Workspace is workspace-wide metadata; selecting a different harness never changes its scope.
// Native harness configuration and transcripts remain in their existing stores.
type Workspace struct {
	c          *Client
	Agents     *WorkspaceCollection[WorkspaceAgent]
	Projects   *WorkspaceCollection[WorkspaceProject]
	Chats      *WorkspaceCollection[WorkspaceChat]
	Operations *ProjectOperations
}

type WorkspaceCollection[T any] struct {
	c    *Client
	kind string
}

func newWorkspace(c *Client) *Workspace {
	return &Workspace{c: c,
		Operations: &ProjectOperations{c: c},
		Agents:     &WorkspaceCollection[WorkspaceAgent]{c, "agents"},
		Projects:   &WorkspaceCollection[WorkspaceProject]{c, "projects"},
		Chats:      &WorkspaceCollection[WorkspaceChat]{c, "chats"}}
}

func workspaceError(op string, err error) error {
	if err == nil {
		return nil
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, registry.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, registry.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, registry.ErrDeleted):
		status = http.StatusGone
	case errors.Is(err, registry.ErrNotFound):
		status = http.StatusNotFound
	}
	return &APIError{Message: err.Error(), Status: status, Op: op, Cause: err}
}

type WorkspaceSyncOptions struct{ Refresh bool }

func (w *Workspace) Snapshot(options ...WorkspaceSyncOptions) (WorkspaceSnapshot, error) {
	issues, err := w.c.core.conversations.Refresh(context.Background(), conversations.Query{Refresh: len(options) > 0 && options[0].Refresh})
	if err != nil {
		return WorkspaceSnapshot{}, workspaceError("Workspace.Snapshot", err)
	}
	w.c.core.registryMu.Lock()
	defer w.c.core.registryMu.Unlock()
	s, err := w.c.core.registry.Snapshot(nil)
	s.SessionDiscoveryIssues = issues
	return s, workspaceError("Workspace.Snapshot", err)
}

// Changes uses both fields of a prior snapshot's cursor. A replaced/restored database returns 409;
// recover by replacing the cache with Snapshot, never by re-importing an authoritative cache.
func (w *Workspace) Changes(since int64, workspaceID string, options ...WorkspaceSyncOptions) (WorkspaceSnapshot, error) {
	s, err := w.c.core.registry.Snapshot(&since)
	if err == nil && workspaceID != "" && workspaceID != s.WorkspaceID {
		err = registry.ErrConflict
	}
	if err != nil {
		return s, workspaceError("Workspace.Changes", err)
	}
	issues, err := w.c.core.conversations.Refresh(context.Background(), conversations.Query{Refresh: len(options) > 0 && options[0].Refresh})
	if err != nil {
		return s, workspaceError("Workspace.Changes", err)
	}
	w.c.core.registryMu.Lock()
	defer w.c.core.registryMu.Unlock()
	s, err = w.c.core.registry.Snapshot(&since)
	s.SessionDiscoveryIssues = issues
	return s, workspaceError("Workspace.Changes", err)
}

// Import inserts missing legacy IDs. Existing records and tombstones always win.
func (w *Workspace) Import(records WorkspaceImport) (WorkspaceSnapshot, error) {
	w.c.core.registryMu.Lock()
	defer w.c.core.registryMu.Unlock()
	if err := w.c.core.registry.Import(records); err != nil {
		return WorkspaceSnapshot{}, workspaceError("Workspace.Import", err)
	}
	if err := w.c.core.registry.RestoreSessions(records.Chats, w.c.core.store); err != nil {
		return WorkspaceSnapshot{}, workspaceError("Workspace.Import", err)
	}
	s, err := w.c.core.registry.Snapshot(nil)
	return s, workspaceError("Workspace.Import", err)
}

// Put creates with a stable client-generated ID (expected=nil), or replaces the record only if
// expected matches its latest revision. Retrying an identical acknowledged write is idempotent.
func (collection *WorkspaceCollection[T]) Put(id string, record T, expected *int64) (WorkspaceSnapshot, error) {
	co := collection.c.core
	data, err := json.Marshal(record)
	if err != nil {
		return WorkspaceSnapshot{}, workspaceError("Workspace.Put", err)
	}
	if collection.kind == "agents" {
		var profile WorkspaceAgent
		_ = json.Unmarshal(data, &profile)
		if _, ok := co.sup.Resolve(profile.AgentType); !ok {
			return WorkspaceSnapshot{}, &APIError{Message: "unknown harness type", Status: http.StatusBadRequest, Op: "Workspace.Put"}
		}
	}
	co.registryMu.Lock()
	if collection.kind == "projects" {
		err = co.projects.Save(id, data, expected)
	} else {
		err = co.registry.Put(collection.kind, id, data, expected)
	}
	co.registryMu.Unlock()
	if err != nil {
		return WorkspaceSnapshot{}, workspaceError("Workspace.Put", err)
	}
	var issues []registry.SessionDiscoveryIssue
	if collection.kind == "projects" {
		issues, err = co.conversations.Refresh(context.Background(), conversations.Query{ProjectID: id})
		if err != nil {
			return WorkspaceSnapshot{}, workspaceError("Workspace.Put", err)
		}
	}
	s, err := co.registry.Snapshot(nil)
	s.SessionDiscoveryIssues = issues
	return s, workspaceError("Workspace.Put", err)
}

// Delete removes membership and dependent chat links. Native files and transcripts are retained;
// use Client.DeleteChat when a transcript purge is intended. Running chats block parent deletion.
func (collection *WorkspaceCollection[T]) Delete(id string, expected *int64) (WorkspaceSnapshot, error) {
	co := collection.c.core
	co.registryMu.Lock()
	defer co.registryMu.Unlock()
	if collection.kind == "projects" {
		if err := co.projects.Remove(id, expected); err != nil {
			return WorkspaceSnapshot{}, workspaceError("Workspace.Delete", err)
		}
		s, err := co.registry.Snapshot(nil)
		return s, workspaceError("Workspace.Delete", err)
	}
	s, err := co.registry.Snapshot(nil)
	if err != nil {
		return s, workspaceError("Workspace.Delete", err)
	}
	for _, chat := range s.Chats {
		if (collection.kind == "chats" && chat.ID == id || collection.kind == "agents" && chat.AgentID == id ||
			collection.kind == "projects" && chat.ProjectID == id) && co.sup.Busy(chat.ID) {
			return WorkspaceSnapshot{}, &APIError{Message: "stop the running chat before removing this record",
				Status: http.StatusConflict, Op: "Workspace.Delete"}
		}
	}
	if err := co.registry.Delete(collection.kind, id, expected, false, co.store); err != nil {
		return WorkspaceSnapshot{}, workspaceError("Workspace.Delete", err)
	}
	s, err = co.registry.Snapshot(nil)
	return s, workspaceError("Workspace.Delete", err)
}

func (c *Client) resolveChat(op, chatID, selected, cwd string) (*orchestrator.Agent, string, error) {
	chat, profile, project, err := c.core.registry.NativeChatContext(chatID, c.core.store)
	if err != nil {
		return nil, "", workspaceError(op, err)
	}
	if chat != nil {
		if selected != "" && selected != profile.AgentType {
			return nil, "", &APIError{Message: "chat belongs to a different harness", Status: http.StatusBadRequest, Op: op}
		}
		selected, cwd = profile.AgentType, project.Path
	}
	path := cwd
	if path == "" {
		path = c.core.sup.CWD()
	}
	if err := c.core.registry.CheckProjectPath(path); err != nil {
		return nil, "", workspaceError(op, err)
	}
	ag, ok := c.core.sup.Resolve(selected)
	if !ok {
		return nil, "", &APIError{Message: "unknown agent", Status: http.StatusBadRequest, Op: op}
	}
	return ag, cwd, nil
}
