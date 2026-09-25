package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/oblien/mindwire/daemon/internal/workspaceexec"
)

func executionError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if os.IsNotExist(err) {
		status = http.StatusNotFound
	}
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func (a *API) workspaceHost(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.execution.Info())
}
func (a *API) workspaceResources(w http.ResponseWriter, r *http.Request) {
	value, err := a.execution.Resources(r.Context())
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) workspaceFiles(w http.ResponseWriter, r *http.Request) {
	value, err := a.execution.Files(r.Context(), r.URL.Query().Get("path"), r.URL.Query().Get("query"))
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) workspaceFileRead(w http.ResponseWriter, r *http.Request) {
	value, err := a.execution.Read(r.URL.Query().Get("path"))
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) workspaceFileWrite(w http.ResponseWriter, r *http.Request) {
	var req workspaceexec.Content
	r.Body = http.MaxBytesReader(w, r.Body, workspaceexec.MaxFileBytes*6+4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid file content")
		return
	}
	if req.Path == "" {
		badRequest(w, "file path is required")
		return
	}
	if err := a.execution.Write(req.Path, req.Content); err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) workspaceFileDelete(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		badRequest(w, "file path is required")
		return
	}
	if err := a.execution.Delete(path); err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) workspaceExec(w http.ResponseWriter, r *http.Request) {
	var req workspaceexec.ExecRequest
	if decode(w, r, &req) != nil {
		badRequest(w, "invalid command")
		return
	}
	result, err := a.execution.Exec(r.Context(), req, nil)
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (a *API) workspaceExecStream(w http.ResponseWriter, r *http.Request) {
	var req workspaceexec.ExecRequest
	if decode(w, r, &req) != nil {
		badRequest(w, "invalid command")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	var mu sync.Mutex
	emit := func(value any) {
		mu.Lock()
		defer mu.Unlock()
		data, _ := json.Marshal(value)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		_ = http.NewResponseController(w).Flush()
	}
	result, err := a.execution.Exec(r.Context(), req, func(kind string, data []byte) {
		emit(map[string]any{"kind": "output", "channel": kind, "data": data})
	})
	if err != nil {
		emit(map[string]any{"kind": "error", "error": err.Error()})
		return
	}
	emit(map[string]any{"kind": "exit", "exitCode": result.ExitCode})
}

func (a *API) terminalsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.execution.Terminals.List(r.URL.Query().Get("directory")))
}
func (a *API) terminalsOpen(w http.ResponseWriter, r *http.Request) {
	var req workspaceexec.TerminalRequest
	if decode(w, r, &req) != nil {
		badRequest(w, "invalid terminal")
		return
	}
	state, err := a.execution.Terminals.Open(req)
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 201, state)
}
func (a *API) terminalGet(w http.ResponseWriter, r *http.Request) {
	t, err := a.execution.Terminals.Get(r.PathValue("id"))
	if err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, t.State())
}
func (a *API) terminalDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.execution.Terminals.Remove(r.PathValue("id")); err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) terminalInput(w http.ResponseWriter, r *http.Request) {
	var req workspaceexec.TerminalInput
	if decode(w, r, &req) != nil {
		badRequest(w, "invalid terminal input")
		return
	}
	t, err := a.execution.Terminals.Get(r.PathValue("id"))
	if err != nil {
		executionError(w, err)
		return
	}
	if err := t.Input(r.Context(), req); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "sequence": req.Sequence})
}
func (a *API) terminalResize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Columns int `json:"columns"`
		Rows    int `json:"rows"`
	}
	if decode(w, r, &req) != nil {
		badRequest(w, "invalid terminal size")
		return
	}
	t, err := a.execution.Terminals.Get(r.PathValue("id"))
	if err != nil {
		executionError(w, err)
		return
	}
	if err = t.Resize(req.Columns, req.Rows); err != nil {
		executionError(w, err)
		return
	}
	writeJSON(w, 200, t.State())
}
func (a *API) terminalEvents(w http.ResponseWriter, r *http.Request) {
	t, err := a.execution.Terminals.Get(r.PathValue("id"))
	if err != nil {
		executionError(w, err)
		return
	}
	after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	if err != nil && r.URL.Query().Get("after") != "" {
		badRequest(w, "invalid terminal cursor")
		return
	}
	replay, events, cancel := t.Subscribe(after)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	write := func(event workspaceexec.TerminalEvent) bool {
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.Sequence, data); err != nil {
			return false
		}
		return http.NewResponseController(w).Flush() == nil
	}
	for _, event := range replay {
		if !write(event) {
			return
		}
	}
	_ = http.NewResponseController(w).Flush()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok || !write(event) {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if http.NewResponseController(w).Flush() != nil {
				return
			}
		}
	}
}
