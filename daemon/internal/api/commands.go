package api

import (
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

func (a *API) commands(w http.ResponseWriter, r *http.Request) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return
	}
	module, ok := ag.Adapter.(agent.CommandsModule)
	if !ok {
		badRequest(w, "this harness does not expose native commands")
		return
	}
	dir := a.dirParam(r)
	catalog, err := module.Commands(r.Context(), agent.CommandQuery{
		CWD: dir, Config: agent.ReadSettings(ag.Adapter, ag.Creds), Env: ag.Auth.EnvForRun(),
		Refresh: r.URL.Query().Get("refresh") == "true",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if catalog.Commands == nil {
		catalog.Commands = []agent.Command{}
	}
	writeJSON(w, http.StatusOK, catalog)
}
