package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/surface"
)

func TestDesktopApprovalUsesSharedInteractionAndNeverCLIIngress(t *testing.T) {
	for _, decision := range []string{"allow", "deny", "cancel"} {
		t.Run(decision, func(t *testing.T) {
			s := newResolveSup(t, &fakeAdapter{id: "desktop-approval-" + decision})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			events := make(chan agent.Event, 4)
			s.mu.Lock()
			s.cancels["run"] = cancel
			s.serviceEmit["run"] = func(ev agent.Event) { events <- ev }
			s.inputs["run"] = make(chan agent.Inbound, 1)
			s.mu.Unlock()
			result := make(chan error, 1)
			go func() {
				result <- s.approveDesktop(ctx, surface.Actor{Kind: "agent", Name: "Codex", RunID: "run", ChatID: "chat"}, "Allow desktop?")
			}()
			var pending *agent.Interaction
			select {
			case ev := <-events:
				pending = ev.Interaction
			case <-time.After(time.Second):
				t.Fatal("approval was not published")
			}
			if pending == nil || !pending.NeedsResponse || pending.Meta["origin"] != "surface" {
				t.Fatal("not a shared pending interaction")
			}
			if err := s.RespondInteraction("different-run", agent.InteractionResponse{InteractionID: pending.ID, Decision: "allow"}); !errors.Is(err, ErrInteractionNotPending) {
				t.Fatal("wrong run answered permission")
			}
			if err := s.RespondInteraction("run", agent.InteractionResponse{InteractionID: pending.ID, Decision: "invalid"}); err == nil {
				t.Fatal("invalid permission accepted")
			}
			if decision == "cancel" {
				cancel()
			} else if err := s.RespondInteraction("run", agent.InteractionResponse{InteractionID: pending.ID, Decision: decision}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if (decision == "allow") != (err == nil) {
					t.Fatalf("decision %s: %v", decision, err)
				}
			case <-time.After(time.Second):
				t.Fatal("approval did not resolve")
			}
			resolved := <-events
			if resolved.Interaction.ID != pending.ID || resolved.Interaction.NeedsResponse {
				t.Fatal("permission card did not resolve in place")
			}
			if len(s.inputs["run"]) != 0 {
				t.Fatal("service response leaked into native CLI ingress")
			}
			if err := s.RespondInteraction("run", agent.InteractionResponse{InteractionID: pending.ID, Decision: "allow"}); !errors.Is(err, ErrInteractionNotPending) {
				t.Fatal("permission answered twice")
			}
		})
	}
}
