package api

import (
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/registry"
)

func (a *API) projectLibrary(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	a.registryMu.Lock()
	defer a.registryMu.Unlock()
	if r.Method == http.MethodPatch {
		var edit registry.ProjectLibraryEdit
		if err := decode(w, r, &edit); err != nil {
			badRequest(w, "invalid project library edit")
			return
		}
		if err := a.registry.EditProjectLibrary(edit); err != nil {
			workspaceError(w, err)
			return
		}
	}
	// No native session discovery or filesystem reads for library-only operations.
	library, err := a.registry.ProjectLibrary()
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, library)
}
