package mindwire

import (
	"context"
	"net/http"

	"github.com/oblien/mindwire/packages/daemon/internal/integrations"
)

type Integration = integrations.Connection
type IntegrationSpec = integrations.Spec
type IntegrationCredentials = integrations.Credentials
type IntegrationReceipt = integrations.Receipt
type IntegrationHostEvent = integrations.HostEvent

// Integrations shares the daemon's persistent connections and native bot runtime.
// Start explicitly opts an embedded host into receiving and executing queued events.
type Integrations struct{ c *Client }

func (i *Integrations) List() ([]Integration, error) {
	value, err := i.c.core.integrations.List()
	return value, workspaceError("Integrations.List", err)
}
func (i *Integrations) Get(id string) (Integration, error) {
	value, err := i.c.core.integrations.Get(id)
	return value, workspaceError("Integrations.Get", err)
}
func (i *Integrations) Create(spec IntegrationSpec) (Integration, error) {
	value, err := i.c.core.integrations.Save("", spec)
	return value, workspaceError("Integrations.Create", err)
}
func (i *Integrations) Update(id string, spec IntegrationSpec) (Integration, error) {
	value, err := i.c.core.integrations.Save(id, spec)
	return value, workspaceError("Integrations.Update", err)
}
func (i *Integrations) Delete(id string) error {
	return workspaceError("Integrations.Delete", i.c.core.integrations.Delete(id))
}
func (i *Integrations) Send(ctx context.Context, id, text string) error {
	return botTaskError("Integrations.Send", i.c.core.integrations.Send(ctx, id, text))
}
func (i *Integrations) Events(id string) ([]IntegrationReceipt, error) {
	value, err := i.c.core.integrations.Events(id)
	return value, workspaceError("Integrations.Events", err)
}
func (i *Integrations) Retry(id, eventID string) (IntegrationReceipt, error) {
	value, err := i.c.core.integrations.Retry(id, eventID)
	return value, workspaceError("Integrations.Retry", err)
}
func (i *Integrations) Start() { i.c.core.integrations.Start() }

// Webhook validates provider signatures itself. Register using Go's path pattern
// POST /integrations/{id}/webhook; keep management routes behind owner auth.
func (i *Integrations) Webhook(w http.ResponseWriter, r *http.Request) {
	i.c.core.integrations.Webhook(w, r)
}

func (i *Integrations) Claim(id string) (*IntegrationHostEvent, error) {
	value, err := i.c.core.integrations.Claim(id)
	return value, workspaceError("Integrations.Claim", err)
}
func (i *Integrations) Complete(id, eventID, lease, text string) (IntegrationReceipt, error) {
	value, err := i.c.core.integrations.Complete(id, eventID, lease, text)
	return value, workspaceError("Integrations.Complete", err)
}
