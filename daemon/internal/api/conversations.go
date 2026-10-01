package api

import (
	"net/http"
	"strconv"

	"github.com/oblien/mindwire/daemon/internal/conversations"
)

func (a *API) conversationsBrowse(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	q := r.URL.Query()
	limit := 0
	if value := q.Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			badRequest(w, "limit must be between 1 and 100")
			return
		}
	}
	page, err := a.conversations.Browse(r.Context(), conversations.BrowseQuery{AgentID: q.Get("agentId"),
		Search: q.Get("search"), Cursor: q.Get("cursor"), Limit: limit, Refresh: q.Get("refresh") == "true"})
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) conversationsOpen(w http.ResponseWriter, r *http.Request) {
	if !a.requireRegistry(w) {
		return
	}
	var request conversations.OpenRequest
	if err := decode(w, r, &request); err != nil {
		badRequest(w, "invalid conversation request")
		return
	}
	result, err := a.conversations.Open(r.Context(), request)
	if err != nil {
		workspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
