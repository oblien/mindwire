package nativesessions

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
)

// RunTurn lets existing /turns callers use an already-running owner. The same
// attachment serves the live-session API, so opening another viewer adds no CLI
// process. The supervisor still owns the request receipt and its detached run.
func (s *Service) RunTurn(ctx context.Context, ad agent.Adapter, in agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	if ctx.Err() != nil {
		return agent.TurnResult{Cancelled: true}, nil
	}
	q := agent.NativeSessionQuery{SessionID: agent.FirstNonEmpty(in.Options.SessionID, in.SessionID), CWD: in.CWD}
	base, _, events, release, err := s.Subscribe(ctx, ad, q, "", 0)
	if err != nil {
		return agent.TurnResult{IsError: true, Text: err.Error()}, err
	}
	defer release()
	emit(agent.Event{Type: agent.EventSession, SessionID: q.SessionID})
	if ctx.Err() != nil {
		return agent.TurnResult{SessionID: q.SessionID, Cancelled: true}, nil
	}
	for _, part := range base.Parts {
		if part.Interaction != nil && part.Interaction.NeedsResponse {
			emit(agent.Event{Type: agent.EventInteraction, Interaction: part.Interaction})
		}
	}
	request := agent.NativeSessionAction{ConnectionID: base.ConnectionID, RequestID: rand.Text(), Kind: "input", Text: in.Message, Attachments: in.Options.Attachments}
	// Wait for the native acknowledgement even if Stop arrives during submission;
	// only its returned turn ID is safe to interrupt. Never kill the owning server.
	submitCtx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	accepted, err := s.Action(submitCtx, ad, q, request)
	cancel()
	if err != nil {
		return agent.TurnResult{SessionID: q.SessionID, IsError: true, Text: err.Error()}, err
	}
	if accepted.TurnID == "" {
		err = agent.NativeError("native_action_unknown", "Codex did not identify the accepted turn. Check the native conversation before sending again.")
		return agent.TurnResult{SessionID: q.SessionID, IsError: true, Text: err.Error()}, err
	}
	emit(agent.Event{Type: agent.EventStatus, Meta: map[string]any{"nativeTurnId": accepted.TurnID, "nativeInputAccepted": true}})
	interrupt := func() error {
		stopCtx, stop := context.WithTimeout(context.Background(), 6*time.Second)
		defer stop()
		_, err := s.Action(stopCtx, ad, q, agent.NativeSessionAction{ConnectionID: base.ConnectionID, RequestID: rand.Text(), Kind: "interrupt", ExpectedTurnID: accepted.TurnID})
		return err
	}
	seenInputs := map[string]bool{request.RequestID: true}
	for {
		select {
		case <-ctx.Done():
			if stopErr := interrupt(); stopErr != nil {
				var nativeErr *agent.NativeSessionError
				if !errors.As(stopErr, &nativeErr) || nativeErr.Code != "native_turn_ended" && nativeErr.Code != "native_turn_changed" {
					return agent.TurnResult{SessionID: q.SessionID, Cancelled: true,
						Text: "Mindwire stopped waiting, but could not confirm Stop in the terminal. The native turn may still be running; reopen this conversation to check."}, nil
				}
			}
			return agent.TurnResult{SessionID: q.SessionID, Cancelled: true}, nil
		case update, ok := <-events:
			if !ok {
				err = agent.NativeError("native_connection_lost", "The connection to the native session was lost. Its work may still be running; reopen the conversation to check.")
				return agent.TurnResult{SessionID: q.SessionID, IsError: true, Text: err.Error()}, err
			}
			if update.TurnID != "" && update.TurnID != accepted.TurnID || update.Event == nil {
				continue
			}
			ev := *update.Event
			if ev.Input != nil {
				if seenInputs[ev.Input.ID] {
					continue
				}
				seenInputs[ev.Input.ID] = true
			}
			emit(ev)
			if ev.Type == agent.EventResult && ev.Result != nil {
				cancelled, _ := ev.Meta["nativeTurnStatus"].(string)
				return agent.TurnResult{SessionID: q.SessionID, Text: ev.Result.Text, IsError: ev.Result.IsError, Cancelled: cancelled == "interrupted"}, nil
			}
		case input, ok := <-in.Inbound:
			if !ok {
				in.Inbound = nil
				continue
			}
			action := agent.NativeSessionAction{ConnectionID: base.ConnectionID, RequestID: rand.Text(), ExpectedTurnID: accepted.TurnID}
			switch input.Kind {
			case "input":
				action.Kind = "input"
				action.Text = input.Text
				if input.Input != nil {
					action.RequestID = input.Input.ID
					action.Attachments = input.Input.Attachments
				}
			case "interrupt":
				action.Kind = "interrupt"
			case "response":
				action.Kind = "response"
				action.Response = &agent.InteractionResponse{InteractionID: input.InteractionID, Decision: input.Decision, Options: input.Options, Text: input.Text, Answers: input.Answers}
			default:
				if input.Ack != nil {
					input.Ack <- agent.NativeActionUnsupported(input.Kind)
				}
				continue
			}
			actionCtx, done := context.WithTimeout(s.ctx, 20*time.Second)
			_, actionErr := s.Action(actionCtx, ad, q, action)
			done()
			if input.Kind == "input" {
				var nativeErr *agent.NativeSessionError
				if errors.As(actionErr, &nativeErr) && nativeErr.Code == "native_turn_ended" {
					actionErr = agent.ErrInputClosed
				}
				if actionErr == nil {
					seenInputs[action.RequestID] = true
				}
				agent.AcknowledgeInput(input, actionErr, emit)
			} else {
				if input.Ack != nil {
					input.Ack <- actionErr
				}
				if actionErr != nil {
					emit(agent.Event{Type: agent.EventError, Error: actionErr.Error()})
				}
			}
		}
	}
}
