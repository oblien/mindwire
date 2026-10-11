package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/nativesessions"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
)

// Resolve the saved native identity, never a caller-supplied arbitrary session
// or path. Registry hydration is the same operation used by paginated history.
func (a *API) nativeSessionContext(w http.ResponseWriter, r *http.Request) (*orchestrator.Agent, agent.NativeSessionQuery) {
	ag := a.agentFor(w, r)
	if ag == nil {
		return nil, agent.NativeSessionQuery{}
	}
	chatID := r.PathValue("id")
	if a.registry != nil {
		a.registryMu.Lock()
		chat, profile, _, err := a.registry.NativeChatContext(chatID, a.store)
		a.registryMu.Unlock()
		if err != nil {
			workspaceError(w, err)
			return nil, agent.NativeSessionQuery{}
		}
		if chat != nil {
			if selected := r.URL.Query().Get("agent"); selected != "" && selected != profile.AgentType {
				badRequest(w, "agent differs from this chat's saved profile")
				return nil, agent.NativeSessionQuery{}
			}
			var ok bool
			ag, ok = a.sup.Resolve(profile.AgentType)
			if !ok {
				badRequest(w, "saved harness is unavailable")
				return nil, agent.NativeSessionQuery{}
			}
		}
	}
	if a.store.PendingFork(ag.ID(), chatID) != nil {
		nativeSessionError(w, agent.NativeError("native_fork_pending", "Send the first message in this fork before attaching to its native session."))
		return nil, agent.NativeSessionQuery{}
	}
	q := agent.NativeSessionQuery{SessionID: a.store.Session(ag.ID(), chatID), CWD: agent.FirstNonEmpty(a.store.ChatCWD(chatID), a.sup.CWD())}
	if err := agent.ValidateNativeSessionQuery(q); err != nil {
		nativeSessionError(w, err)
		return nil, q
	}
	return ag, q
}

func (a *API) nativeSessionActivity(w http.ResponseWriter, r *http.Request) {
	ag, q := a.nativeSessionContext(w, r)
	if ag == nil {
		return
	}
	activity, err := a.sup.NativeSessions().Activity(r.Context(), ag.Adapter, q)
	if err != nil {
		nativeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, activity)
}

// The caller holds registryMu while validating and mutating the chat metadata.
func (a *API) nativeChatMutation(w http.ResponseWriter, r *http.Request, chatID string) bool {
	if a.registry != nil {
		if _, _, _, err := a.registry.NativeChatContext(chatID, a.store); err != nil {
			workspaceError(w, err)
			return false
		}
	}
	if err := a.sup.CheckNativeChatMutation(r.Context(), chatID); err != nil {
		nativeSessionError(w, err)
		return false
	}
	return true
}

// The cursor contains an attachment epoch as well as a sequence, so a daemon
// restart cannot be confused with an already-consumed stream of the same chat.
func nativeCursor(r *http.Request) (string, int64, error) {
	cursor := r.Header.Get("Last-Event-ID")
	if cursor == "" {
		cursor = r.URL.Query().Get("cursor")
	}
	if cursor == "" {
		return "", 0, nil
	}
	connection, sequence, ok := strings.Cut(cursor, ":")
	after, err := strconv.ParseInt(sequence, 10, 64)
	if !ok || err != nil || after < 0 || !agent.ValidNativeSessionID(connection) {
		return "", 0, agent.NativeError("native_cursor_invalid", "Invalid native event cursor.")
	}
	return connection, after, nil
}

func (a *API) nativeSessionEvents(w http.ResponseWriter, r *http.Request) {
	ag, q := a.nativeSessionContext(w, r)
	if ag == nil {
		return
	}
	connection, after, err := nativeCursor(r)
	if err != nil {
		nativeSessionError(w, err)
		return
	}
	base, replay, events, detach, err := a.sup.NativeSessions().Subscribe(r.Context(), ag.Adapter, q, connection, after)
	if err != nil {
		nativeSessionError(w, err)
		return
	}
	defer detach()
	controller := http.NewResponseController(w)
	sseHeaders(w)
	write := func(kind, id string, value any) bool {
		data, err := json.Marshal(value)
		if err != nil {
			return false
		}
		// A stalled viewer cannot pin its subscription forever. This affects
		// only its HTTP response, never the harness's work.
		_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if _, err = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", id, kind, data); err != nil {
			return false
		}
		err = controller.Flush()
		_ = controller.SetWriteDeadline(time.Time{})
		return err == nil
	}
	kind := "snapshot"
	if base.Replay {
		kind = "resume"
	}
	if !write(kind, fmt.Sprintf("%s:%d", base.ConnectionID, base.Sequence), nativesessions.Frame{Type: kind, Snapshot: &base}) {
		return
	}
	publish := func(event nativesessions.Event) bool {
		return write("update", fmt.Sprintf("%s:%d", event.ConnectionID, event.Sequence), nativesessions.Frame{Type: "update", Update: &event})
	}
	for _, event := range replay {
		if !publish(event) {
			return
		}
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok || !publish(event) {
				return
			}
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || controller.Flush() != nil {
				return
			}
			_ = controller.SetWriteDeadline(time.Time{})
		}
	}
}

func (a *API) nativeSessionAction(w http.ResponseWriter, r *http.Request) {
	ag, q := a.nativeSessionContext(w, r)
	if ag == nil {
		return
	}
	var action agent.NativeSessionAction
	if err := decode(w, r, &action); err != nil {
		badRequest(w, "invalid native action")
		return
	}
	var err error
	action.Attachments, err = a.resolveAttachments(r.PathValue("id"), action.Attachments)
	if err != nil {
		attachmentError(w, err)
		return
	}
	receipt, err := a.sup.NativeSessions().Action(r.Context(), ag.Adapter, q, action)
	if err != nil {
		nativeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func nativeSessionError(w http.ResponseWriter, err error) {
	var native *agent.NativeSessionError
	if !errors.As(err, &native) {
		if errors.Is(err, context.Canceled) {
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, &agent.NativeSessionError{Code: "native_session_unavailable", Message: "The native session could not be reached. Check its terminal and reconnect."})
		return
	}
	status := http.StatusConflict
	switch native.Code {
	case "native_session_invalid", "native_action_invalid", "native_attachment_invalid", "native_cursor_invalid", "native_response_invalid", "native_turn_required":
		status = http.StatusBadRequest
	case "native_session_capacity", "native_action_capacity", "native_connection_lost", "native_action_unknown":
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, native)
}
