package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// HistoryPageVersion adds an opt-in envelope and byte budget to the existing
// messages endpoint. Legacy callers continue to receive their unchanged array.
const HistoryPageVersion = 1

var errHistoryCursor = errors.New("history cursor is no longer present; reload the latest page")

type messagePage struct {
	Messages []json.RawMessage `json:"messages"`
	HasMore  bool              `json:"hasMore"`
	Before   string            `json:"before,omitempty"`
}

func writeHistory[T any](w http.ResponseWriter, r *http.Request, messages []T, id func(T) string) {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	before := query.Get("before")
	if query.Get("paged") != "true" {
		writeJSON(w, http.StatusOK, pageWindow(messages, limit, before, id))
		return
	}
	if query.Get("limit") == "" {
		limit = 40
	}
	budget := 1 << 20
	if value := query.Get("maxBytes"); value != "" {
		budget, _ = strconv.Atoi(value)
	}
	if limit < 1 || limit > 200 || budget < 1 || budget > 8<<20 {
		badRequest(w, "limit must be 1–200 and maxBytes must be 1–8388608")
		return
	}
	page, err := boundedHistory(messages, limit, before, budget, id)
	if errors.Is(err, errHistoryCursor) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "history_cursor_invalid"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not encode chat history"})
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// Keep whole messages and their native IDs. The budget is soft: up to two newest
// messages are always included (or one when limit=1), so a large answer retains
// its adjacent user input. No tools, diffs, or output are truncated to fit a page.
func boundedHistory[T any](messages []T, limit int, before string, budget int, id func(T) string) (messagePage, error) {
	end := len(messages)
	if before != "" {
		found := false
		for i := range messages {
			if id(messages[i]) == before {
				end, found = i, true
				break
			}
		}
		if !found {
			return messagePage{}, errHistoryCursor
		}
	}
	page := messagePage{Messages: make([]json.RawMessage, 0, min(limit, end))}
	start, size := end, 2
	for start > 0 && len(page.Messages) < limit {
		encoded, err := json.Marshal(messages[start-1])
		if err != nil {
			return messagePage{}, err
		}
		if len(page.Messages) >= min(2, limit) && size+len(encoded)+1 > budget {
			break
		}
		page.Messages = append(page.Messages, encoded)
		size += len(encoded) + 1
		start--
	}
	for i, j := 0, len(page.Messages)-1; i < j; i, j = i+1, j-1 {
		page.Messages[i], page.Messages[j] = page.Messages[j], page.Messages[i]
	}
	page.HasMore = start > 0
	if start < end {
		page.Before = id(messages[start])
	}
	return page, nil
}
