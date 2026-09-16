package mindwire

import (
	"context"
	"iter"

	"github.com/oblien/mindwire/daemon/internal/projects"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

type ProjectRequest = projects.Request
type ProjectAuth = projects.Auth
type ProjectSpec = registry.ProjectSpec
type ProjectOperation = registry.ProjectOperation
type ProjectRemoveRequest = projects.RemoveRequest

// CreateProject starts a daemon-owned folder import, new directory, or clone.
// Request.ID is a stable idempotency key; closing an observer never cancels the work.
func (w *Workspace) CreateProject(request ProjectRequest) (ProjectOperation, error) {
	o, err := w.c.core.projects.Start(request)
	return o, workspaceError("Workspace.CreateProject", err)
}

// RemoveProjectFiles deletes the confirmed project's directory and membership.
// Projects.Delete only removes membership and retains files/native transcripts.
func (w *Workspace) RemoveProjectFiles(id string, request ProjectRemoveRequest) (ProjectOperation, error) {
	w.c.core.registryMu.Lock()
	defer w.c.core.registryMu.Unlock()
	o, err := w.c.core.projects.RemoveFiles(id, request)
	return o, workspaceError("Workspace.RemoveProjectFiles", err)
}

type ProjectOperations struct{ c *Client }

func (p *ProjectOperations) Get(id string) (ProjectOperation, error) {
	o, err := p.c.core.projects.Get(id)
	return o, workspaceError("Operations.Get", err)
}
func (p *ProjectOperations) List(activeOnly bool) ([]ProjectOperation, error) {
	o, err := p.c.core.projects.List(activeOnly)
	return o, workspaceError("Operations.List", err)
}
func (p *ProjectOperations) Cancel(id string) (ProjectOperation, error) {
	o, err := p.c.core.projects.Cancel(id)
	return o, workspaceError("Operations.Cancel", err)
}
func (p *ProjectOperations) Retry(id string, auth ProjectAuth) (ProjectOperation, error) {
	p.c.core.registryMu.Lock()
	defer p.c.core.registryMu.Unlock()
	o, err := p.c.core.projects.Retry(id, auth)
	return o, workspaceError("Operations.Retry", err)
}

// Watch yields the latest durable snapshot immediately, then changes until terminal.
// Cancelling ctx detaches only this observer. Cancel(id) explicitly stops the operation.
func (p *ProjectOperations) Watch(ctx context.Context, id string) iter.Seq2[ProjectOperation, error] {
	return func(yield func(ProjectOperation, error) bool) {
		var sequence int64 = -1
		for {
			changed := p.c.core.projects.Changes()
			o, err := p.Get(id)
			if err != nil {
				yield(o, err)
				return
			}
			if o.Sequence != sequence {
				if !yield(o, nil) {
					return
				}
				sequence = o.Sequence
			}
			if !o.Active() {
				return
			}
			select {
			case <-ctx.Done():
				yield(ProjectOperation{}, ctx.Err())
				return
			case <-changed:
			}
		}
	}
}
