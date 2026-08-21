package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oblien/mindwire/daemon/internal/agent"
	"github.com/oblien/mindwire/daemon/internal/notify"
	"github.com/oblien/mindwire/daemon/internal/orchestrator"
	"github.com/oblien/mindwire/daemon/internal/session"
	"github.com/oblien/mindwire/daemon/internal/stream"
)

// resolveFakeAdapter is a CLI-free adapter for the resolve HTTP tests: its RunStream emits the
// completion sentinel on the first turn, so a resolve run through the API completes deterministically
// in one iteration without touching a real CLI.
type resolveFakeAdapter struct{ id string }

func (f resolveFakeAdapter) RunStream(_ context.Context, _ agent.TurnInput, emit agent.Emit) (agent.TurnResult, error) {
	text := "done <<RESOLVE_DONE>>"
	emit(agent.Event{Type: agent.EventResult, Result: &agent.ResultInfo{Text: text}})
	return agent.TurnResult{Text: text}, nil
}

func (f resolveFakeAdapter) ID() string { return f.id }
func (f resolveFakeAdapter) Meta() agent.CatalogEntry {
	return agent.CatalogEntry{ID: f.id, Name: "ResolveFake"}
}
func (f resolveFakeAdapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{Resolve: true}
}
func (f resolveFakeAdapter) Settings() agent.SettingsSchema { return agent.SettingsSchema{} }
func (f resolveFakeAdapter) InstallSteps() []agent.Step     { return nil }
func (f resolveFakeAdapter) VersionCommand() string         { return "" }
func (f resolveFakeAdapter) ConfigPath() string             { return "" }
func (f resolveFakeAdapter) Auth(agent.CredStore) agent.AuthModule {
	return resolveFakeAuth{}
}
func (f resolveFakeAdapter) History(agent.HistoryQuery) ([]agent.Message, error) { return nil, nil }
func (f resolveFakeAdapter) Notifications() agent.NotificationSpec {
	return agent.NotificationSpec{}
}
func (f resolveFakeAdapter) Doctor(context.Context) []agent.Check { return nil }

type resolveFakeAuth struct{}

func (resolveFakeAuth) Methods() []agent.AuthMethod { return nil }
func (resolveFakeAuth) Begin(context.Context, string) (agent.AuthState, error) {
	return agent.AuthState{}, nil
}
func (resolveFakeAuth) Step(context.Context, map[string]string) (agent.AuthState, error) {
	return agent.AuthState{}, nil
}
func (resolveFakeAuth) Status(context.Context) agent.AuthStatus { return agent.AuthStatus{} }
func (resolveFakeAuth) EnvForRun() map[string]string            { return nil }

// TestRunChildrenHTTP: GET /runs/{id}/children returns a resolve parent's child iterations; an ordinary
// run returns an empty list (never an error); an unknown id is a 404.
func TestRunChildrenHTTP(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	sup := orchestrator.New(store, stream.New(), notify.Fanout(nil), t.TempDir(), "claude-code")
	mux := http.NewServeMux()
	New(store, stream.New(), sup).Register(mux)

	// A resolve parent with two child iterations, plus an unrelated ordinary run.
	_ = store.SaveRun(session.Run{ID: "p1", ChatID: "c1", Agent: "claude-code", Status: "done", Kind: "resolve", Iterations: 2, StopReason: "done", CreatedAt: "t0"})
	_ = store.SaveRun(session.Run{ID: "c1a", ChatID: "c1", Agent: "claude-code", Status: "done", ParentID: "p1", CreatedAt: "t1"})
	_ = store.SaveRun(session.Run{ID: "c1b", ChatID: "c1", Agent: "claude-code", Status: "done", ParentID: "p1", CreatedAt: "t2"})
	_ = store.SaveRun(session.Run{ID: "plain", ChatID: "c2", Agent: "claude-code", Status: "done", CreatedAt: "t3"})

	// Parent → both children, oldest→newest.
	children := getChildren(t, mux, "p1", http.StatusOK)
	if len(children) != 2 || children[0].ID != "c1a" || children[1].ID != "c1b" {
		t.Fatalf("children = %+v, want [c1a c1b]", children)
	}
	for _, c := range children {
		if c.ParentID != "p1" {
			t.Errorf("child %s ParentID = %q, want p1", c.ID, c.ParentID)
		}
	}

	// An ordinary run has no children: a 200 empty list, not a 404.
	if got := getChildren(t, mux, "plain", http.StatusOK); len(got) != 0 {
		t.Errorf("plain run children = %+v, want empty", got)
	}

	// Unknown id → 404.
	getChildren(t, mux, "nope", http.StatusNotFound)
}

// getChildren issues GET /runs/{id}/children, asserts the status, and decodes the run list on a 200.
func getChildren(t *testing.T, h http.Handler, id string, wantStatus int) []session.Run {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/runs/"+id+"/children", nil))
	if rec.Code != wantStatus {
		t.Fatalf("GET /runs/%s/children: status %d, want %d (body %s)", id, rec.Code, wantStatus, rec.Body.String())
	}
	if wantStatus != http.StatusOK {
		return nil
	}
	var out []session.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode children: %v (body %s)", err, rec.Body.String())
	}
	return out
}

// TestResolveModeHTTP: POST /turns {mode:"resolve"} routes to a global-resolve run — a 202 parent Run
// with kind:"resolve". Driven by a fake adapter so it completes without a real CLI; once done, the
// parent's child iteration is visible via GET /runs/{id}/children.
func TestResolveModeHTTP(t *testing.T) {
	fake := resolveFakeAdapter{id: "resolve-fake"}
	agent.Register(fake)
	store, err := session.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	sup := orchestrator.New(store, stream.New(), notify.Fanout(nil), t.TempDir(), fake.id)
	mux := http.NewServeMux()
	New(store, stream.New(), sup).Register(mux)

	// An unrecognized mode is a caller error (400), not a silent fallback to a normal turn.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/turns?agent="+fake.id, strings.NewReader(`{"chatId":"cbad","message":"m","mode":"bogus"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mode:bogus → status %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	// A real resolve: 202 with a parent Run of kind "resolve".
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/turns?agent="+fake.id, strings.NewReader(`{"chatId":"cok","message":"do it","mode":"resolve"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("mode:resolve → status %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	var parent session.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &parent); err != nil {
		t.Fatalf("decode parent: %v (body %s)", err, rec.Body.String())
	}
	if parent.Kind != "resolve" {
		t.Fatalf("parent.Kind = %q, want resolve", parent.Kind)
	}

	// The resolve runs in the background; wait for it to settle, then its one child is visible.
	waitRunDone(t, store, parent.ID)
	children := getChildren(t, mux, parent.ID, http.StatusOK)
	if len(children) != 1 {
		t.Fatalf("children = %d, want 1", len(children))
	}
}

// waitRunDone polls the store until a run reaches a terminal status (or a short deadline elapses).
func waitRunDone(t *testing.T, store *session.Store, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r, ok := store.GetRun(id); ok {
			switch r.Status {
			case "done", "error", "cancelled":
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach a terminal status in time", id)
}
