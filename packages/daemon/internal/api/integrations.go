package api

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/oblien/mindwire/packages/daemon/internal/integrations"
)

func (a *API) integrationService(w http.ResponseWriter) *integrations.Service {
	if a.integrations == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "integrations require a workspace registry"})
	}
	return a.integrations
}
func (a *API) integrationsList(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	value, err := s.List()
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) integrationGet(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	value, err := s.Get(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) integrationSave(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	var spec integrations.Spec
	if err := decode(w, r, &spec); err != nil {
		badRequest(w, "invalid integration configuration")
		return
	}
	value, err := s.Save(r.PathValue("id"), spec)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) integrationDelete(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	if err := s.Delete(r.PathValue("id")); err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
func (a *API) integrationSend(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := decode(w, r, &input); err != nil {
		badRequest(w, "invalid message")
		return
	}
	if err := s.Send(r.Context(), r.PathValue("id"), input.Text); err != nil {
		botTaskError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"sent": true})
}
func (a *API) integrationEvents(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	events, err := s.Events(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, events)
}
func (a *API) integrationRetry(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	event, err := s.Retry(r.PathValue("id"), r.PathValue("eventId"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, event)
}
func (a *API) IntegrationWebhook(w http.ResponseWriter, r *http.Request) {
	if s := a.integrationService(w); s != nil {
		a.serviceOperation(s.Webhook)(w, r)
	}
}

func (a *API) integrationClaim(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	event, err := s.Claim(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, event)
}
func (a *API) integrationComplete(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	var input struct {
		Lease string `json:"lease"`
		Text  string `json:"text"`
	}
	if err := decode(w, r, &input); err != nil {
		badRequest(w, "invalid completion")
		return
	}
	event, err := s.Complete(r.PathValue("id"), r.PathValue("eventId"), input.Lease, input.Text)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, 200, event)
}

// The authenticated host forwards the exact provider bytes, never a trusted decoded event.
func (a *API) integrationForward(w http.ResponseWriter, r *http.Request) {
	s := a.integrationService(w)
	if s == nil {
		return
	}
	var input struct {
		Body    string            `json:"body"`
		Headers map[string]string `json:"headers"`
	}
	if err := decode(w, r, &input); err != nil || len(input.Body) > 1<<20 {
		badRequest(w, "invalid webhook")
		return
	}
	forwarded, _ := http.NewRequestWithContext(r.Context(), "POST", "http://provider.invalid/webhook", strings.NewReader(input.Body))
	forwarded.SetPathValue("id", r.PathValue("id"))
	for _, key := range []string{"Content-Type", "Origin", "Authorization", "X-Slack-Signature", "X-Slack-Request-Timestamp", "X-Signature-Ed25519", "X-Signature-Timestamp", "X-Telegram-Bot-Api-Secret-Token"} {
		for k, v := range input.Headers {
			if strings.EqualFold(key, k) {
				forwarded.Header.Set(key, v)
			}
		}
	}
	capture := &integrationResponse{header: make(http.Header), status: 200}
	s.Webhook(capture, forwarded)
	writeJSON(w, 200, map[string]any{"status": capture.status, "body": capture.body.String(), "contentType": capture.header.Get("Content-Type")})
}

type integrationResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *integrationResponse) Header() http.Header            { return r.header }
func (r *integrationResponse) WriteHeader(status int)         { r.status = status }
func (r *integrationResponse) Write(body []byte) (int, error) { return r.body.Write(body) }
