package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/oblien/mindwire/daemon/internal/gitauthor"
	"github.com/oblien/mindwire/daemon/internal/gitops"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

func (a *API) authorDirectory(r *http.Request) (string, error) {
	if id := r.URL.Query().Get("projectId"); id != "" {
		project, err := a.registry.Project(id)
		if err != nil {
			return "", err
		}
		return gitops.Root(r.Context(), project.Path)
	}
	if path := r.URL.Query().Get("path"); path != "" {
		return gitops.Root(r.Context(), path)
	}
	return "", nil
}

func (a *API) gitIdentity(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	directory, err := a.authorDirectory(r.WithContext(ctx))
	if err != nil {
		workspaceError(w, err)
		return
	}
	state, err := a.gitAuthors.Read(ctx, directory)
	if err != nil {
		gitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// Attribution is a settings write. This handler has no path to committing,
// staging, or changing a branch; retrying it can only save the same settings.
func (a *API) gitIdentitySave(w http.ResponseWriter, r *http.Request) {
	if !a.requireGit(w) {
		return
	}
	var body json.RawMessage
	if decode(w, r, &body) != nil {
		badRequest(w, "invalid Git author settings")
		return
	}
	var fields map[string]json.RawMessage
	var req gitauthor.Update
	if json.Unmarshal(body, &fields) != nil || fields["identity"] == nil || json.Unmarshal(body, &req) != nil {
		badRequest(w, "identity is required; use null to remove an override")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	directory, err := a.authorDirectory(r.WithContext(ctx))
	if err != nil {
		workspaceError(w, err)
		return
	}
	if req.Scope == gitauthor.Repository && directory == "" {
		workspaceError(w, fmt.Errorf("%w: select a repository", registry.ErrInvalid))
		return
	}
	state, err := a.gitAuthors.Save(ctx, directory, req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, registry.ErrConflict) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, state)
}
