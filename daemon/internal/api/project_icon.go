package api

import (
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/projecticon"
	"github.com/oblien/mindwire/daemon/internal/registry"
)

// A path previews an in-project candidate; omitting it reads the saved icon.
// The same authenticated endpoint is used through cloud proxies and paired SSH.
func (a *API) projectIcon(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	p, err := a.registry.Project(r.PathValue("id"))
	if err != nil {
		workspaceError(w, err)
		return
	}
	name := r.URL.Query().Get("path")
	if name == "" && p.IconPath != nil {
		name = *p.IconPath
	}
	if name == "" {
		workspaceError(w, registry.ErrNotFound)
		return
	}
	icon, err := projecticon.Read(p.Path, name)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, icon)
}
