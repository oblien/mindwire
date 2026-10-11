package mindwire

import (
	"context"
	"iter"
	"net/http"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/nativesessions"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
)

func (c *Client) checkNativeChatMutation(op, chatID string) error {
	if _, _, _, err := c.core.registry.NativeChatContext(chatID, c.core.store); err != nil {
		return workspaceError(op, err)
	}
	if err := c.core.sup.CheckNativeChatMutation(context.Background(), chatID); err != nil {
		return &APIError{Message: err.Error(), Status: http.StatusConflict, Op: op, Cause: err}
	}
	return nil
}

type NativeSessionCapabilities = agent.NativeSessionCapabilities
type SessionActivity = agent.SessionActivity
type NativeSessionAction = agent.NativeSessionAction
type NativeSessionReceipt = agent.NativeSessionReceipt
type NativeSessionError = agent.NativeSessionError
type NativeSessionSnapshot = nativesessions.Snapshot
type NativeSessionEvent = nativesessions.Event
type NativeSessionFrame = nativesessions.Frame

type NativeSessionOptions struct {
	Agent string
	// Reuse both cursor fields only while retaining the corresponding UI state.
	ConnectionID string
	After        int64
}

func (c *Client) nativeSession(chatID string, options NativeSessionOptions) (*orchestrator.Agent, agent.NativeSessionQuery, error) {
	c.core.registryMu.Lock()
	ag, _, err := c.resolveChat("NativeSession", chatID, orDefault(options.Agent, c.defaultAgent), "")
	c.core.registryMu.Unlock()
	if err != nil {
		return nil, agent.NativeSessionQuery{}, err
	}
	if c.core.store.PendingFork(ag.ID(), chatID) != nil {
		return nil, agent.NativeSessionQuery{}, agent.NativeError("native_fork_pending", "Send the first message in this fork before attaching.")
	}
	q := agent.NativeSessionQuery{SessionID: c.core.store.Session(ag.ID(), chatID), CWD: agent.FirstNonEmpty(c.core.store.ChatCWD(chatID), c.core.sup.CWD())}
	return ag, q, agent.ValidateNativeSessionQuery(q)
}

// NativeSessionActivity discovers an existing owner without resuming a thread.
// Per-session capabilities, rather than harness-wide hints, govern live controls.
func (c *Client) NativeSessionActivity(ctx context.Context, chatID string, options NativeSessionOptions) (SessionActivity, error) {
	ag, q, err := c.nativeSession(chatID, options)
	if err != nil {
		return SessionActivity{}, err
	}
	return c.core.sup.NativeSessions().Activity(ctx, ag.Adapter, q)
}

// NativeSessionEvents observes the existing native process. Breaking iteration
// detaches this viewer; it never stops the turn. A snapshot replaces the current
// live-turn projection, while a resume retains it and replays newer updates.
func (c *Client) NativeSessionEvents(ctx context.Context, chatID string, options NativeSessionOptions) iter.Seq2[NativeSessionFrame, error] {
	return func(yield func(NativeSessionFrame, error) bool) {
		ag, q, err := c.nativeSession(chatID, options)
		if err != nil {
			yield(NativeSessionFrame{}, err)
			return
		}
		base, replay, events, detach, err := c.core.sup.NativeSessions().Subscribe(ctx, ag.Adapter, q, options.ConnectionID, options.After)
		if err != nil {
			yield(NativeSessionFrame{}, err)
			return
		}
		defer detach()
		kind := "snapshot"
		if base.Replay {
			kind = "resume"
		}
		if !yield(NativeSessionFrame{Type: kind, Snapshot: &base}, nil) {
			return
		}
		for _, event := range replay {
			if !yield(NativeSessionFrame{Type: "update", Update: &event}, nil) {
				return
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					yield(NativeSessionFrame{}, agent.NativeError("native_connection_lost", "The native connection ended. Reconnect to read its current state."))
					return
				}
				if !yield(NativeSessionFrame{Type: "update", Update: &event}, nil) {
					return
				}
			}
		}
	}
}

// ActOnNativeSession deduplicates requestId within the connectionId returned by
// NativeSessionEvents. Keep both on retries; an expired epoch is rejected rather
// than resending a possibly accepted message. Stop requires ExpectedTurnID.
func (c *Client) ActOnNativeSession(ctx context.Context, chatID string, action NativeSessionAction, options NativeSessionOptions) (NativeSessionReceipt, error) {
	ag, q, err := c.nativeSession(chatID, options)
	if err != nil {
		return NativeSessionReceipt{}, err
	}
	action.Attachments, err = c.core.surfaces.Artifacts().ResolveInputs(chatID, action.Attachments)
	if err != nil {
		return NativeSessionReceipt{}, err
	}
	return c.core.sup.NativeSessions().Action(ctx, ag.Adapter, q, action)
}
