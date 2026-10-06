package api

import (
	"errors"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/artifact"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

func (a *API) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	if a.surfaces == nil {
		writeJSON(w, 503, map[string]string{"error": "Attachment storage is unavailable. Update the Mindwire service."})
		return
	}
	var req artifact.UploadChunk
	if err := decode(w, r, &req); err != nil {
		badRequest(w, "invalid attachment chunk")
		return
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	state, err := a.surfaces.Artifacts().Upload(r.PathValue("id"), r.PathValue("attachmentID"), req)
	if err != nil {
		attachmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (a *API) resolveAttachments(chatID string, attachments []agent.Attachment) ([]agent.Attachment, error) {
	if a.surfaces != nil {
		return a.surfaces.Artifacts().ResolveInputs(chatID, attachments)
	}
	for _, attachment := range attachments {
		if attachment.ArtifactID != "" {
			return nil, artifact.ErrUpload
		}
	}
	return attachments, nil
}

func attachmentError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, artifact.ErrUpload):
		status = http.StatusBadRequest
	case errors.Is(err, artifact.ErrUploadConflict):
		status = http.StatusConflict
	case errors.Is(err, artifact.ErrStorageFull):
		status = http.StatusInsufficientStorage
	case errors.Is(err, registry.ErrNotFound):
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
